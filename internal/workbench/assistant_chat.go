package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"yovoice/internal/assistant"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 助手对话：用户消息交给内置助手一轮一轮地跑，运行中再发的消息排队到本轮结束后；
// 流式文本与工具卡片经状态广播给界面，会话记录持久化（有上限）。

// AssistantContext 是界面随每条用户消息附带的上下文，后端拼成简短前缀发给助手，不展示给用户。
type AssistantContext struct {
	Page      string          `json:"page,omitempty"`
	WorkID    string          `json:"workId,omitempty"`
	WorkTitle string          `json:"workTitle,omitempty"`
	WorkKind  string          `json:"workKind,omitempty"`
	Locale    schema.UiLocale `json:"locale,omitempty"`
}

// chatMessage 是排队中的一条用户消息。
type chatMessage struct {
	text    string
	context AssistantContext
}

// chatState 管理对话循环与当前一轮。
type chatState struct {
	mu      sync.Mutex
	running bool // 对话循环是否在运行
	stopped bool // 服务关闭后拒绝新消息
	cancel  context.CancelFunc
	queue   []chatMessage
	fresh   bool // 新对话后的首条消息加“新话题”前缀
	// turn 是当前一轮的实时数据；事件回调与工具处理器并发读写，由锁保护。
	turn *chatTurn
}

// chatTurn 是一轮对话的实时状态。
type chatTurn struct {
	text      strings.Builder // 已完成的助手消息文本
	delta     strings.Builder // 当前消息的流式增量
	cards     []schema.AssistantToolCard
	locale    schema.UiLocale // 界面语言，工具卡片摘要据此选择
	model     string
	startedAt time.Time
}

