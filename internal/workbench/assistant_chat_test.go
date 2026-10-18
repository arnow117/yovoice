package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yovoice/internal/assistant"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/msg"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 工具校验大多直接调用处理器：返回 error 即被拒绝；端到端流程另用假 ocho 驱动。
var toolReq = &mcp.CallToolRequest{}

// 一轮对话：流式增量与完整文本进入实时状态，工具卡片随调用更新，结束后写入消息记录。
func TestChatStreamsTextAndToolCards(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Delta: "你"},
		{Delta: "好"},
		{Assistant: "你好，列表在此。"},
		{Call: "list_works", Args: json.RawMessage(`{}`)},
		{Result: "success"},
	}}})
	changes, stop := w.Store.Subscribe()
	defer stop()
	must(t, w.SendAssistant("有哪些作品？", AssistantContext{Page: "text", Locale: schema.UiLocaleZhCN}))
	streamed, ran := false, false
	var lastLive *schema.AssistantLive
	deadline := time.After(30 * time.Second)
	for !(lastLive != nil && lastLive.Turn == nil && lastLive.Queued == 0 && len(w.Store.Read().AssistantChat.Messages) >= 2) {
		select {
		case <-changes:
		case <-deadline:
			t.Fatal("对话没有结束")
		}
		state := w.Store.Read()
		lastLive = state.AssistantLive
		if live := state.AssistantLive; live != nil && live.Turn != nil {
			ran = true
			if strings.Contains(live.Turn.Text, "你好") {
				streamed = true
			}
		}
	}
	if !ran || !streamed {
		t.Fatalf("应看到运行中的实时文本：ran=%v streamed=%v", ran, streamed)
	}
	messages := w.Store.Read().AssistantChat.Messages
	reply := messages[len(messages)-1]
	if reply.Role != "assistant" || reply.Status != "completed" || reply.Text != "你好，列表在此。" {
		t.Fatalf("助手消息不对：%+v", reply)
	}
	if len(reply.Tools) != 1 || reply.Tools[0].Tool != "list_works" || reply.Tools[0].State != "done" || reply.Tools[0].Summary == "" {
		t.Fatalf("工具卡片不对：%+v", reply.Tools)
	}
	if messages[0].Role != "user" || messages[0].Text != "有哪些作品？" {
		t.Fatalf("用户消息不对：%+v", messages[0])
	}
}

// 运行中再发的消息排队，本轮结束后按顺序处理。
func TestChatQueuesWhileRunning(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{SleepMS: 300},
		{Assistant: "第一"},
		{Result: "success"},
	}, {
		{Assistant: "第二"},
		{Result: "success"},
	}}})
	must(t, w.SendAssistant("一", AssistantContext{}))
	time.Sleep(100 * time.Millisecond)
	must(t, w.SendAssistant("二", AssistantContext{}))
	must(t, w.SendAssistant("三", AssistantContext{}))
	messages := waitChatIdle(t, w)
	if len(messages) != 6 {
		t.Fatalf("应有 6 条消息：%+v", messages)
	}
	for i, want := range []string{"一", "二", "三"} {
		if messages[i*2].Text != want || messages[i*2+1].Text != (map[int]string{0: "第一", 1: "第二", 2: "第二"}[i]) {
			t.Fatalf("第 %d 轮顺序不对：%+v", i, messages)
		}
	}
}

// 取消：正在运行的一轮停止（保留已生成的部分），排队消息清空，之后能继续对话。
func TestChatCancel(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Assistant: "写到一半"},
		{WaitInterrupt: true},
	}, {
		{Result: "success"},
	}}})
	must(t, w.SendAssistant("慢慢写", AssistantContext{}))
	waitFor(t, func() bool {
		live := w.Store.Read().AssistantLive
		return live != nil && live.Turn != nil && strings.Contains(live.Turn.Text, "写到一半")
	})
	must(t, w.SendAssistant("排队的", AssistantContext{}))
	waitFor(t, func() bool { return w.Store.Read().AssistantLive != nil && w.Store.Read().AssistantLive.Queued == 1 })
	w.CancelAssistant()
	messages := waitChatIdle(t, w)
	reply := messages[len(messages)-1]
	if reply.Role != "assistant" || reply.Status != "cancelled" || reply.Text != "写到一半" {
		t.Fatalf("取消后的消息不对：%+v", reply)
	}
	must(t, w.SendAssistant("再来", AssistantContext{}))
	if got := waitChatIdle(t, w); len(got) != len(messages)+2 {
		t.Fatal("取消后应能继续对话")
	}
}

