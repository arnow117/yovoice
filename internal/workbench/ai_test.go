package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 服务配置写入状态，密钥只在状态库的私有设置中；清除与删除同步移除密钥。
func TestAIProviderKeysStayOutOfState(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	key := "sk-very-secret-key-000"
	p, err := w.SaveAIProvider(AIProviderInput{Preset: "deepseek", BaseURL: "http://evil.test", Format: "responses", Key: &key, Model: "deepseek-chat"})
	must(t, err)
	if p.BaseURL != "https://api.deepseek.com" || p.Format != "chat_completions" || p.KeyMask != "sk-v••••••••-000" {
		t.Fatalf("固定预设应忽略地址与协议：%+v", p)
	}
	if w.Store.Read().AIProviderID != p.ID {
		t.Fatal("第一个服务应成为当前服务")
	}
	if state, _ := json.Marshal(w.Store.Read()); strings.Contains(string(state), key) {
		t.Fatal("密钥不应出现在广播给界面的状态中")
	}
	info, err := os.Stat(filepath.Join(w.Store.Root, "yovoice.db"))
	must(t, err)
	if info.Mode().Perm()&0077 != 0 && os.PathSeparator == '/' {
		t.Fatal("状态库权限应为 0600", info.Mode())
	}
	// 不传 Key 时保留密钥。
	p, err = w.SaveAIProvider(AIProviderInput{ID: p.ID, Preset: "deepseek", Name: "我的 DeepSeek", Model: "deepseek-reasoner"})
	must(t, err)
	if _, c, _ := w.aiConfig(""); c.Key != key || p.Name != "我的 DeepSeek" {
		t.Fatal("未传密钥时应保留原密钥")
	}
	if _, err = w.SaveAIProvider(AIProviderInput{Preset: "custom", BaseURL: "ftp://x", Format: "chat_completions"}); err == nil {
		t.Fatal("应拒绝非 http 地址")
	}
	local, err := w.SaveAIProvider(AIProviderInput{Preset: "ollama", BaseURL: "http://192.168.1.5:11434/v1", Format: "anthropic_messages"})
	must(t, err)
	if local.BaseURL != "http://192.168.1.5:11434/v1" || local.Format != "chat_completions" {
		t.Fatalf("本地服务只允许改地址：%+v", local)
	}
	// 写作栏切换服务与模型：同时改当前服务和它的模型，模型留空时保持原值。
	must(t, w.UseAIProvider(local.ID, "qwen3:8b"))
	must(t, w.UseAIProvider(p.ID, ""))
	if s := w.Store.Read(); s.AIProviderID != p.ID || s.AIProviders[0].Model != "deepseek-reasoner" || s.AIProviders[1].Model != "qwen3:8b" {
		t.Fatalf("切换服务与模型错误：%+v", s.AIProviders)
	}
	if w.UseAIProvider("missing", "") == nil {
		t.Fatal("应拒绝不存在的服务")
	}
	must(t, w.DeleteAIProvider(p.ID))
	keys, err := w.readSecrets()
	must(t, err)
	if _, ok := keys[p.ID]; ok || w.Store.Read().AIProviderID != local.ID {
		t.Fatal("删除服务应移除密钥并切换当前服务")
	}
}

// fakeLLM 返回一个可用的模型列表与连通测试服务。
func fakeLLM(t *testing.T) *httptest.Server {
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3"},{"id":"llama3"}]}`))
			return
		}
		n.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// aiWorkbench 准备一个指向假 LLM 服务的 Workbench；助手运行时指向测试二进制（假 ocho）。
func aiWorkbench(t *testing.T, plan fakeocho.Plan) *Workbench {
	w, err := New(t.TempDir())
	must(t, err)
	t.Cleanup(w.Close)
	_, err = w.SaveAIProvider(AIProviderInput{Preset: "ollama", BaseURL: fakeLLM(t).URL + "/v1"})
	must(t, err)
	_, err = w.AIModels(context.Background(), "")
	must(t, err)
	t.Setenv("YOVOICE_OCHO_PATH", selfExecutable(t))
	data, err := json.Marshal(plan)
	must(t, err)
	path := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(path, data, 0600))
	t.Setenv(fakeocho.PlanEnv, path)
	return w
}

func selfExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	must(t, err)
	return executable
}

