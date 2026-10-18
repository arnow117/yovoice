package workbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"yovoice/internal/assistant"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 上下文前缀带上选中内容、当前台词、播放位置、作品模型与最近一次生成的错误码。
func TestChatMessageRichContext(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := schema.DefaultDraft()
	d.Title, d.ModelID = "开场旁白", "index-2.5-q8"
	must(t, w.SaveDraft(d))
	code := msg.ErrGenerateFailed
	must(t, w.Store.Update(func(s *schema.State) {
		s.Activity = &schema.Activity{ProjectID: d.ID, Kind: "generate", Status: "failed", ErrorCode: &code, ErrorParams: msg.Params{"detail": strings.Repeat("长", 300)}}
		s.History = append([]schema.Generation{{ID: "g1", Title: "开场旁白", Duration: 12.5, CreatedAt: time.Now().Add(-3 * time.Minute), Settings: schema.Draft{ID: d.ID}}}, s.History...)
	}, true))
	text := w.chatMessageText(chatMessage{text: "这句太快了", context: AssistantContext{Page: "story", WorkID: d.ID,
		Selection: "  夜色\n很深 ", Cue: &AssistantCue{Index: 3, Total: 12, Speaker: "旁白", Text: "风停了。"}, Playhead: 83}})
	for _, want := range []string{`selected text: "夜色 很深"`, `selected line: #3 of 12, speaker "旁白": "风停了。"`, "playhead: 1:23",
		"work model: index-2.5-q8 (not installed)", "last generation: failed, error error.generateFailed", `latest take: "开场旁白", 12.5s, 3 min ago (id g1)`} {
		if !strings.Contains(text, want) {
			t.Fatalf("上下文缺少 %q：\n%s", want, text)
		}
	}
	if strings.Count(text, "长") > 130 {
		t.Fatal("错误参数应截断")
	}
}

// 偏好记忆：去重、上限、删除与清空；记住的偏好随上下文带上，工具卡片显示内容。
func TestAssistantMemory(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	if _, err = w.RememberAssistant("  "); err == nil {
		t.Fatal("空偏好应被拒绝")
	}
	first, err := w.RememberAssistant("旁白默认用 温柔女声")
	must(t, err)
	for i := range schema.AssistantMemoLimit {
		_, err = w.RememberAssistant("偏好 " + string(rune('A'+i)))
		must(t, err)
	}
	memory := w.Store.Read().AssistantMemory
	if len(memory) != schema.AssistantMemoLimit || memory[0].ID == first.ID {
		t.Fatalf("超过上限应丢弃最旧的一条：%d", len(memory))
	}
	again, err := w.RememberAssistant("偏好 C")
	must(t, err)
	memory = w.Store.Read().AssistantMemory
	if len(memory) != schema.AssistantMemoLimit || memory[len(memory)-1].ID != again.ID {
		t.Fatal("重复的偏好应移到最新而不是新增")
	}
	text := w.chatMessageText(chatMessage{text: "你好"})
	if !strings.Contains(text, "remembered preferences:") || !strings.Contains(text, "["+again.ID+"] 偏好 C") {
		t.Fatalf("上下文应带上偏好：%s", text)
	}
	_, _, err = w.forgetTool(context.Background(), toolReq, struct {
		ID string `json:"id" jsonschema:"偏好 ID"`
	}{ID: again.ID})
	must(t, err)
	if len(w.Store.Read().AssistantMemory) != schema.AssistantMemoLimit-1 {
		t.Fatal("forget_preference 应删除一条")
	}
	must(t, w.ForgetAssistant(""))
	if len(w.Store.Read().AssistantMemory) != 0 {
		t.Fatal("清空后不应留下偏好")
	}
}