// 新对话：存一条分隔并换会话 ID，下一条消息带“新话题”前缀发给助手。
func TestChatReset(t *testing.T) {
	echo := filepath.Join(t.TempDir(), "user-messages.txt")
	w := aiWorkbench(t, fakeocho.Plan{EchoUser: echo, Turns: [][]fakeocho.Step{{
		{Result: "success"},
	}, {
		{Result: "success"},
	}}})
	must(t, w.SendAssistant("第一个话题", AssistantContext{Page: "score", WorkID: "abc", WorkTitle: "示例", WorkKind: "score", Locale: schema.UiLocaleZhCN}))
	waitChatIdle(t, w)
	before := w.Store.Read().AssistantChat.SessionID
	must(t, w.ResetAssistant())
	after := w.Store.Read().AssistantChat
	if after.SessionID == before {
		t.Fatal("新对话应换会话 ID")
	}
	divider := after.Messages[len(after.Messages)-1]
	if divider.Role != "divider" {
		t.Fatalf("应存一条分隔：%+v", divider)
	}
	must(t, w.SendAssistant("第二个话题", AssistantContext{}))
	waitChatIdle(t, w)
	raw, err := os.ReadFile(echo)
	must(t, err)
	parts := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n--MSG--\n")
	if len(parts) != 2 {
		t.Fatalf("应收到两条用户消息：%q", raw)
	}
	if strings.Contains(parts[0], "新对话开始") || !strings.Contains(parts[0], "<ui_context>") || !strings.Contains(parts[0], "示例") || !strings.Contains(parts[0], "arrangement") {
		t.Fatalf("首条消息应带界面上下文：%q", parts[0])
	}
	if !strings.Contains(parts[1], "新对话开始") {
		t.Fatalf("新对话后的首条消息应带新话题前缀：%q", parts[1])
	}
}

// 未配置 AI 服务：消息照常入库，本轮立即以 aiNotConfigured 失败。
func TestChatWithoutProvider(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	t.Setenv("YOVOICE_OCHO_PATH", selfExecutable(t))
	must(t, w.SendAssistant("你好", AssistantContext{}))
	messages := waitChatIdle(t, w)
	reply := messages[len(messages)-1]
	if reply.Status != "failed" || reply.ErrorCode != msg.ErrAINotConfigured {
		t.Fatalf("未配置服务应失败：%+v", reply)
	}
}

// 输入校验：空消息与超长消息立即报错。
func TestChatSendValidation(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	if err = w.SendAssistant("  ", AssistantContext{}); err == nil {
		t.Fatal("空消息应报错")
	}
	if err = w.SendAssistant(strings.Repeat("长", 2001), AssistantContext{}); err == nil {
		t.Fatal("超长消息应报错")
	}
}

// 会话记录有上限：超过 200 条时丢弃最旧的。
func TestChatHistoryLimit(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	for i := range schema.AssistantChatLimit + 5 {
		w.appendChat(schema.AssistantMessage{ID: string(rune('a' + i%26)), Role: "user", Text: strings.Repeat("x", i+1), CreatedAt: time.Now()})
	}
	messages := w.Store.Read().AssistantChat.Messages
	if len(messages) != schema.AssistantChatLimit {
		t.Fatalf("应截断到 %d 条，实际 %d", schema.AssistantChatLimit, len(messages))
	}
	if messages[0].Text != strings.Repeat("x", 6) {
		t.Fatalf("应保留最新的消息：%+v", messages[0])
	}
}

