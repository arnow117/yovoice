package workbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/recognition"
	"yovoice/internal/schema"
	"yovoice/internal/testkit"
)

func TestApplySpeakerBindingUsesBoundVoiceWithoutChangingCallerEmotion(t *testing.T) {
	voice := schema.Voice{ID: strings.Repeat("a", 32), ReferenceText: "参考原文"}
	draft := schema.DefaultDraft()
	draft.ModelID = "voxcpm2-q8"
	draft.EmotionText = "保持调用方的情绪"
	if err := applySpeakerBinding(&draft, voice); err != nil {
		t.Fatal(err)
	}
	if draft.VoiceID == nil || *draft.VoiceID != voice.ID || draft.ReferenceText != voice.ReferenceText || draft.VoxMode != "continuation" || draft.EmotionText != "保持调用方的情绪" {
		t.Fatalf("%+v", draft)
	}
}

func TestApplySpeakerBindingReferenceTextRequirements(t *testing.T) {
	const voiceID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	tests := []struct {
		name          string
		modelID       string
		referenceText string
		wantErrorCode string
		wantVoiceMode string
	}{
		{
			name:          "Qwen Base permits an empty reference transcript",
			modelID:       "qwen3-tts-base-q8",
			wantVoiceMode: "",
		},
		{
			name:          "Omni clone rejects an empty reference transcript",
			modelID:       "omnivoice-q8",
			wantErrorCode: "invalid_input",
			wantVoiceMode: "clone",
		},
		{
			name:          "Omni clone accepts a supplied reference transcript",
			modelID:       "omnivoice-q8",
			referenceText: "绑定参考原文",
			wantVoiceMode: "clone",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := schema.DefaultDraft()
			draft.ModelID = test.modelID
			voice := schema.Voice{ID: voiceID, ReferenceText: test.referenceText}

			err := applySpeakerBinding(&draft, voice)
			if code := SpeakerErrorCode(err); code != test.wantErrorCode {
				t.Fatalf("SpeakerErrorCode() = %q, want %q (err = %v)", code, test.wantErrorCode, err)
			}
			if draft.VoiceMode != test.wantVoiceMode {
				t.Fatalf("VoiceMode = %q, want %q", draft.VoiceMode, test.wantVoiceMode)
			}
			if test.wantErrorCode != "" {
				return
			}
			if draft.VoiceID == nil || *draft.VoiceID != voiceID {
				t.Fatalf("VoiceID = %v, want %q", draft.VoiceID, voiceID)
			}
			if draft.ReferenceText != test.referenceText {
				t.Fatalf("ReferenceText = %q, want %q", draft.ReferenceText, test.referenceText)
			}
		})
	}
}

func TestSpeakerRuntimeLogsConfigurationRejectionOnceWithoutPrivateValues(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()

	const privateValue = "relative/private-speaker-runtime"
	t.Setenv("YOVOICE_SPEAKER_PYTHON", privateValue)
	t.Setenv("YOVOICE_SPEAKER_MODEL", "")
	t.Setenv("YOVOICE_SPEAKER_RUNNER", "")

	if runtime := workbench.SpeakerRuntime(); runtime.Configured {
		t.Fatalf("runtime = %+v", runtime)
	}
	if runtime := workbench.SpeakerRuntime(); runtime.Configured {
		t.Fatalf("runtime = %+v", runtime)
	}
	_, err = workbench.speakerConfigSnapshot()
	if recognition.Code(err) != "runtime_not_configured" {
		t.Fatalf("runtime error = %v", err)
	}
	if field, reason, ok := recognition.ConfigFailure(err); !ok || field != "YOVOICE_SPEAKER_PYTHON" || reason != "not_absolute" {
		t.Fatalf("configuration failure = (%q, %q, %t)", field, reason, ok)
	}

	content, err := os.ReadFile(filepath.Join(workbench.Store.Root, "logs", "service.log"))
	if err != nil {
		t.Fatal(err)
	}
	entry := string(content)
	if strings.Count(entry, "speaker.config_rejected") != 1 {
		t.Fatalf("speaker configuration rejection should log once: %s", entry)
	}
	if !strings.Contains(entry, `"field":"YOVOICE_SPEAKER_PYTHON"`) || !strings.Contains(entry, `"reason":"not_absolute"`) {
		t.Fatalf("configuration rejection log = %s", entry)
	}
	if strings.Contains(entry, privateValue) || strings.Contains(entry, workbench.Store.Root) {
		t.Fatalf("configuration rejection log leaked private values: %s", entry)
	}
}

