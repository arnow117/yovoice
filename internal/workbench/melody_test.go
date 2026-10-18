package workbench

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
	"yovoice/internal/testkit"
)

func yueWorkbench(t *testing.T) (*Workbench, schema.Draft) {
	wb, err := New(t.TempDir())
	must(t, err)
	t.Cleanup(wb.Close)
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(wb.Store.Root, "models", "Yue2-3B-GGUF")
	must(t, os.MkdirAll(model, 0700))
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "yue2-3b-q8", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Title, d.Text, d.Lyrics = "music", "yue2-3b-q8", "夏夜", "Mandarin, city pop", "[verse]\n晚风"
	return wb, d
}

// 先出谱：结果放在会话状态里等界面填入，作品里已有的谱不被覆盖。
func TestPlanMelody(t *testing.T) {
	wb, d := yueWorkbench(t)
	d.Abc = "旧谱"
	must(t, wb.Store.Update(func(s *schema.State) { s.Drafts = []schema.Draft{d} }, true))
	must(t, wb.PlanMelody(d))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity.Status != "completed" || state.Activity.Task != "melody" || state.Melody == nil || state.Melody.Abc != testkit.ABC || state.Melody.ProjectID != d.ID || state.Melody.Source != "plan" {
		t.Fatalf("%+v %+v", state.Activity, state.Melody)
	}
	if len(state.History) != 0 || state.Drafts[0].Abc != "旧谱" {
		t.Fatal("出谱不产生版本，也不改写作品里的谱")
	}
	must(t, wb.DismissMelody(state.Melody.ID))
	if wb.Store.Read().Melody != nil {
		t.Fatal("取用后应清除")
	}
}

// 直接生成时引擎附带这次所用的谱，存进版本；按谱生成时版本保存的是作品里的谱。
func TestGenerationKeepsMelody(t *testing.T) {
	wb, d := yueWorkbench(t)
	must(t, wb.Generate(d))
	<-wb.Done()
	if h := wb.Store.Read().History; len(h) != 1 || h[0].Settings.Abc != testkit.ABC {
		t.Fatalf("%+v", h)
	}
	if wb.Store.Read().Drafts[0].Abc != "" {
		t.Fatal("作品本身的谱不被引擎结果改写")
	}
	d.Abc, _ = score.StripABCChords(testkit.ABC)
	must(t, wb.Generate(d))
	<-wb.Done()
	if h := wb.Store.Read().History; len(h) != 2 || h[0].Settings.Abc != d.Abc {
		t.Fatalf("%+v", h[0].Settings.Abc)
	}
	var ce *msg.CallError
	d.Abc = strings.Replace(testkit.ABC, "c16|", "c8|", 1)
	if err := wb.Generate(d); !errors.As(err, &ce) || ce.Code != msg.ErrMelodyInvalid || ce.Params["bar"] != 1 {
		t.Fatal(err)
	}
}

func TestCoverRequiresModel(t *testing.T) {
	wb, d := yueWorkbench(t)
	var ce *msg.CallError
	if err := wb.CoverMelody(d.ID, bytes.NewReader(wav())); !errors.As(err, &ce) || ce.Code != msg.ErrCoverModelRequired {
		t.Fatal(err)
	}
	if err := wb.CoverMelody("bad", bytes.NewReader(wav())); !errors.As(err, &ce) || ce.Code != msg.ErrDraftIDInvalid {
		t.Fatal(err)
	}
}

func TestInspectAndTransformMelody(t *testing.T) {
	in := InspectMelody(testkit.ABC)
	if in.Issue != nil || in.Tempo != 120 || in.Bars != 2 || in.Seconds != 4 || !in.Chords || in.Key != "C major" || len(in.Score.Tracks) != 3 {
		t.Fatalf("%+v", in)
	}
	if bad := InspectMelody(strings.Replace(testkit.ABC, "c16|", "c8|", 1)); bad.Issue == nil || bad.Issue.Bar != 1 || bad.Issue.Voice != "Vocal" || bad.Score != nil {
		t.Fatalf("%+v", bad)
	}
	out, err := TransformMelody(testkit.ABC, "tempo", 90)
	must(t, err)
	if !strings.Contains(out, "Q:1/4=90") {
		t.Fatal(out)
	}
	if out, err = TransformMelody(out, "stripChords", 0); err != nil || InspectMelody(out).Chords {
		t.Fatal(out, err)
	}
	if _, err = TransformMelody(out, "transpose", 0); err == nil {
		t.Fatal("未知操作应失败")
	}
}

func TestDecodeMelody(t *testing.T) {
	answer := "SUMMARY: 副歌升高了一个全音\nSTYLE: unchanged\n```abc\n" + testkit.ABC + "```\n"
	edit, err := decodeMelody(answer)
	if err != nil || edit.Summary != "副歌升高了一个全音" || edit.Style != "" || edit.Abc != testkit.ABC {
		t.Fatalf("%+v %v", edit, err)
	}
	edit, err = decodeMelody(strings.Replace(answer, "unchanged", "jazz, upright bass, 88 BPM", 1))
	if err != nil || edit.Style != "jazz, upright bass, 88 BPM" {
		t.Fatal(edit, err)
	}
	var issue *score.ABCIssue
	if _, err = decodeMelody(strings.Replace(answer, "c16|", "c8|", 1)); !errors.As(err, &issue) || issue.Bar != 1 {
		t.Fatal(err)
	}
	if _, err = decodeMelody("no score"); err == nil {
		t.Fatal("缺少谱应失败")
	}
}