// 提议：参数校验、确认卡片、用户确认后执行，已处理的提议不能重复执行。
func TestAssistantProposalFlow(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{})
	keep, gone := schema.DefaultDraft(), schema.DefaultDraft()
	keep.Title, gone.Title = "保留", "草稿二"
	must(t, w.SaveDraft(keep))
	must(t, w.SaveDraft(gone))
	before := len(w.Store.Read().Drafts)
	for _, bad := range []proposeInput{{Action: "delete_work", Target: "missing"}, {Action: "format_disk"}, {Action: "install_model", Target: "nope"}, {Action: "set_download_source", Value: "ftp"}} {
		if _, _, err := w.proposeTool(context.Background(), toolReq, bad); err == nil {
			t.Fatalf("无效提议应被拒绝：%+v", bad)
		}
	}
	args, _ := json.Marshal(map[string]any{"action": "delete_work", "target": gone.ID, "reason": "重复的草稿"})
	dismissArgs, _ := json.Marshal(map[string]any{"action": "delete_work", "target": keep.ID})
	turns := [][]fakeocho.Step{{{Call: "propose_action", Args: args}, {Call: "propose_action", Args: dismissArgs}, {Result: "success"}}}
	data, _ := json.Marshal(fakeocho.Plan{Turns: turns})
	planPath := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(planPath, data, 0600))
	w.assistant = assistant.New(w.Store.Root, assistant.Options{Tools: assistantTools, NewServer: w.newAssistantServer, ExtraEnv: []string{fakeocho.PlanEnv + "=" + planPath}})
	must(t, w.SendAssistant("把草稿二删掉", AssistantContext{Locale: schema.UiLocaleZhCN}))
	messages := waitChatIdle(t, w)
	cards := replyCards(messages[len(messages)-1])
	if len(cards) != 2 || cards[0].Proposal == nil || cards[1].Proposal == nil {
		t.Fatalf("应有两张提议卡片：%+v", cards)
	}
	proposal := cards[0].Proposal
	if proposal.State != "pending" || proposal.Label != "草稿二" || proposal.Reason != "重复的草稿" || !strings.Contains(cards[0].Summary, "草稿二") {
		t.Fatalf("提议内容不对：%+v %q", proposal, cards[0].Summary)
	}
	if len(w.Store.Read().Drafts) != before {
		t.Fatal("确认之前不应删除")
	}
	must(t, w.ResolveAssistantProposal(proposal.ID, true))
	must(t, w.ResolveAssistantProposal(cards[1].Proposal.ID, false))
	if err := w.ResolveAssistantProposal(proposal.ID, true); err == nil {
		t.Fatal("已处理的提议不能再次执行")
	}
	drafts := w.Store.Read().Drafts
	if len(drafts) != before-1 || slices.ContainsFunc(drafts, func(d schema.Draft) bool { return d.ID == gone.ID }) {
		t.Fatalf("确认后只删除提议的作品：%+v", drafts)
	}
	cards = replyCards(currentMessages(w)[len(currentMessages(w))-1])
	if cards[0].Proposal.State != "confirmed" || cards[1].Proposal.State != "dismissed" {
		t.Fatalf("提议状态应写回会话：%+v %+v", cards[0].Proposal, cards[1].Proposal)
	}
}

// 确认时对象已不存在：提议标记为失败而不是执行到别的对象上。
func TestAssistantProposalStale(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := schema.DefaultDraft()
	must(t, w.SaveDraft(d))
	p := schema.AssistantProposal{ID: "p1", Action: "delete_work", Target: d.ID, Label: d.Title, State: "pending"}
	must(t, w.Store.Update(func(s *schema.State) {
		s.AssistantSessions = &schema.AssistantSessions{CurrentID: "s", Sessions: []schema.AssistantSession{{ID: "s", Messages: []schema.AssistantMessage{{ID: "m", Role: "assistant",
			Blocks: []schema.AssistantBlock{{Kind: "tool", Card: &schema.AssistantToolCard{Tool: "propose_action", State: "done", Proposal: &p}}}}}}}}
	}, true))
	must(t, w.DeleteDraft(d.ID))
	must(t, w.ResolveAssistantProposal("p1", true))
	if got := w.findProposal("p1"); got == nil || got.State != "failed" || got.ErrorCode != msg.ErrRequestInvalid {
		t.Fatalf("过期的提议应标记失败：%+v", got)
	}
}

// 错误日志交给模型前去掉本机路径并截断长文本。
func TestSafeFields(t *testing.T) {
	fields := safeFields(map[string]any{"path": filepath.Join(string(filepath.Separator)+"Users", "me", "voice.wav"), "code": "@yovoice.error.busy", "detail": strings.Repeat("x", 500), "count": 3})
	if fields["path"] != "voice.wav" || fields["code"] != "error.busy" || len([]rune(fields["detail"].(string))) != 241 || fields["count"] != 3 {
		t.Fatalf("字段处理不对：%+v", fields)
	}
}