func TestSpeakerRuntimeSerializesConfiguredValuesWithoutLosingZeroOrPrecision(t *testing.T) {
	t.Run("configured values retain zero threshold and subsecond timeout", func(t *testing.T) {
		workbench, err := New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer workbench.Close()

		configureSpeakerRuntime(t, workbench)
		t.Setenv("YOVOICE_SPEAKER_THRESHOLD", "0")
		t.Setenv("YOVOICE_SPEAKER_TIMEOUT", "500ms")

		encoded, err := json.Marshal(workbench.SpeakerRuntime())
		if err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if err := json.Unmarshal(encoded, &actual); err != nil {
			t.Fatal(err)
		}

		if threshold, ok := actual["threshold"].(float64); !ok || threshold != 0 {
			t.Fatalf("threshold = %#v, want numeric zero", actual["threshold"])
		}
		if timeout, ok := actual["timeoutSeconds"].(float64); !ok || timeout != 0.5 {
			t.Fatalf("timeoutSeconds = %#v, want 0.5", actual["timeoutSeconds"])
		}
	})

	t.Run("unconfigured runtime omits configuration metadata", func(t *testing.T) {
		workbench, err := New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer workbench.Close()

		t.Setenv("YOVOICE_SPEAKER_PYTHON", "")
		t.Setenv("YOVOICE_SPEAKER_MODEL", "")
		t.Setenv("YOVOICE_SPEAKER_RUNNER", "")

		encoded, err := json.Marshal(workbench.SpeakerRuntime())
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != `{"configured":false}` {
			t.Fatalf("unconfigured runtime = %s", encoded)
		}
	})
}

