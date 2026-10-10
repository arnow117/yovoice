package workbench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

func TestSynthesizePreservesOrdinaryTerminalErrorDetail(t *testing.T) {
	workbench, err := New(t.TempDir())
	must(t, err)
	defer workbench.Close()
	voice := configureProfileGeneration(t, workbench)
	draft := schema.DefaultDraft()
	draft.ID = schema.NewID()
	draft.ModelID = "voxcpm2-q8"
	draft.Text = "返回损坏响应"
	draft.VoiceID = &voice.ID
	draft.ReferenceText = voice.ReferenceText
	draft.VoxMode = "continuation"

	_, err = workbench.Synthesize(context.Background(), draft)
	if err == nil || !strings.Contains(err.Error(), string(msg.ErrUnknown)) || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("ordinary synthesis error = %v", err)
	}
	activity := workbench.Store.Read().Activity
	if activity == nil || activity.ErrorCode == nil || *activity.ErrorCode != msg.ErrUnknown || activity.ErrorParams["detail"] != "unexpected EOF" {
		t.Fatalf("terminal activity = %+v", activity)
	}
}

func TestSynthesizePrioritizesFinalStateSaveFailure(t *testing.T) {
	workbench, err := New(t.TempDir())
	must(t, err)
	defer workbench.Close()
	voice := configureProfileGeneration(t, workbench)
	saved := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSaved := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseSaved)
	workbench.afterGenerationSaved = func() {
		close(saved)
		<-release
	}
	draft := schema.DefaultDraft()
	draft.ID = schema.NewID()
	draft.ModelID = "voxcpm2-q8"
	draft.Text = "正常结果"
	draft.VoiceID = &voice.ID
	draft.ReferenceText = voice.ReferenceText
	draft.VoxMode = "continuation"

	result := make(chan error, 1)
	go func() {
		_, synthErr := workbench.Synthesize(context.Background(), draft)
		result <- synthErr
	}()
	select {
	case <-saved:
	case <-time.After(2 * time.Second):
		t.Fatal("generation history was not saved")
	}
	backup := filepath.Join(workbench.Store.Root, "state.backup.json")
	must(t, os.Remove(backup))
	must(t, os.Mkdir(backup, 0700))
	releaseSaved()
	select {
	case err = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("synthesis did not finish after release")
	}
	if err == nil || !strings.Contains(err.Error(), string(msg.ErrStateSaveFailed)) || !strings.Contains(err.Error(), "state.backup.json") {
		t.Fatalf("final save failure = %v", err)
	}
	activity := workbench.Store.Read().Activity
	if activity == nil || activity.ErrorCode == nil || *activity.ErrorCode != msg.ErrStateSaveFailed || activity.ErrorParams["detail"] == "" {
		t.Fatalf("terminal activity = %+v", activity)
	}
}

// 历史快照不携带时间轴，作品自身的时间轴保持不变。
func TestGenerationSnapshotOmitsTimeline(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(wb.Store.Root, "models", "fake.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	reference := filepath.Join(t.TempDir(), "reference.wav")
	must(t, os.WriteFile(reference, wav(), 0600))
	voice, err := wb.ImportVoice(context.Background(), reference, "")
	must(t, err)
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "index-2.5-q8", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	d := schema.DefaultDraft()
	d.VoiceID = &voice.ID
	d.Timeline = &schema.AudioTimeline{Tracks: []schema.AudioLane{{ID: "lane", Name: "旁白", Clips: []schema.AudioClip{}}}}
	must(t, wb.Generate(d))
	done := wb.Done()
	<-done
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.History) != 1 {
		t.Fatalf("%+v", state.Activity)
	}
	if state.History[0].Settings.Timeline != nil {
		t.Fatal("历史快照不应携带时间轴")
	}
	if state.Drafts[0].ID != d.ID || state.Drafts[0].Timeline == nil {
		t.Fatal("作品时间轴应保留")
	}
}