// 消息前缀：界面上下文按页面/作品/语言拼装，作品信息优先取状态里的真实值。
func TestChatMessagePrefix(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := schema.DefaultDraft()
	d.Title, d.Kind = "夜色钢琴", "score"
	must(t, w.SaveDraft(d))
	text := w.chatMessageText(chatMessage{text: "改一下", context: AssistantContext{Page: "score", WorkID: d.ID, WorkTitle: "旧标题", Locale: schema.UiLocaleZhCN}}, false)
	if !strings.Contains(text, "page: arrangement") || !strings.Contains(text, `"夜色钢琴"`) || !strings.Contains(text, "kind score") || !strings.Contains(text, "ui_locale: zh-CN") || !strings.HasSuffix(text, "改一下") {
		t.Fatalf("上下文前缀不对：%q", text)
	}
	fresh := w.chatMessageText(chatMessage{text: "新开始", context: AssistantContext{Locale: schema.UiLocaleEn}}, true)
	if !strings.Contains(fresh, "新对话开始") || !strings.Contains(fresh, "ui_locale: en") || !strings.HasSuffix(fresh, "新开始") {
		t.Fatalf("新话题前缀不对：%q", fresh)
	}
}

// 作品工具的创建、读取与事件：create_work 落盘并发跳转事件，get_work 不带时间线。
func TestChatWorkTools(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{})
	_, _, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "编曲错误"})
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatal("无效类型应报错", err)
	}
	_, result, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "score", Title: " 夜曲 "})
	must(t, err)
	id := result.(map[string]any)["id"].(string)
	s := w.Store.Read()
	draft, ok := s.Drafts[0], false
	for _, d := range s.Drafts {
		if d.ID == id {
			draft, ok = d, true
		}
	}
	if !ok || draft.Title != "夜曲" || draft.Kind != "score" || draft.Score == nil || draft.ModelID != "musescore-general-sf2" {
		t.Fatalf("新建的编曲作品不对：%+v", draft)
	}
	if nav := s.AssistantLive.Nav; nav == nil || nav.WorkID != id {
		t.Fatalf("应发出打开作品事件：%+v", s.AssistantLive)
	}
	// get_work：可编辑内容齐全，时间线这类大字段不出现。
	must(t, w.Store.Update(func(s *schema.State) {
		for i := range s.Drafts {
			if s.Drafts[i].ID == id {
				s.Drafts[i].Timeline = &schema.AudioTimeline{Tracks: []schema.AudioLane{{ID: "l", Name: "轨", Clips: []schema.AudioClip{{ID: "c", Start: 1, Duration: 1}}}}}
			}
		}
	}, true))
	_, content, err := w.getWorkTool(context.Background(), toolReq, workIDInput{ID: id})
	must(t, err)
	encoded, _ := json.Marshal(content)
	if strings.Contains(string(encoded), "timeline") || !strings.Contains(string(encoded), "夜曲") {
		t.Fatalf("get_work 内容不对：%s", encoded)
	}
	if _, _, err = w.getWorkTool(context.Background(), toolReq, workIDInput{ID: "missing"}); err == nil || !strings.Contains(err.Error(), "list_works") {
		t.Fatal("未知作品应提示 list_works", err)
	}
}

// update_work：乐谱走同一套校验、错误信息具体；成功后作品更新并发刷新事件。
func TestChatUpdateWorkScore(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{})
	_, created, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "score", Title: "示例小步舞曲"})
	must(t, err)
	id := created.(map[string]any)["id"].(string)
	bad, _ := json.Marshal(map[string]any{"tempo": 100, "timeSignature": []int{4, 4}, "tracks": []any{
		map[string]any{"id": "p", "name": "钢琴", "program": 0, "notes": []any{[]any{1, 99, 60, 1, 80}}},
	}})
	var score scoreInput
	must(t, json.Unmarshal(bad, &score))
	if _, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, Score: &score}); err == nil || !strings.Contains(err.Error(), "beat") {
		t.Fatal("越拍音符应给出具体错误", err)
	}
	good, _ := json.Marshal(map[string]any{"tempo": 96, "timeSignature": []int{4, 4}, "tracks": []any{
		map[string]any{"id": "p", "name": "钢琴", "role": "piano", "program": 0, "notes": []any{[]any{1, 1, 60, 1, 80}, []any{1, 3, 64, 1, 76}}},
	}})
	must(t, json.Unmarshal(good, &score))
	_, summary, err := w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, Score: &score})
	must(t, err)
	if summary.(map[string]any)["notes"] != 2 {
		t.Fatalf("摘要不对：%+v", summary)
	}
	draft := w.Store.Read().Drafts[0]
	if draft.Score == nil || len(draft.Score.Tracks[0].Notes) != 2 || draft.Score.Tempo != 96 {
		t.Fatalf("乐谱未写入：%+v", draft.Score)
	}
	if update := w.Store.Read().AssistantLive.Update; update == nil || update.WorkID != id {
		t.Fatalf("应发出作品刷新事件：%+v", w.Store.Read().AssistantLive)
	}
}