func TestSpeakerOperationKeepsItsOwnCancelAfterHandoff(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	first, err := workbench.startSpeakerOperation(context.Background(), time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	<-first.done
	started := make(chan struct{})
	released := make(chan struct{})
	second, err := workbench.startSpeakerOperation(context.Background(), time.Second, func(ctx context.Context) error {
		close(started)
		select {
		case <-released:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	first.cancel()
	select {
	case <-second.done:
		t.Fatal("completed operation cancellation affected its successor")
	case <-time.After(50 * time.Millisecond):
	}
	close(released)
	<-second.done
	if err = second.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSpeakerOperationTimeoutReleasesSharedSlot(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	op, err := workbench.startSpeakerOperation(context.Background(), 25*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-op.done
	if !errors.Is(op.Err(), context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", op.Err())
	}
	next, err := workbench.startSpeakerOperation(context.Background(), time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("timeout did not release operation slot: %v", err)
	}
	<-next.done
}

func TestSpeakerRequestsDoNotStartFromCancelledParent(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = workbench.EnrollSpeaker(ctx, SpeakerSource{FilePath: filepath.Join(t.TempDir(), "missing.wav")}, "A", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled enrollment = %v", err)
	}
	profiles, err := workbench.ListSpeakers()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("cancelled enrollment persisted profiles: %+v %v", profiles, err)
	}

	draft := schema.DefaultDraft()
	if _, err = workbench.SynthesizeSpeaker(ctx, strings.Repeat("1", 32), draft); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled profile synthesis = %v", err)
	}
}

func TestSpeakerSidecarMutationsRejectFrozenStore(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()

	profilesPath := filepath.Join(workbench.Store.Root, "speakers.json")
	profiles := []byte(`{"version":1,"profiles":[]}`)
	if err = os.WriteFile(profilesPath, profiles, 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "speaker.wav")
	if err = os.WriteFile(input, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}

	workbench.Store.Freeze()
	_, cleanup, err := workbench.copySpeakerInput(SpeakerSource{FilePath: input}, "")
	if cleanup != nil {
		cleanup()
	}
	assertStorageRestart(t, err)
	if _, err = workbench.startSpeakerOperation(context.Background(), time.Second, func(context.Context) error { return nil }); err == nil {
		t.Fatal("冻结后不应启动声纹操作")
	} else {
		assertStorageRestart(t, err)
	}
	if err = workbench.DeleteSpeaker(context.Background(), strings.Repeat("a", 32)); err == nil {
		t.Fatal("冻结后不应删除声纹档案")
	} else {
		assertStorageRestart(t, err)
	}
	actual, err := os.ReadFile(profilesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, profiles) {
		t.Fatalf("冻结后改写了声纹档案: %s", actual)
	}
	assertSpeakerTemporaryFilesRemoved(t, workbench)
}

func TestSpeakerReaderRequestsRejectFrozenOrClosedStoreBeforeReading(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*Workbench)
		context func() context.Context
		invoke  func(*Workbench, context.Context, io.Reader) error
	}{
		{
			name: "frozen enrollment",
			prepare: func(workbench *Workbench) {
				workbench.Store.Freeze()
			},
			context: context.Background,
			invoke: func(workbench *Workbench, ctx context.Context, reader io.Reader) error {
				_, err := workbench.EnrollSpeakerFromNamed(ctx, reader, "name", true, "")
				return err
			},
		},
		{
			name: "closed identification",
			prepare: func(workbench *Workbench) {
				workbench.Close()
			},
			context: context.Background,
			invoke: func(workbench *Workbench, ctx context.Context, reader io.Reader) error {
				_, err := workbench.IdentifySpeakerFrom(ctx, reader)
				return err
			},
		},
		{
			name:    "cancelled enrollment",
			prepare: func(*Workbench) {},
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			invoke: func(workbench *Workbench, ctx context.Context, reader io.Reader) error {
				_, err := workbench.EnrollSpeakerFromNamed(ctx, reader, "name", true, "")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workbench, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(workbench.Close)
			test.prepare(workbench)

			reader := &readCounter{}
			if err = test.invoke(workbench, test.context(), reader); err == nil {
				t.Fatal("不可写状态应拒绝声纹上传")
			}
			if reader.reads != 0 {
				t.Fatalf("不可写状态不应读取上传正文，实际读取 %d 次", reader.reads)
			}
			assertSpeakerTemporaryFilesRemoved(t, workbench)
		})
	}
}

func TestSpeakerReaderDoesNotWriteAfterMigrationFreezes(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	workbench.beforeSpeakerReceiveCommit = workbench.Store.Freeze

	_, err = workbench.EnrollSpeakerFromNamed(context.Background(), bytes.NewReader([]byte("speaker upload")), "name", true, "")
	assertStorageRestart(t, err)
	assertSpeakerTemporaryFilesRemoved(t, workbench)
}

type readCounter struct {
	reads int
}

func (r *readCounter) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

func assertStorageRestart(t *testing.T, err error) {
	t.Helper()
	var callError *msg.CallError
	if !errors.As(err, &callError) || callError.Code != msg.ErrStorageRestart {
		t.Fatalf("冻结写入错误 = %v", err)
	}
}

func TestEnrollSpeakerCancelsBeforeCommit(t *testing.T) {
	const commitWait = 3 * time.Second

	if runtime.GOOS == "windows" {
		t.Skip("the controlled recognition runtime fixture uses POSIX shell scripts")
	}
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	input := configureSpeakerRuntime(t, workbench)
	accepted := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseCommit := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(releaseCommit)
	workbench.beforeSpeakerCommit = func() {
		close(accepted)
		<-release
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() {
		_, enrollErr := workbench.EnrollSpeaker(ctx, SpeakerSource{FilePath: input}, "A", "")
		result <- enrollErr
	}()

	select {
	case <-accepted:
		cancel()
		releaseCommit()
	case enrollErr := <-result:
		releaseCommit()
		t.Fatalf("enrollment returned before commit acceptance: %v", enrollErr)
	case <-time.After(commitWait):
		cancel()
		releaseCommit()
		t.Fatalf("enrollment did not reach commit acceptance within %s", commitWait)
	}

	select {
	case enrollErr := <-result:
		if !errors.Is(enrollErr, context.Canceled) {
			t.Fatalf("pre-accept cancellation = %v", enrollErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled enrollment did not return within 1s")
	}
	profiles, err := workbench.ListSpeakers()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("pre-accept cancellation saved profiles: %+v %v", profiles, err)
	}
}

func TestSpeakerEnrollIdentifyDeleteAndRestartJourney(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the controlled recognition runtime fixture uses POSIX shell scripts")
	}
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := configureSpeakerRuntime(t, workbench)
	ctx := context.Background()
	voice, err := workbench.ImportVoice(ctx, input, "preserved voice")
	if err != nil {
		t.Fatal(err)
	}
	if err = workbench.Store.Update(func(state *schema.State) {
		state.History = append(state.History, schema.Generation{ID: strings.Repeat("f", 32), FileName: "kept.wav"})
	}, true); err != nil {
		t.Fatal(err)
	}

	first, err := workbench.EnrollSpeaker(ctx, SpeakerSource{FilePath: input}, "Repeated", voice.ID)
	if err != nil {
		t.Fatal(err)
	}
	stateBeforeQuery := workbench.Store.Read()
	profilesBeforeQuery, err := workbench.ListSpeakers()
	if err != nil {
		t.Fatal(err)
	}
	matched, err := workbench.IdentifySpeaker(ctx, SpeakerSource{FilePath: input})
	if err != nil || matched.Status != "matched" || matched.ProfileID != first.ID {
		t.Fatalf("first identify = %+v %v", matched, err)
	}
	if stateAfterQuery := workbench.Store.Read(); !reflect.DeepEqual(stateBeforeQuery, stateAfterQuery) {
		t.Fatalf("identify changed state: before=%+v after=%+v", stateBeforeQuery, stateAfterQuery)
	}
	profilesAfterQuery, err := workbench.ListSpeakers()
	if err != nil || !reflect.DeepEqual(profilesBeforeQuery, profilesAfterQuery) {
		t.Fatalf("identify changed profiles: before=%+v after=%+v err=%v", profilesBeforeQuery, profilesAfterQuery, err)
	}

	runtimeRoot := filepath.Dir(input)
	block := filepath.Join(runtimeRoot, "block-runner")
	started := filepath.Join(runtimeRoot, "runner-started")
	release := filepath.Join(runtimeRoot, "runner-release")
	if err = os.WriteFile(block, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	concurrent := make(chan struct {
		result recognition.Identification
		err    error
	}, 1)
	go func() {
		result, identifyErr := workbench.IdentifySpeaker(ctx, SpeakerSource{FilePath: input})
		concurrent <- struct {
			result recognition.Identification
			err    error
		}{result: result, err: identifyErr}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(started); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent identify did not reach the runner")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = workbench.DeleteSpeaker(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	oldSnapshot := <-concurrent
	if oldSnapshot.err != nil || oldSnapshot.result.Status != "matched" || oldSnapshot.result.ProfileID != first.ID {
		t.Fatalf("concurrent old snapshot = %+v %v", oldSnapshot.result, oldSnapshot.err)
	}
	if err = os.Remove(block); err != nil {
		t.Fatal(err)
	}
	updated, err := workbench.IdentifySpeaker(ctx, SpeakerSource{FilePath: input})
	if err != nil || updated.Status != "unknown" || updated.ProfileID != "" {
		t.Fatalf("post-delete query = %+v %v", updated, err)
	}

	second, err := workbench.EnrollSpeaker(ctx, SpeakerSource{FilePath: input}, "Repeated", voice.ID)
	if err != nil || second.ID == first.ID {
		t.Fatalf("explicit repeat enrollment = %+v %v", second, err)
	}
	third, err := workbench.EnrollSpeaker(ctx, SpeakerSource{FilePath: input}, "Repeated", voice.ID)
	if err != nil || third.ID == second.ID {
		t.Fatalf("second explicit repeat enrollment = %+v %v", third, err)
	}
	if err = workbench.DeleteSpeaker(ctx, first.ID); err != nil {
		t.Fatalf("idempotent delete = %v", err)
	}
	if err = workbench.DeleteSpeaker(ctx, third.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = workbench.SynthesizeSpeaker(ctx, first.ID, schema.DefaultDraft()); SpeakerErrorCode(err) != "profile" {
		t.Fatalf("deleted profile synthesis = %v", err)
	}
	serviceRoot := workbench.Store.Root
	workbench.Close()

	workbench, err = New(serviceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	profiles, err := workbench.ListSpeakers()
	if err != nil || len(profiles) != 1 || profiles[0].ID != second.ID {
		t.Fatalf("restart profiles = %+v %v", profiles, err)
	}
	state := workbench.Store.Read()
	if len(state.Voices) != 1 || state.Voices[0].ID != voice.ID || len(state.History) != 1 {
		t.Fatalf("delete changed preserved state: %+v", state)
	}
	matched, err = workbench.IdentifySpeaker(ctx, SpeakerSource{FilePath: input})
	if err != nil || matched.Status != "matched" || matched.ProfileID != second.ID {
		t.Fatalf("post-delete identify = %+v %v", matched, err)
	}
}

func TestSpeakerReaderUploadsAreRemovedAfterCancellationOrClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the controlled recognition runtime fixture uses POSIX shell scripts")
	}

	tests := []struct {
		name   string
		invoke func(*Workbench, context.Context, *bytes.Reader) error
		stop   func(*Workbench)
	}{
		{
			name: "enrollment cancellation",
			invoke: func(workbench *Workbench, ctx context.Context, input *bytes.Reader) error {
				_, err := workbench.EnrollSpeakerFrom(ctx, input, "Cancelled enrollment", "")
				return err
			},
			stop: (*Workbench).Cancel,
		},
		{
			name: "identification close",
			invoke: func(workbench *Workbench, ctx context.Context, input *bytes.Reader) error {
				_, err := workbench.IdentifySpeakerFrom(ctx, input)
				return err
			},
			stop: (*Workbench).Close,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workbench, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(workbench.Close)

			inputPath := configureSpeakerRuntime(t, workbench)
			input, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			stateBefore := workbench.Store.Read()

			runtimeRoot := filepath.Dir(inputPath)
			block := filepath.Join(runtimeRoot, "block-runner")
			started := filepath.Join(runtimeRoot, "runner-started")
			if err = os.WriteFile(block, []byte("block"), 0600); err != nil {
				t.Fatal(err)
			}

			result := make(chan error, 1)
			go func() {
				result <- test.invoke(workbench, context.Background(), bytes.NewReader(input))
			}()
			waitForSpeakerRunner(t, started)

			test.stop(workbench)
			select {
			case operationErr := <-result:
				if !errors.Is(operationErr, context.Canceled) {
					t.Fatalf("reader operation error = %v", operationErr)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("reader operation did not finish after cancellation")
			}

			profiles, listErr := workbench.ListSpeakers()
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(profiles) != 0 {
				t.Fatalf("cancelled reader operation persisted profiles: %+v", profiles)
			}
			if stateAfter := workbench.Store.Read(); !reflect.DeepEqual(stateBefore, stateAfter) {
				t.Fatalf("reader operation changed state: before=%+v after=%+v", stateBefore, stateAfter)
			}
			assertSpeakerTemporaryFilesRemoved(t, workbench)
		})
	}
}

func waitForSpeakerRunner(t *testing.T, started string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("speaker operation did not reach the controlled runner")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertSpeakerTemporaryFilesRemoved(t *testing.T, workbench *Workbench) {
	t.Helper()
	for _, pattern := range []string{"upload-*", "recognition-*"} {
		leftovers, err := filepath.Glob(filepath.Join(workbench.Store.Root, "downloads", pattern))
		if err != nil {
			t.Fatal(err)
		}
		if len(leftovers) != 0 {
			t.Fatalf("reader operation left temporary files for %s: %v", pattern, leftovers)
		}
	}
}

func configureSpeakerRuntime(t *testing.T, workbench *Workbench) string {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "speaker.wav")
	pcm := wav()
	for index := 44; index+1 < len(pcm); index += 2 {
		pcm[index+1] = 0x20
	}
	if err := os.WriteFile(input, pcm, 0600); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(root, "model.onnx")
	runner := filepath.Join(root, "runner.py")
	python := filepath.Join(root, "python.sh")
	converter := filepath.Join(root, "converter.sh")
	if err := os.WriteFile(model, []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, []byte("runner"), 0600); err != nil {
		t.Fatal(err)
	}
	embedding := unitSpeakerVector()
	modelHash := fmt.Sprintf("%x", sha256.Sum256([]byte("model")))
	response, err := json.Marshal(map[string]any{
		"protocolVersion": 1,
		"modelSha256":     modelHash,
		"runtimeVersion":  recognition.RuntimeVersion,
		"dimension":       recognition.Dimension,
		"embedding":       embedding,
	})
	if err != nil {
		t.Fatal(err)
	}
	pythonScript := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"if [ -e '" + filepath.Join(root, "block-runner") + "' ]; then\n" +
		"\ttouch '" + filepath.Join(root, "runner-started") + "'\n" +
		"\twhile [ ! -e '" + filepath.Join(root, "runner-release") + "' ]; do sleep 0.01; done\n" +
		"fi\n" +
		"printf '%s\\n' '" + string(response) + "'\n"
	if err = os.WriteFile(python, []byte(pythonScript), 0700); err != nil {
		t.Fatal(err)
	}
	converterScript := "#!/bin/sh\n" +
		"input=\n" +
		"output=\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		"\tif [ \"$1\" = \"-i\" ]; then\n" +
		"\t\tinput=$2\n" +
		"\t\tshift 2\n" +
		"\t\tcontinue\n" +
		"\tfi\n" +
		"\toutput=$1\n" +
		"\tshift\n" +
		"done\n" +
		"cp \"$input\" \"$output\"\n"
	if err = os.WriteFile(converter, []byte(converterScript), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOVOICE_SPEAKER_PYTHON", python)
	t.Setenv("YOVOICE_SPEAKER_MODEL", model)
	t.Setenv("YOVOICE_SPEAKER_RUNNER", runner)
	t.Setenv("YOVOICE_SPEAKER_THRESHOLD", ".6")
	t.Setenv("YOVOICE_SPEAKER_MARGIN", ".05")
	t.Setenv("YOVOICE_SPEAKER_TIMEOUT", "2s")
	original := speakerConverter
	speakerConverter = func() (string, error) { return converter, nil }
	t.Cleanup(func() { speakerConverter = original })
	return input
}

func TestInvalidRecognitionAudioDoesNotMutateWorkbench(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the controlled recognition runtime fixture uses POSIX shell scripts")
	}
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	validInput := configureSpeakerRuntime(t, workbench)

	cases := []struct {
		name string
		data []byte
	}{
		{name: "silent", data: speakerRecognitionWAV(16000, 0)},
		{name: "corrupt", data: []byte("not a WAV")},
		{name: "short", data: speakerRecognitionWAV(15999, 1024)},
		{name: "too long", data: speakerRecognitionWAV(16000*60+1, 1024)},
	}
	inputDirectory := t.TempDir()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if len(test.data) > 2<<20 {
				t.Fatalf("test WAV unexpectedly exceeds local fixture limit: %d", len(test.data))
			}
			input := filepath.Join(inputDirectory, test.name+".wav")
			if err := os.WriteFile(input, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			stateBefore := workbench.Store.Read()

			if _, err := workbench.EnrollSpeaker(context.Background(), SpeakerSource{FilePath: input}, "Invalid "+test.name, ""); SpeakerErrorCode(err) != "audio" {
				t.Fatalf("enroll error = %v, code = %q", err, SpeakerErrorCode(err))
			}
			assertInvalidRecognitionInputHasNoSideEffects(t, workbench, stateBefore)

			if _, err := workbench.IdentifySpeaker(context.Background(), SpeakerSource{FilePath: input}); SpeakerErrorCode(err) != "audio" {
				t.Fatalf("identify error = %v, code = %q", err, SpeakerErrorCode(err))
			}
			assertInvalidRecognitionInputHasNoSideEffects(t, workbench, stateBefore)
		})
	}

	result, err := workbench.IdentifySpeaker(context.Background(), SpeakerSource{FilePath: validInput})
	if err != nil || result.Status != "unknown" || result.ProfileID != "" {
		t.Fatalf("operation slot was not reusable after invalid audio: %+v %v", result, err)
	}
}

func assertInvalidRecognitionInputHasNoSideEffects(t *testing.T, workbench *Workbench, stateBefore schema.State) {
	t.Helper()
	profiles, err := workbench.ListSpeakers()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Fatalf("invalid input persisted profiles: %+v", profiles)
	}
	stateAfter := workbench.Store.Read()
	if !reflect.DeepEqual(stateBefore, stateAfter) {
		t.Fatalf("invalid input changed workbench state: before=%+v after=%+v", stateBefore, stateAfter)
	}
	leftovers, err := filepath.Glob(filepath.Join(workbench.Store.Root, "downloads", "recognition-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("invalid input left recognition temporary files: %v", leftovers)
	}
}

func speakerRecognitionWAV(samples int, value int16) []byte {
	dataLength := samples * 2
	result := make([]byte, 44+dataLength)
	copy(result, "RIFF")
	binary.LittleEndian.PutUint32(result[4:], uint32(len(result)-8))
	copy(result[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(result[16:], 16)
	binary.LittleEndian.PutUint16(result[20:], 1)
	binary.LittleEndian.PutUint16(result[22:], 1)
	binary.LittleEndian.PutUint32(result[24:], 16000)
	binary.LittleEndian.PutUint32(result[28:], 32000)
	binary.LittleEndian.PutUint16(result[32:], 2)
	binary.LittleEndian.PutUint16(result[34:], 16)
	copy(result[36:], "data")
	binary.LittleEndian.PutUint32(result[40:], uint32(dataLength))
	for sample := 0; sample < samples; sample++ {
		binary.LittleEndian.PutUint16(result[44+sample*2:], uint16(value))
	}
	return result
}

func TestSpeakerTimeoutCoversBlockingConverter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a POSIX shell script; Windows conversion is not claimed by this test")
	}
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	script := filepath.Join(t.TempDir(), "blocking-converter.sh")
	if err = os.WriteFile(script, []byte("#!/bin/sh\nsleep 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	original := speakerConverter
	speakerConverter = func() (string, error) { return script, nil }
	defer func() { speakerConverter = original }()
	input := filepath.Join(t.TempDir(), "input.wav")
	if err = os.WriteFile(input, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	config := recognition.Config{Timeout: 25 * time.Millisecond}
	op, err := workbench.startSpeakerOperation(context.Background(), config.Timeout, func(ctx context.Context) error {
		_, extractErr := workbench.extractSpeakerInline(ctx, config, input)
		return extractErr
	})
	if err != nil {
		t.Fatal(err)
	}
	<-op.done
	if !errors.Is(op.Err(), context.DeadlineExceeded) {
		t.Fatalf("converter did not observe timeout: %v", op.Err())
	}
	workbench.mu.Lock()
	active := workbench.active
	workbench.mu.Unlock()
	if active != nil {
		t.Fatal("blocking converter left operation active")
	}
}

func TestSynthesizeSpeakerUsesBoundVoiceAndReturnsOwnedGeneration(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	voice := configureProfileGeneration(t, workbench)
	config := recognition.Config{ModelSHA256: strings.Repeat("a", 64), FeatureVersion: "test"}
	profile, err := recognition.NewProfile(strings.Repeat("1", 32), "A", voice.ID, config, unitSpeakerVector())
	if err != nil {
		t.Fatal(err)
	}
	if err = workbench.speakerRepository().Save(context.Background(), []recognition.Profile{profile}); err != nil {
		t.Fatal(err)
	}
	voiceReference, err := workbench.MediaFile("voices", voice.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOVOICE_TEST_EXPECTED_VOICE_REF", voiceReference)
	t.Setenv("YOVOICE_TEST_EXPECTED_TEXT", "检查Vox绑定")
	draft := schema.DefaultDraft()
	draft.ID = schema.NewID()
	draft.ModelID = "voxcpm2-q8"
	draft.Text = "检查Vox绑定"
	draft.Mode = "speaker"
	draft.EmotionText = "调用方情绪"
	generation, err := workbench.SynthesizeSpeaker(context.Background(), profile.ID, draft)
	if err != nil {
		t.Fatal(err)
	}
	if generation.ID == "" || generation.Settings.VoiceID == nil || *generation.Settings.VoiceID != voice.ID || generation.Settings.ReferenceText != voice.ReferenceText || generation.Settings.EmotionText != draft.EmotionText {
		t.Fatalf("generation did not preserve profile binding and caller settings: %+v", generation.Settings)
	}
	if err = workbench.Store.Update(func(state *schema.State) {
		state.Models = append(state.Models, schema.InstalledModel{ID: "index-2.5-q8", Path: state.Models[0].Path})
	}, true); err != nil {
		t.Fatal(err)
	}
	indexDraft := schema.DefaultDraft()
	indexDraft.ID = schema.NewID()
	indexDraft.ModelID = "index-2.5-q8"
	indexDraft.Text = "检查Index绑定"
	indexDraft.Mode = "text"
	indexDraft.EmotionText = "调用方情绪"
	t.Setenv("YOVOICE_TEST_EXPECTED_TEXT", "检查Index绑定")
	secondGeneration, err := workbench.SynthesizeSpeaker(context.Background(), profile.ID, indexDraft)
	if err != nil {
		t.Fatalf("Index profile binding request = %v", err)
	}
	if secondGeneration.ID == "" || secondGeneration.ID == generation.ID {
		t.Fatalf("independent profile operations returned %+v and %+v", generation, secondGeneration)
	}
	if err = workbench.Store.Update(func(state *schema.State) { state.History = nil }, true); err != nil {
		t.Fatal(err)
	}
	if generation.ID == "" {
		t.Fatal("result depended on mutable shared history")
	}

	conflicting := draft
	conflicting.VoiceID = &voice.ID
	if _, err = workbench.SynthesizeSpeaker(context.Background(), profile.ID, conflicting); SpeakerErrorCode(err) != "invalid_input" {
		t.Fatalf("explicit primary voice conflict = %v", err)
	}
}

func TestSynthesizeSpeakerReturnsOwnedResultAfterSuccessorStarts(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	voice := configureProfileGeneration(t, workbench)
	config := recognition.Config{ModelSHA256: strings.Repeat("b", 64), FeatureVersion: "test"}
	profile, err := recognition.NewProfile(strings.Repeat("8", 32), "A", voice.ID, config, unitSpeakerVector())
	if err != nil {
		t.Fatal(err)
	}
	if err = workbench.speakerRepository().Save(context.Background(), []recognition.Profile{profile}); err != nil {
		t.Fatal(err)
	}
	aReady := make(chan struct{})
	releaseA := make(chan struct{})
	var readyOnce sync.Once
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseA) }) }
	t.Cleanup(release)
	workbench.beforeSpeakerResultReturn = func() {
		readyOnce.Do(func() {
			close(aReady)
			<-releaseA
		})
	}

	aDraft := schema.DefaultDraft()
	aDraft.ID = schema.NewID()
	aDraft.ModelID = "voxcpm2-q8"
	aDraft.Text = "A 已完成"
	aContext, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	aResult := make(chan struct {
		generation schema.Generation
		err        error
	}, 1)
	go func() {
		generation, synthErr := workbench.SynthesizeSpeaker(aContext, profile.ID, aDraft)
		aResult <- struct {
			generation schema.Generation
			err        error
		}{generation: generation, err: synthErr}
	}()
	select {
	case <-aReady:
	case <-time.After(2 * time.Second):
		select {
		case completed := <-aResult:
			t.Fatalf("completed A result did not reach the caller hook: %+v %v", completed.generation, completed.err)
		default:
			t.Fatal("completed A result did not wait for its caller")
		}
	}

	bDraft := aDraft
	bDraft.ID = schema.NewID()
	bDraft.Text = "等待取消"
	bContext, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	bResult := make(chan error, 1)
	go func() {
		_, synthErr := workbench.SynthesizeSpeaker(bContext, profile.ID, bDraft)
		bResult <- synthErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		workbench.mu.Lock()
		active := workbench.active
		workbench.mu.Unlock()
		if active != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("successor operation did not acquire the slot")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = workbench.Store.Update(func(state *schema.State) { state.History = nil }, true); err != nil {
		t.Fatal(err)
	}
	cancelA()
	select {
	case synthErr := <-bResult:
		t.Fatalf("A cancellation affected successor B: %v", synthErr)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case completed := <-aResult:
		if completed.err != nil || completed.generation.ID == "" {
			t.Fatalf("A owned result after B handoff = %+v %v", completed.generation, completed.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A caller did not receive its completed result")
	}
	cancelB()
	select {
	case synthErr := <-bResult:
		if !errors.Is(synthErr, context.Canceled) {
			t.Fatalf("successor B cancellation = %v", synthErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("successor B did not stop")
	}
}

func TestSynthesizeSpeakerRedactsNativeDiagnostics(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	voice := configureProfileGeneration(t, workbench)
	config := recognition.Config{ModelSHA256: strings.Repeat("a", 64), FeatureVersion: "test"}
	profile, err := recognition.NewProfile(strings.Repeat("2", 32), "A", voice.ID, config, unitSpeakerVector())
	if err != nil {
		t.Fatal(err)
	}
	if err = workbench.speakerRepository().Save(context.Background(), []recognition.Profile{profile}); err != nil {
		t.Fatal(err)
	}
	draft := schema.DefaultDraft()
	draft.ID = schema.NewID()
	draft.ModelID = "voxcpm2-q8"
	draft.Text = "private request 私有标记失败"
	draft.Mode = "speaker"
	if _, err = workbench.SynthesizeSpeaker(context.Background(), profile.ID, draft); SpeakerErrorCode(err) != "runtime_failed" {
		t.Fatalf("profile generation error = %v", err)
	}
	const marker = "/private/speaker-profile/marker"
	activity := workbench.Store.Read().Activity
	if activity == nil || activity.ErrorCode == nil || *activity.ErrorCode != msg.ErrGenerateFailed || len(activity.ErrorParams) != 0 {
		t.Fatalf("profile activity leaked native marker: %+v", activity)
	}
	for _, name := range []string{"service.log", "last-error.txt", "engine.log.out", "engine.log", "engine-error.log", "last-inference-error.txt"} {
		content, readErr := os.ReadFile(filepath.Join(workbench.Store.Root, "logs", name))
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), marker) {
			t.Fatalf("profile request leaked native marker to %s", name)
		}
	}
}

func configureProfileGeneration(t *testing.T, workbench *Workbench) schema.Voice {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(workbench.Store.Root, "models", "fake.gguf")
	if err = os.WriteFile(model, []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(t.TempDir(), "reference.wav")
	if err = os.WriteFile(reference, wav(), 0600); err != nil {
		t.Fatal(err)
	}
	voice, err := workbench.importVoice(context.Background(), reference, "voice", "绑定原文", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = workbench.Store.Update(func(state *schema.State) {
		state.Models = []schema.InstalledModel{{ID: "voxcpm2-q8", Path: model}}
		state.RuntimePath = &executable
		state.RuntimeBackend = ptr("cpu")
		state.RuntimeVersion = ptr(catalog.EngineVersion)
		state.Preferences.Backend = "cpu"
	}, true); err != nil {
		t.Fatal(err)
	}
	return voice
}

func unitSpeakerVector() []float64 {
	values := make([]float64, recognition.Dimension)
	values[0] = 1
	return values
}

func TestWaitSpeakerOperationKeepsAcceptedSuccessAfterCallerCancellation(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	op, err := workbench.startSpeakerOperation(context.Background(), time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	<-op.done
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = waitSpeakerOperation(ctx, op); err != nil {
		t.Fatalf("accepted operation was changed to cancellation: %v", err)
	}

	blocked, err := workbench.startSpeakerOperation(context.Background(), time.Second, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err = waitSpeakerOperation(ctx, blocked); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-accept cancellation = %v", err)
	}
}

func TestApplySpeakerBindingRejectsUnsupportedModels(t *testing.T) {
	voice := schema.Voice{ID: strings.Repeat("a", 32)}
	draft := schema.DefaultDraft()
	draft.ModelID = "kokoro-82m-q8"
	if err := applySpeakerBinding(&draft, voice); SpeakerErrorCode(err) != "unsupported_clone" {
		t.Fatalf("%v", err)
	}
}

func TestProfileOperationFinalPersistenceFailureIsRedacted(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseOperation := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseOperation()
	op, err := workbench.startProfileOperation("generate", msg.ActivityGenerate, nil, nil, func(context.Context) error {
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	const marker = "test-profile-final-state-save-failure"
	testkit.FailSettingsUpdate(t, workbench.Store.Root, "activity", "completed", marker)
	releaseOperation()
	<-op.done

	var callError *msg.CallError
	if !errors.As(op.Err(), &callError) || callError.Code != msg.ErrStateSaveFailed {
		t.Fatalf("terminal error = %v", op.Err())
	}
	if code := SpeakerErrorCode(op.Err()); code != "persistence_failed" {
		t.Fatalf("terminal error code = %q", code)
	}
	activity := workbench.Store.Read().Activity
	if activity == nil || activity.Status != "failed" || activity.ErrorCode == nil || *activity.ErrorCode != msg.ErrStateSaveFailed || len(activity.ErrorParams) != 0 {
		t.Fatalf("activity = %+v", activity)
	}
	for _, name := range []string{"service.log", "last-error.txt"} {
		content, readErr := os.ReadFile(filepath.Join(workbench.Store.Root, "logs", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), workbench.Store.Root) || strings.Contains(string(content), "detail") || strings.Contains(string(content), marker) {
			t.Fatalf("profile persistence failure leaked into %s: %s", name, content)
		}
	}
}

func TestProfileOperationRedactsNativeFailureMarker(t *testing.T) {
	workbench, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workbench.Close()
	const marker = "/private/speaker-profile/marker"
	op, err := workbench.startProfileOperation("generate", msg.ActivityGenerate, nil, nil, func(context.Context) error {
		return errors.New(marker)
	})
	if err != nil {
		t.Fatal(err)
	}
	<-op.done
	var callError *msg.CallError
	if !errors.As(op.Err(), &callError) || callError.Code != msg.ErrGenerateFailed {
		t.Fatalf("terminal error = %v", op.Err())
	}
	activity := workbench.Store.Read().Activity
	if activity == nil || activity.ErrorCode == nil || *activity.ErrorCode != msg.ErrGenerateFailed {
		t.Fatalf("activity = %+v", activity)
	}
	for _, name := range []string{"service.log", "last-error.txt"} {
		content, readErr := os.ReadFile(filepath.Join(workbench.Store.Root, "logs", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), marker) {
			t.Fatalf("native marker leaked to %s", name)
		}
	}
}

func TestCompatibleSpeakerProfilesRejectsOldFeatureVersion(t *testing.T) {
	config := recognition.Config{ModelSHA256: strings.Repeat("a", 64), FeatureVersion: "new"}
	existing := recognition.Profile{ID: strings.Repeat("1", 32), ModelSHA256: config.ModelSHA256, FeatureVersion: "old", Dimension: recognition.Dimension}
	next := recognition.Profile{ID: strings.Repeat("2", 32), ModelSHA256: config.ModelSHA256, FeatureVersion: config.FeatureVersion, Dimension: recognition.Dimension}
	if err := compatibleSpeakerProfiles([]recognition.Profile{existing}, next, config); SpeakerErrorCode(err) != "feature_mismatch" {
		t.Fatalf("%v", err)
	}
}

func TestCopySpeakerFileChecksBytesCopiedAndPrivateMode(t *testing.T) {
	directory := t.TempDir()
	oversized := filepath.Join(directory, "growing.wav")
	if err := os.WriteFile(oversized, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(oversized, maxUpload+1); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "copied.wav")
	var callError *msg.CallError
	if err := copySpeakerFile(oversized, destination); !errors.As(err, &callError) || callError.Code != msg.ErrAudioTooLarge {
		t.Fatalf("oversized copy = %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("oversized partial output remained: %v", err)
	}

	small := filepath.Join(directory, "small.wav")
	if err := os.WriteFile(small, []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copySpeakerFile(small, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private output mode = %v %v", info, err)
	}
}