// SendAssistant 接收一条用户消息：立即入库并开始（或排队）一轮助手回复。
func (w *Workbench) SendAssistant(text string, context AssistantContext) error {
	text = strings.TrimSpace(text)
	if text == "" || schema.TextLen(text) > 2000 {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	if context.Locale != "" {
		if _, err := schema.ParseUiLocale(string(context.Locale)); err != nil {
			context.Locale = ""
		}
	}
	w.chat.mu.Lock()
	if w.chat.stopped {
		w.chat.mu.Unlock()
		return msg.Err(msg.ErrBusy, nil)
	}
	w.chat.queue = append(w.chat.queue, chatMessage{text: text, context: context})
	start := !w.chat.running
	w.chat.running = true
	w.chat.mu.Unlock()
	w.publishQueued()
	if start {
		go w.chatLoop()
	}
	return nil
}

// CancelAssistant 取消正在运行的一轮并清空排队；没有进行中的对话时什么都不做。
func (w *Workbench) CancelAssistant() {
	w.chat.mu.Lock()
	dropped := len(w.chat.queue)
	w.chat.queue = nil
	cancel := w.chat.cancel
	w.chat.mu.Unlock()
	if dropped > 0 {
		w.publishQueued()
	}
	if cancel != nil {
		diag.Log(w.Store.Root, "assistant.cancel_requested")
		cancel()
	}
}

// ResetAssistant 开始新的一段对话：取消进行中的一轮、清空排队、存一条分隔并换会话 ID。
// 助手的长期记忆不清；下一条消息会告知助手新话题开始。
func (w *Workbench) ResetAssistant() error {
	w.CancelAssistant()
	w.chat.mu.Lock()
	w.chat.fresh = true
	w.chat.mu.Unlock()
	w.appendChat(schema.AssistantMessage{ID: schema.NewID(), Role: "divider", CreatedAt: time.Now().UTC()})
	return w.Store.Update(func(s *schema.State) {
		if s.AssistantChat != nil {
			s.AssistantChat.SessionID = schema.NewID()
		}
	}, true)
}

// stopChat 取消进行中的对话并等待循环结束，服务关闭时调用。
func (w *Workbench) stopChat() {
	w.chat.mu.Lock()
	w.chat.stopped = true
	w.chat.queue = nil
	cancel := w.chat.cancel
	running := w.chat.running
	w.chat.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	deadline := time.After(15 * time.Second)
	for running {
		if cancel != nil {
			cancel()
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			return
		}
		w.chat.mu.Lock()
		running = w.chat.running
		cancel = w.chat.cancel
		w.chat.mu.Unlock()
	}
}

// chatLoop 依次处理排队消息，队列清空后退出。
func (w *Workbench) chatLoop() {
	for {
		w.chat.mu.Lock()
		if w.chat.stopped || len(w.chat.queue) == 0 {
			w.chat.running = false
			w.chat.cancel = nil
			w.chat.mu.Unlock()
			w.publishLive(func(live *schema.AssistantLive) { live.Turn, live.Queued = nil, 0 })
			return
		}
		next := w.chat.queue[0]
		w.chat.queue = w.chat.queue[1:]
		fresh := w.chat.fresh
		w.chat.fresh = false
		ctx, cancel := context.WithCancel(context.Background())
		w.chat.cancel = cancel
		w.chat.mu.Unlock()
		w.runTurn(ctx, next, fresh)
		cancel()
	}
}

// runTurn 跑一轮：用户消息入库、启动助手、实时广播、结束时写入助手消息。
func (w *Workbench) runTurn(parent context.Context, next chatMessage, fresh bool) {
	w.appendChat(schema.AssistantMessage{ID: schema.NewID(), Role: "user", Text: next.text, CreatedAt: time.Now().UTC()})
	cfg, err := w.assistantConfig()
	if err != nil {
		var ce *msg.CallError
		code, params := msg.ErrUnknown, msg.Params{"detail": err.Error()}
		if errors.As(err, &ce) {
			code, params = ce.Code, ce.Params
		}
		w.appendChat(schema.AssistantMessage{ID: schema.NewID(), Role: "assistant", Status: "failed", ErrorCode: code, ErrorParams: params, CreatedAt: time.Now().UTC()})
		return
	}
	timeout := aiTimeout(w.Store.Read().Preferences)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	turn := &chatTurn{model: cfg.Model, startedAt: time.Now().UTC(), locale: next.context.Locale}
	w.chat.mu.Lock()
	w.chat.turn = turn
	w.chat.mu.Unlock()
	w.publishLive(func(live *schema.AssistantLive) { live.Turn = turn.snapshot() })

	// 流式增量广播合流：状态推送带完整状态，逐字广播太频繁，这里按最小间隔合并。
	var lastDelta time.Time
	_, err = w.assistant.Ask(ctx, cfg, w.chatMessageText(next, fresh), func(e assistant.Event) {
		w.chat.mu.Lock()
		switch e.Kind {
		case assistant.EventText:
			turn.text.WriteString(e.Text)
			turn.delta.Reset()
		case assistant.EventTextDelta:
			turn.delta.WriteString(e.Text)
		case assistant.EventToolStart:
			turn.startCard(e.Tool, e.Args)
		case assistant.EventToolResult:
			turn.settleCard(e.Tool, e.ToolError)
		}
		snapshot := turn.snapshot()
		coalesced := time.Since(lastDelta) >= 100*time.Millisecond || e.Kind != assistant.EventTextDelta
		if coalesced {
			lastDelta = time.Now()
		}
		w.chat.mu.Unlock()
		if coalesced {
			w.publishLive(func(live *schema.AssistantLive) { live.Turn = snapshot })
		}
	})
	w.chat.mu.Lock()
	final := turn.snapshot()
	w.chat.turn = nil
	w.chat.mu.Unlock()

	reply := schema.AssistantMessage{ID: schema.NewID(), Role: "assistant", Text: final.Text, Tools: final.Tools, CreatedAt: time.Now().UTC()}
	switch {
	case err == nil:
		reply.Status = "completed"
	case errors.Is(parent.Err(), context.Canceled):
		reply.Status = "cancelled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		reply.Status = "failed"
		reply.ErrorCode, reply.ErrorParams = msg.ErrAITimeout, msg.Params{"seconds": int(timeout.Seconds())}
	default:
		reply.Status = "failed"
		var ce *msg.CallError
		if errors.As(err, &ce) {
			reply.ErrorCode, reply.ErrorParams = ce.Code, ce.Params
		} else {
			reply.ErrorCode, reply.ErrorParams = msg.ErrUnknown, msg.Params{"detail": err.Error()}
		}
	}
	diag.Log(w.Store.Root, "assistant.turn_finished", "status", reply.Status, "error", diag.Error(err))
	// 先写入回复再清除实时状态：界面看到的“空闲”帧一定带着本轮的助手消息。
	w.appendChat(reply)
	w.publishLive(func(live *schema.AssistantLive) { live.Turn = nil })
}

// chatCard 供助手工具更新当前轮的工具卡片：state 为 running、done 或 error；没有运行中的一轮时忽略。
// summary 提供 zh 与 en 两种文案，按界面语言选择。
func (w *Workbench) chatCard(tool, summaryZH, summaryEN, state string) {
	w.chat.mu.Lock()
	turn := w.chat.turn
	var snapshot *schema.AssistantTurn
	if turn != nil {
		summary := summaryZH
		if turn.locale == schema.UiLocaleEn {
			summary = summaryEN
		}
		turn.noteCard(tool, summary, state)
		snapshot = turn.snapshot()
	}
	w.chat.mu.Unlock()
	if snapshot != nil {
		w.publishLive(func(live *schema.AssistantLive) { live.Turn = snapshot })
	}
}

// chatNav 让界面跳到某作品或页面；ID 每次变化即新事件。
func (w *Workbench) chatNav(workID, page string) {
	nav := &schema.AssistantNav{ID: schema.NewID(), WorkID: workID, Page: page}
	w.publishLive(func(live *schema.AssistantLive) { live.Nav = nav })
}

// chatUpdate 通知界面：作品刚被助手保存，正打开的编辑器应刷新。
func (w *Workbench) chatUpdate(workID string) {
	update := &schema.AssistantUpdate{ID: schema.NewID(), WorkID: workID}
	w.publishLive(func(live *schema.AssistantLive) { live.Update = update })
}

// publishQueued 广播当前排队条数。
func (w *Workbench) publishQueued() {
	w.chat.mu.Lock()
	queued := len(w.chat.queue)
	w.chat.mu.Unlock()
	w.publishLive(func(live *schema.AssistantLive) { live.Queued = queued })
}

// publishLive 只在内存中更新助手的实时状态并广播。
func (w *Workbench) publishLive(change func(*schema.AssistantLive)) {
	_ = w.Store.Update(func(s *schema.State) {
		if s.AssistantLive == nil {
			s.AssistantLive = &schema.AssistantLive{}
		}
		live := *s.AssistantLive
		change(&live)
		s.AssistantLive = &live
	}, false)
}

// appendChat 追加一条消息并持久化，超过上限时丢弃最旧的消息。
func (w *Workbench) appendChat(m schema.AssistantMessage) {
	_ = w.Store.Update(func(s *schema.State) {
		if s.AssistantChat == nil {
			s.AssistantChat = &schema.AssistantChat{SessionID: schema.NewID(), Messages: []schema.AssistantMessage{}}
		}
		s.AssistantChat.Messages = append(s.AssistantChat.Messages, m)
		if excess := len(s.AssistantChat.Messages) - schema.AssistantChatLimit; excess > 0 {
			s.AssistantChat.Messages = s.AssistantChat.Messages[excess:]
		}
	}, true)
}

// snapshot 返回可安全广播的实时状态副本；调用方持有 chat.mu。
func (t *chatTurn) snapshot() *schema.AssistantTurn {
	turn := &schema.AssistantTurn{Text: t.text.String() + t.delta.String(), Model: t.model, StartedAt: t.startedAt}
	if len(t.cards) > 0 {
		turn.Tools = append([]schema.AssistantToolCard{}, t.cards...)
	}
	return turn
}

// startCard 登记一次工具调用开始；调用方持有 chat.mu。
func (t *chatTurn) startCard(tool, args string) {
	for i := range t.cards {
		if t.cards[i].Tool == tool && t.cards[i].State == "running" {
			return
		}
	}
	t.cards = append(t.cards, schema.AssistantToolCard{Tool: tool, Args: args, State: "running"})
}

// noteCard 由工具处理器登记或更新自己的卡片；调用方持有 chat.mu。
func (t *chatTurn) noteCard(tool, summary, state string) {
	for i := range t.cards {
		if t.cards[i].Tool == tool && t.cards[i].State == "running" {
			t.cards[i].Summary, t.cards[i].State = summary, state
			return
		}
	}
	t.cards = append(t.cards, schema.AssistantToolCard{Tool: tool, Summary: summary, State: state})
}

// settleCard 在工具结果事件到达时收尾仍为 running 的同名卡片；调用方持有 chat.mu。
func (t *chatTurn) settleCard(tool string, failed bool) {
	for i := range t.cards {
		if t.cards[i].Tool == tool && t.cards[i].State == "running" {
			if failed {
				t.cards[i].State = "error"
			} else {
				t.cards[i].State = "done"
			}
		}
	}
}

// pageLabels 是界面页面在上下文前缀里的描述。
var pageLabels = map[string]string{
	"text":       "voice generation（语音生成）",
	"story":      "story dubbing（故事配音）",
	"music":      "music generation（音乐生成）",
	"score":      "arrangement（编曲）",
	"characters": "voice library（声音库）",
	"voices":     "reference audio library（参考音色库）",
	"history":    "history（历史生成）",
	"settings":   "settings（设置）",
}

// chatMessageText 组装发给助手的一条用户消息：新话题提示、界面上下文前缀与用户正文。
func (w *Workbench) chatMessageText(next chatMessage, fresh bool) string {
	var b strings.Builder
	if fresh {
		b.WriteString("（新对话开始：请忽略之前对话里提到的作品与上下文。）\n\n")
	}
	context := next.context
	b.WriteString("<ui_context>\n")
	if label, ok := pageLabels[context.Page]; ok {
		b.WriteString("page: " + label + "\n")
	}
	if id := strings.TrimSpace(context.WorkID); id != "" {
		title, kind := strings.TrimSpace(context.WorkTitle), strings.TrimSpace(context.WorkKind)
		for _, draft := range w.Store.Read().Drafts {
			if draft.ID == id {
				title, kind = draft.Title, workKindOf(draft)
				break
			}
		}
		if title != "" {
			b.WriteString(fmt.Sprintf("current work: %q (id %s, kind %s)\n", title, id, kind))
		} else {
			b.WriteString(fmt.Sprintf("current work id: %s (kind %s)\n", id, kind))
		}
	}
	if context.Locale != "" {
		b.WriteString("ui_locale: " + string(context.Locale) + "\n")
	}
	b.WriteString("</ui_context>\n\n")
	b.WriteString(next.text)
	return b.String()
}