// update_work 的其余字段：歌词纯音乐规则、演唱语言、模型与音色检查、故事配音的角色台词与音色分配。
func TestChatUpdateWorkFields(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{})
	_, created, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "music", Title: "夏夜"})
	must(t, err)
	id := created.(map[string]any)["id"].(string)
	if _, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, Instrumental: boolPtr(true), Lyrics: strPtr("[verse]\n有歌词的行")}); err == nil || !strings.Contains(err.Error(), "section markers only") {
		t.Fatal("纯音乐歌词只允许段落标记", err)
	}
	if _, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, Language: strPtr("xx")}); err == nil || !strings.Contains(err.Error(), "演唱语言") {
		t.Fatal("不支持的演唱语言应报错", err)
	}
	if _, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, ModelID: strPtr("ace-step-1.5-turbo-bf16")}); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatal("未安装的模型应引导用户去设置", err)
	}
	if _, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, VoiceID: strPtr("v1")}); err == nil || !strings.Contains(err.Error(), "list_voices") {
		t.Fatal("未知音色应提示 list_voices", err)
	}
	style, lyrics, language := "city pop", "[verse]\n晚风", "zh"
	_, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: id, Text: &style, Lyrics: &lyrics, Language: &language})
	must(t, err)
	draft := w.Store.Read().Drafts[0]
	if draft.Text != style || draft.Lyrics != lyrics || draft.SynthesisLanguage != "zh" {
		t.Fatalf("音乐字段未写入：%+v", draft)
	}

	// 故事配音：subtitles 整体写入，speakerVoices 按名字分配已有角色。
	_, story, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "story", Title: "双人故事"})
	must(t, err)
	storyID := story.(map[string]any)["id"].(string)
	voiceID := schema.NewID()
	must(t, w.Store.Update(func(s *schema.State) {
		s.Voices = append(s.Voices, schema.Voice{ID: voiceID, Name: "暖声", FileName: "v.wav"})
	}, true))
	badCues := &workSubtitles{
		Speakers: []workSubtitleSpeaker{{Name: "旁白"}, {Name: "主角"}},
		Cues:     []workSubtitleCue{{Speaker: "旁白", Text: "夜里。"}, {Speaker: "没有这个角色", Text: "我来了。"}},
	}
	if _, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: storyID, Subtitles: badCues}); err == nil || !strings.Contains(err.Error(), "speaker") {
		t.Fatal("台词角色不在列表应报错", err)
	}
	_, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: storyID, Subtitles: &workSubtitles{
		Speakers: []workSubtitleSpeaker{{Name: "旁白"}, {Name: "主角"}},
		Cues:     []workSubtitleCue{{Speaker: "旁白", Text: "夜里。"}, {Speaker: "主角", Text: "我来了。"}},
	}})
	must(t, err)
	draft = w.Store.Read().Drafts[0]
	if len(draft.Subtitles.Speakers) != 2 || len(draft.Subtitles.Cues) != 2 || draft.Text != "夜里。\n我来了。" {
		t.Fatalf("故事配音未写入：%+v", draft.Subtitles)
	}
	_, _, err = w.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: storyID, SpeakerVoices: []workSpeakerVoice{{Speaker: "主角", VoiceID: voiceID}}})
	must(t, err)
	draft = w.Store.Read().Drafts[0]
	assigned := draft.Subtitles.Speakers[1].Settings.VoiceID
	if assigned == nil || *assigned != voiceID {
		t.Fatalf("角色音色未分配：%+v", draft.Subtitles.Speakers[1])
	}
}