// 模型列表与连通测试的结果写回服务。
func TestAIModelsAndTest(t *testing.T) {
	server := fakeLLM(t)
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	_, err = w.SaveAIProvider(AIProviderInput{Preset: "ollama", BaseURL: server.URL + "/v1"})
	must(t, err)
	id := w.Store.Read().AIProviderID
	models, err := w.AIModels(context.Background(), id)
	must(t, err)
	p := w.Store.Read().AIProviders[0]
	if len(models) != 2 || p.Model != "llama3" || len(p.Models) != 2 {
		t.Fatalf("应保存模型列表并选中第一个：%+v", p)
	}
	result, err := w.TestAIProvider(context.Background(), id)
	must(t, err)
	if !result.OK || w.Store.Read().AIProviders[0].LastTest == nil {
		t.Fatal("测试结果应记录在服务上", result)
	}
}

// scoreArgs 组装 submit_score 的工具参数。
func scoreArgs(bad bool) json.RawMessage {
	beat := 1.0
	if bad {
		beat = 99 // 超出每小节拍数，submit_score 应拒绝。
	}
	data, _ := json.Marshal(map[string]any{
		"title": " 暖光钢琴 ", "tempo": 100, "timeSignature": []int{4, 4}, "key": "C 大调",
		"sections": []any{map[string]any{"name": "主歌", "start": 1, "end": 2}},
		"tracks": []any{
			map[string]any{"id": "p", "name": "钢琴", "role": "piano", "program": 0,
				"notes": []any{[]any{1, 1, 60, 1, 80}, []any{2, beat, 64, 0.5, 70}}},
			map[string]any{"id": "d", "name": "鼓", "role": "drums", "program": 0, "drums": true,
				"notes": []any{map[string]any{"bar": 1, "beat": 1, "pitch": 36, "length": 0.25, "velocity": 100}}},
		},
	})
	return data
}

// AI 写谱：助手先提交一份不合规乐谱被拒，按错误修正后再次提交成功。
func TestComposeScoreSubmitsAndRepairs(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "get_draft", Args: json.RawMessage(`{}`)},
		{Call: "submit_score", Args: scoreArgs(true)},
		{Call: "submit_score", Args: scoreArgs(false)},
		{Result: "success"},
	}}})
	task, err := w.StartScore(ScoreBrief{ProjectID: "p1", Brief: "温暖的钢琴小品", Seconds: 10, Locale: schema.UiLocaleZhCN})
	must(t, err)
	done := waitAI(t, w)
	if done.Status != "completed" || done.Stage != "repairing" {
		t.Fatalf("任务应完成且阶段为 repairing：%+v", done)
	}
	var score ComposedScore
	must(t, json.Unmarshal(done.Result, &score))
	if score.Title != "暖光钢琴" || len(score.Tracks) != 2 || score.Tracks[0].Notes[1].Beat != 1 || !score.Tracks[1].Drums {
		t.Fatalf("提交的乐谱不对：%+v", score)
	}
	if done.ID != task.ID || done.ProjectID != "p1" {
		t.Fatalf("任务身份不对：%+v", done)
	}
	must(t, w.DismissAI(done.ID))
}

// 本轮没有成功提交时返回 aiResultInvalid。
func TestComposeScoreWithoutSubmission(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Assistant: "我写不了"},
		{Result: "success"},
	}}})
	_, err := w.ComposeScore(context.Background(), ScoreBrief{Brief: "x"})
	var ce *msg.CallError
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResultInvalid {
		t.Fatal("无提交应返回 aiResultInvalid", err)
	}
}

// AI 写词：submit_lyrics 提交后取回结果；未配置服务与 style 缺失各有明确错误。
func TestWriteLyrics(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	var ce *msg.CallError
	t.Setenv("YOVOICE_OCHO_PATH", selfExecutable(t))
	if _, err = w.WriteLyrics(context.Background(), LyricsBrief{Brief: "夏天"}); !errors.As(err, &ce) || ce.Code != msg.ErrAINotConfigured {
		t.Fatal("未配置服务时应返回 aiNotConfigured", err)
	}
	w = aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "submit_lyrics", Args: json.RawMessage(`{"title":"夏夜","style":"city pop, warm synths","lyrics":"[verse]\n晚风吹过","language":"xx"}`)},
		{Result: "success"},
	}}})
	l, err := w.WriteLyrics(context.Background(), LyricsBrief{Brief: "夏天的晚风", Language: "zh"})
	must(t, err)
	if l.Title != "夏夜" || l.Style != "city pop, warm synths" || !strings.HasPrefix(l.Lyrics, "[verse]") || l.Language != "" {
		t.Fatalf("%+v", l)
	}
	// style 必填：被拒后没有再提交，返回 aiResultInvalid。
	w = aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "submit_lyrics", Args: json.RawMessage(`{"title":"无风格","lyrics":"[verse]\n风"}`)},
		{Result: "success"},
	}}})
	_, err = w.WriteLyrics(context.Background(), LyricsBrief{Brief: "x"})
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResultInvalid {
		t.Fatal("style 缺失应被拒绝并最终 aiResultInvalid", err)
	}
}

