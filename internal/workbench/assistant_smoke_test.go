package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yovoice/internal/schema"
)

// 真实冒烟（本机可选）：设置 ANTHROPIC_BASE_URL、ANTHROPIC_API_KEY、YOVOICE_SMOKE_MODEL 与
// YOVOICE_OCHO_PATH 后运行，用临时数据目录跑一次写谱与写词并打印结果摘要；未设置时跳过。
func TestSmokeAssistantCompose(t *testing.T) {
	model := os.Getenv("YOVOICE_SMOKE_MODEL")
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	key := os.Getenv("ANTHROPIC_API_KEY")
	executable := os.Getenv("YOVOICE_OCHO_PATH")
	if model == "" || baseURL == "" || key == "" || executable == "" {
		t.Skip("未设置 ANTHROPIC_BASE_URL/ANTHROPIC_API_KEY/YOVOICE_SMOKE_MODEL/YOVOICE_OCHO_PATH，跳过真实冒烟")
	}
	if _, err := os.Stat(executable); err != nil {
		t.Skipf("ocho 不可用：%v", err)
	}
	root, err := os.MkdirTemp("", "yovoice-smoke-*")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	w, err := New(root)
	must(t, err)
	t.Cleanup(w.Close)
	_, err = w.SaveAIProvider(AIProviderInput{Preset: "custom", Name: "冒烟", Format: "anthropic_messages", BaseURL: baseURL, Key: &key, Model: model})
	must(t, err)
	t.Setenv("YOVOICE_OCHO_PATH", executable)

	wait := func(t *testing.T) schema.AITask {
		t.Helper()
		deadline := time.After(10 * time.Minute)
		for {
			if task := w.Store.Read().AITask; task != nil && task.Status != "running" {
				return *task
			}
			select {
			case <-time.After(500 * time.Millisecond):
			case <-deadline:
				t.Fatal("冒烟任务超时未结束")
			}
		}
	}

	t.Run("score", func(t *testing.T) {
		_, err := w.StartScore(ScoreBrief{Brief: "温暖的钢琴小品，带一点弦乐铺底", Seconds: 20, Locale: schema.UiLocaleZhCN})
		must(t, err)
		task := wait(t)
		if task.Status != "completed" {
			t.Fatalf("写谱任务失败：%+v", task)
		}
		var score ComposedScore
		must(t, json.Unmarshal(task.Result, &score))
		notes := 0
		for _, track := range score.Tracks {
			notes += len(track.Notes)
		}
		t.Logf("写谱完成：%q tempo=%g %d 小节 %d 声部 %d 音符 时长约 %.0fs 阶段=%s", score.Title, score.Tempo, score.Bars(), len(score.Tracks), notes, score.Duration(), task.Stage)
	})

	t.Run("lyrics", func(t *testing.T) {
		_, err := w.StartLyrics(LyricsBrief{Brief: "夏夜城市里的一段晚风", Language: "zh", Locale: schema.UiLocaleZhCN})
		must(t, err)
		task := wait(t)
		if task.Status != "completed" {
			t.Fatalf("写词任务失败：%+v", task)
		}
		var lyrics Lyrics
		must(t, json.Unmarshal(task.Result, &lyrics))
		t.Logf("写词完成：%q style=%q language=%s 歌词 %d 字 阶段=%s", lyrics.Title, lyrics.Style, lyrics.Language, len([]rune(lyrics.Lyrics)), task.Stage)
	})

	// 冒烟结束后保留日志便于排查：打印助手子进程日志的尾部。
	if log, err := os.ReadFile(filepath.Join(root, "logs", "assistant.log")); err == nil {
		tail := string(log)
		if r := []rune(tail); len(r) > 1500 {
			tail = string(r[len(r)-1500:])
		}
		t.Logf("assistant.log 尾部：\n%s", tail)
	}
}