// generate_work 在缺少推理内核时返回明确错误；open_page 发出页面跳转事件。
func TestChatGenerateAndOpenPage(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{})
	_, created, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "text", Title: "旁白"})
	must(t, err)
	id := created.(map[string]any)["id"].(string)
	if _, _, err = w.generateWorkTool(context.Background(), toolReq, workIDInput{ID: id}); err == nil {
		t.Fatal("缺少推理内核应报错", err)
	} else {
		var ce *msg.CallError
		if !errors.As(err, &ce) {
			t.Fatal("应是带错误码的失败", err)
		}
	}
	_, _, err = w.openPageTool(context.Background(), toolReq, pageInput{Page: "编曲"})
	if err == nil || !strings.Contains(err.Error(), "page") {
		t.Fatal("无效页面应报错", err)
	}
	_, _, err = w.openPageTool(context.Background(), toolReq, pageInput{Page: "score"})
	must(t, err)
	if nav := w.Store.Read().AssistantLive.Nav; nav == nil || nav.Page != "score" {
		t.Fatalf("应发出页面跳转事件：%+v", w.Store.Read().AssistantLive)
	}
}

// 端到端：假 ocho 在对话里先新建编曲作品、再按已知 id 写入乐谱，界面事件与消息卡片齐全。
func TestChatEndToEndCreateAndUpdate(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{})
	// 先直接建好一个编曲作品，拿到 id 写进计划（假 ocho 的计划在启动前固定）。
	_, created, err := w.createWorkTool(context.Background(), toolReq, createWorkInput{Kind: "score", Title: "示例小步舞曲"})
	must(t, err)
	id := created.(map[string]any)["id"].(string)
	args, mErr := json.Marshal(map[string]any{"id": id, "score": map[string]any{
		"tempo": 100, "timeSignature": []int{4, 4},
		"tracks": []any{map[string]any{"id": "p", "name": "钢琴", "role": "piano", "program": 0,
			"notes": []any{[]any{1, 1, 60, 1, 80}, []any{1, 3, 64, 1, 76}}}},
	}})
	must(t, mErr)
	turns := [][]fakeocho.Step{{
		{Call: "list_works", Args: json.RawMessage(`{}`)},
		{Call: "get_work", Args: json.RawMessage(`{"id":"` + id + `"}`)},
		{Call: "update_work", Args: args},
		{Result: "success"},
	}}
	data, mErr := json.Marshal(fakeocho.Plan{Turns: turns})
	must(t, mErr)
	planPath := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(planPath, data, 0600))
	w.assistant = assistant.New(w.Store.Root, assistant.Options{Tools: assistantTools, NewServer: w.newAssistantServer,
		ExtraEnv: []string{fakeocho.PlanEnv + "=" + planPath}})
	must(t, w.SendAssistant("给这个作品写四小节钢琴旋律", AssistantContext{Page: "score", WorkID: id, WorkTitle: "示例小步舞曲", WorkKind: "score", Locale: schema.UiLocaleZhCN}))
	messages := waitChatIdle(t, w)
	reply := messages[len(messages)-1]
	if reply.Status != "completed" {
		t.Fatalf("本轮应完成：%+v", reply)
	}
	var notes []schema.ScoreNote
	for _, d := range w.Store.Read().Drafts {
		if d.ID == id && d.Score != nil && len(d.Score.Tracks) > 0 {
			notes = d.Score.Tracks[0].Notes
		}
	}
	if len(notes) != 2 {
		t.Fatalf("乐谱未写入：%+v", notes)
	}
	if len(reply.Tools) != 3 || reply.Tools[2].Tool != "update_work" || reply.Tools[2].State != "done" {
		t.Fatalf("工具卡片不对：%+v", reply.Tools)
	}
	live := w.Store.Read().AssistantLive
	if live.Update == nil || live.Update.WorkID != id {
		t.Fatalf("应发出作品刷新事件：%+v", live)
	}
}

// waitFor 轮询状态直到条件满足或超时。
func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("等待超时")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