// 纯音乐时歌词只允许段落标记，带唱词的提交被拒绝。
func TestSubmitLyricsInstrumentalMarkersOnly(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "submit_lyrics", Args: json.RawMessage(`{"title":"夜航","style":"ambient, warm pad","lyrics":"[intro]\n[verse]\n有歌词的行","language":"zh"}`)},
		{Result: "success"},
	}}})
	_, err := w.WriteLyrics(context.Background(), LyricsBrief{Brief: "夜航", Instrumental: true})
	var ce *msg.CallError
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResultInvalid {
		t.Fatal("纯音乐带唱词应被拒绝", err)
	}
}

// 后台任务：立即返回运行中的任务，完成后结果写回状态，界面取用后清除。
func TestAITaskCompletesInBackground(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "submit_lyrics", Args: json.RawMessage(`{"title":"夏夜","style":"city pop","lyrics":"[verse]\n晚风","language":"zh"}`)},
		{Result: "success"},
	}}})
	task, err := w.StartLyrics(LyricsBrief{ProjectID: "p1", Brief: "夏天"})
	must(t, err)
	if task.Status != "running" || task.ProjectID != "p1" || task.Model != "llama3" || task.Timeout != schema.AITimeoutDefault || task.Stage != "writing" {
		t.Fatalf("应返回运行中的任务：%+v", task)
	}
	done := waitAI(t, w)
	var l Lyrics
	if done.Status != "completed" || json.Unmarshal(done.Result, &l) != nil || l.Title != "夏夜" || done.FinishedAt == nil {
		t.Fatalf("结果应写回状态：%+v", done)
	}
	must(t, w.DismissAI(done.ID))
	if w.Store.Read().AITask != nil {
		t.Fatal("取用后应清除任务")
	}
	var ce *msg.CallError
	if _, err = w.StartScore(ScoreBrief{Brief: " "}); !errors.As(err, &ce) || ce.Code != msg.ErrRequestInvalid {
		t.Fatal("无效输入应立即报错", err)
	}
}

// 循环段落只写一遍，由 repeats 展开；展开后超出音符上限要报错。
func TestBuildScoreRepeats(t *testing.T) {
	in := submitScoreInput{}
	in.Tempo, in.TimeSignature = 120, []int{4, 4}
	in.Tracks = append(in.Tracks, submitScoreTrack{ID: "d", Name: "鼓", Role: "drums", Notes: []any{
		[]any{float64(1), float64(1), float64(36), float64(1), float64(100)},
		[]any{float64(2), float64(3), float64(38), float64(1), float64(90)},
		[]any{float64(5), float64(1), float64(49), float64(2), float64(110)},
	}, Repeats: []scoreRepeat{{From: 1, To: 2, At: []int{3, 9}}}})
	s, err := buildScore(in)
	must(t, err)
	notes := s.Tracks[0].Notes
	if len(notes) != 7 || notes[2].Bar != 3 || notes[3].Bar != 4 || notes[3].Beat != 3 || notes[4].Bar != 5 || notes[6].Bar != 10 {
		t.Fatalf("展开结果不对：%+v", notes)
	}
	many, at := make([]any, 0, 400), make([]int, 0, 60)
	for i := range 400 {
		many = append(many, []any{float64(1), float64(1) + float64(i)/100, float64(60), float64(0.01), float64(80)})
	}
	for i := range 60 {
		at = append(at, i+2)
	}
	in.Tracks[0].Notes, in.Tracks[0].Repeats = many, []scoreRepeat{{From: 1, To: 1, At: at}}
	if _, err = buildScore(in); err == nil || !strings.Contains(err.Error(), "after repeats") {
		t.Fatal("展开超出上限应报错", err)
	}
}

// waitAI 等待后台任务结束并返回最终状态。
func waitAI(t *testing.T, w *Workbench) schema.AITask {
	t.Helper()
	changes, stop := w.Store.Subscribe()
	defer stop()
	deadline := time.After(30 * time.Second)
	for {
		if task := w.Store.Read().AITask; task != nil && task.Status != "running" {
			return *task
		}
		select {
		case <-changes:
		case <-deadline:
			t.Fatal("AI 任务没有结束")
		}
	}
}

// 取消会中止正在进行的任务，且同一时间只允许一个 AI 任务。
func TestCancelAI(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{WaitInterrupt: true},
	}, {
		{Call: "submit_lyrics", Args: json.RawMessage(`{"title":"夏夜","style":"city pop","lyrics":"[verse]\n晚风"}`)},
		{Result: "success"},
	}}})
	_, err := w.StartLyrics(LyricsBrief{Brief: "x"})
	must(t, err)
	var ce *msg.CallError
	if _, err = w.StartLyrics(LyricsBrief{Brief: "y"}); !errors.As(err, &ce) || ce.Code != msg.ErrBusy {
		t.Fatal("并发请求应返回 busy", err)
	}
	w.CancelAI()
	if task := waitAI(t, w); task.Status != "cancelled" {
		t.Fatal("取消后任务应为 cancelled", task)
	}
	if _, err = w.StartLyrics(LyricsBrief{Brief: "z"}); err != nil {
		t.Fatal("结束后应能再次开始", err)
	}
}

// 超时按设置计时，失败原因带上秒数；ocho 缺失时 startAI 直接返回 assistantMissing。
func TestAITimeoutAndMissingOcho(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{SleepMS: 5000},
	}}})
	p := w.Store.Read().Preferences
	p.AITimeout = 1
	if err := w.SavePreferences(p); err == nil {
		t.Fatal("超时低于下限应拒绝")
	}
	p.AITimeout = schema.AITimeoutMin
	must(t, w.SavePreferences(p))
	if aiTimeout(w.Store.Read().Preferences) != time.Duration(schema.AITimeoutMin)*time.Second {
		t.Fatal("应使用设置的超时")
	}
	// 测试里不等 30 秒：用更短的父级截止时间验证错误转换。
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := w.ComposeScore(ctx, ScoreBrief{Brief: "x"})
	var ce *msg.CallError
	if !errors.As(err, &ce) || ce.Code != msg.ErrAITimeout {
		t.Fatal("超时应返回 aiTimeout", err)
	}
	// ocho 缺失：开始任务前直接报错，不创建任务。
	w2 := aiWorkbench(t, fakeocho.Plan{})
	w2.Store.Update(func(s *schema.State) {}, false)
	t.Setenv("YOVOICE_OCHO_PATH", filepath.Join(t.TempDir(), "missing-ocho"))
	if _, err = w2.StartScore(ScoreBrief{Brief: "x"}); !errors.As(err, &ce) || ce.Code != msg.ErrAssistantMissing {
		t.Fatal("ocho 缺失应返回 assistantMissing", err)
	}
	if w2.Store.Read().AITask != nil {
		t.Fatal("ocho 缺失时不应创建任务")
	}
}

// 继续修改：followUp 与草稿进 get_draft，助手在同一上下文里再次提交。
func TestComposeScoreFollowUp(t *testing.T) {
	w := aiWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "submit_score", Args: scoreArgs(false)},
		{Result: "success"},
	}, {
		{Call: "get_draft", Args: json.RawMessage(`{}`)},
		{Call: "submit_score", Args: scoreArgs(false)},
		{Result: "success"},
	}}})
	_, err := w.ComposeScore(context.Background(), ScoreBrief{Brief: "温暖的钢琴小品"})
	must(t, err)
	draft := &schema.Score{Tempo: 100, TimeSignature: []int{4, 4}}
	second, err := w.ComposeScore(context.Background(), ScoreBrief{Brief: "温暖的钢琴小品", FollowUp: "节奏再轻快一些", Draft: draft})
	must(t, err)
	if second.Title != "暖光钢琴" {
		t.Fatalf("继续修改的结果不对：%+v", second)
	}
}

// get_draft 返回任务上下文与当前草稿。
func TestAIDraftContext(t *testing.T) {
	score := schema.Score{Tempo: 100, TimeSignature: []int{4, 4}}
	d := &aiDraft{kind: "score", taskID: "t1", projectID: "p1", brief: "夜色", followUp: "再轻快些", seconds: 30, locale: schema.UiLocaleZhCN, scoreDraft: &score}
	context := d.context()
	task := context["task"].(map[string]any)
	if task["id"] != "t1" || task["kind"] != "score" || task["followUp"] != "再轻快些" || context["seconds"] != 30 || context["uiLocale"] != schema.UiLocaleZhCN {
		t.Fatalf("写谱上下文不对：%+v", context)
	}
	l := &aiDraft{kind: "lyrics", taskID: "t2", brief: "夏夜", instrumental: true, musicLanguage: "zh", lyricsDraft: &Lyrics{Title: "夏夜", Style: "city pop"}}
	context = l.context()
	if context["instrumental"] != true || context["language"] != "zh" || context["lyricsDraft"].(Lyrics).Title != "夏夜" {
		t.Fatalf("写词上下文不对：%+v", context)
	}
}
