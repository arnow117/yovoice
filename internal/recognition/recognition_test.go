package recognition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func unitVector(first float64) []float64 {
	values := make([]float64, Dimension)
	values[0] = first
	if first < 1 {
		values[1] = math.Sqrt(1 - first*first)
	}
	return values
}

func testConfig() Config {
	return Config{ModelSHA256: strings.Repeat("a", 64), RunnerSHA256: strings.Repeat("b", 64), FeatureVersion: "v1", Threshold: .6, Margin: .05}
}

func TestRepositoryRoundTripDoesNotExposeEmbedding(t *testing.T) {
	repository := NewRepository(t.TempDir())
	profile, err := NewProfile(strings.Repeat("1", 32), "Alice", "", testConfig(), unitVector(1))
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.Save(context.Background(), []Profile{profile}); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.Load()
	if err != nil || len(loaded) != 1 || len(loaded[0].Embedding) != Dimension {
		t.Fatalf("%+v %v", loaded, err)
	}
	public := loaded[0].Public()
	if public.ID != profile.ID || public.Dimension != Dimension {
		t.Fatal(public)
	}
	encoded, err := os.ReadFile(repository.Path())
	if err != nil || !strings.Contains(string(encoded), "embedding") {
		t.Fatal("private data was not stored", err)
	}
}

func TestRepositoryRejectsCorruptionAndCancelledCommit(t *testing.T) {
	repository := NewRepository(t.TempDir())
	profile, err := NewProfile(strings.Repeat("2", 32), "Bob", "", testConfig(), unitVector(1))
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.Save(context.Background(), []Profile{profile}); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = repository.Save(cancelled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if loaded, loadErr := repository.Load(); loadErr != nil || len(loaded) != 1 {
		t.Fatalf("cancelled save changed data: %+v %v", loaded, loadErr)
	}
	if err = os.WriteFile(repository.Path(), []byte(`{"version":2,"profiles":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Load(); Code(err) != "persistence_failed" {
		t.Fatalf("%v", err)
	}
	if _, err = os.Stat(filepath.Join(repository.root, "state.backup.json")); !os.IsNotExist(err) {
		t.Fatal("speaker repository must not create state backups")
	}
}

func TestRepositoryCancellationAtAcceptPointDoesNotPublish(t *testing.T) {
	repository := NewRepository(t.TempDir())
	first, err := NewProfile(strings.Repeat("5", 32), "First", "", testConfig(), unitVector(1))
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.Save(context.Background(), []Profile{first}); err != nil {
		t.Fatal(err)
	}
	second, err := NewProfile(strings.Repeat("6", 32), "Second", "", testConfig(), unitVector(1))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	repository.beforeAccept = cancel
	if err = repository.Save(ctx, []Profile{first, second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("accept-point cancellation = %v", err)
	}
	loaded, err := repository.Load()
	if err != nil || len(loaded) != 1 || loaded[0].ID != first.ID {
		t.Fatalf("cancelled save published data: %+v %v", loaded, err)
	}
}

func TestIdentifyDecisionBoundariesAndCompatibility(t *testing.T) {
	config := testConfig()
	first, err := NewProfile(strings.Repeat("3", 32), "A", "", config, unitVector(1))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewProfile(strings.Repeat("4", 32), "B", "", config, unitVector(.95))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Identify(config, []Profile{first, second}, unitVector(1))
	if err != nil || result.Status != "matched" || result.ProfileID != first.ID || result.Score == nil {
		t.Fatalf("%+v %v", result, err)
	}
	second.Embedding = unitVector(.97)
	result, err = Identify(config, []Profile{first, second}, unitVector(1))
	if err != nil || result.Status != "ambiguous" || result.ProfileID != "" {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = Identify(config, []Profile{}, unitVector(1))
	if err != nil || result.Status != "unknown" || result.Reason != "empty_library" || result.Score != nil {
		t.Fatalf("%+v %v", result, err)
	}
	first.FeatureVersion = "old"
	if _, err = Identify(config, []Profile{first}, unitVector(1)); Code(err) != "feature_mismatch" {
		t.Fatalf("%v", err)
	}
}

func TestIdentifySingleCandidateThresholdBoundary(t *testing.T) {
	config := testConfig()
	cases := []struct {
		name          string
		score         float64
		wantStatus    string
		wantReason    string
		selectProfile bool
	}{
		{
			name:       "below threshold",
			score:      config.Threshold - .01,
			wantStatus: "unknown",
			wantReason: "below_threshold",
		},
		{
			name:          "at threshold",
			score:         config.Threshold,
			wantStatus:    "matched",
			wantReason:    "matched",
			selectProfile: true,
		},
	}

	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			profileID := strings.Repeat(string(rune('a'+index)), 32)
			profile, err := NewProfile(profileID, test.name, "", config, unitVector(test.score))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Identify(config, []Profile{profile}, unitVector(1))
			if err != nil {
				t.Fatal(err)
			}
			if result.Score == nil {
				t.Fatal("single-candidate result omitted score")
			}
			actualScore := *result.Score
			if actualScore != test.score {
				t.Fatalf("score = %v, want %v", actualScore, test.score)
			}
			if test.score == config.Threshold && actualScore != config.Threshold {
				t.Fatalf("threshold score = %v, want exactly %v", actualScore, config.Threshold)
			}
			if result.Status != test.wantStatus || result.Reason != test.wantReason {
				t.Fatalf("identification = %+v", result)
			}
			if len(result.Candidates) != 1 {
				t.Fatalf("candidate count = %d, want 1", len(result.Candidates))
			}
			candidate := result.Candidates[0]
			if candidate.Score != test.score {
				t.Fatalf("candidate score = %v, want %v", candidate.Score, test.score)
			}
			if test.selectProfile {
				if result.ProfileID != profileID || candidate.ProfileID != profileID {
					t.Fatalf("selected profile = %+v, want %q", result, profileID)
				}
				return
			}
			if result.ProfileID != "" {
				t.Fatalf("below-threshold result selected profile %q", result.ProfileID)
			}
		})
	}
}

func TestIdentifySortsByExactScoreBeforeDecisionTolerance(t *testing.T) {
	config := testConfig()
	config.Threshold = .6
	config.Margin = .05
	profiles := []Profile{}
	for _, item := range []struct {
		id    string
		score float64
	}{
		{strings.Repeat("a", 32), config.Threshold - 1.1e-12},
		{strings.Repeat("b", 32), config.Threshold - .35e-12},
		{strings.Repeat("c", 32), config.Threshold + .4e-12},
	} {
		profile, err := NewProfile(item.id, item.id, "", config, unitVector(item.score))
		if err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, profile)
	}
	result, err := Identify(config, profiles, unitVector(1))
	if err != nil || result.Status != "ambiguous" || result.Score == nil || result.Candidates[0].ProfileID != strings.Repeat("c", 32) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	profiles[1].Embedding = profiles[2].Embedding
	result, err = Identify(config, profiles[1:], unitVector(1))
	if err != nil || result.Status != "ambiguous" {
		t.Fatalf("equal-score tie must be ambiguous: %+v %v", result, err)
	}
	config.Margin = 1e-14
	result, err = Identify(config, profiles[1:], unitVector(1))
	if err != nil || result.Status != "ambiguous" || result.ProfileID != "" || len(result.Candidates) != 2 {
		t.Fatalf("equal-score tie with a tiny margin must be ambiguous: %+v %v", result, err)
	}
}

func TestLoadConfigRequiresAbsoluteInputsAndChecksLimits(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"python", "model", "runner"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{
		"YOVOICE_SPEAKER_PYTHON":    filepath.Join(directory, "python"),
		"YOVOICE_SPEAKER_MODEL":     filepath.Join(directory, "model"),
		"YOVOICE_SPEAKER_RUNNER":    filepath.Join(directory, "runner"),
		"YOVOICE_SPEAKER_THRESHOLD": "0.6",
		"YOVOICE_SPEAKER_MARGIN":    "0.05",
		"YOVOICE_SPEAKER_TIMEOUT":   "2s",
	}
	config, err := LoadConfig(func(key string) string { return env[key] })
	if err != nil || config.FeatureVersion == "" || config.Timeout.String() != "2s" {
		t.Fatalf("%+v %v", config, err)
	}
	env["YOVOICE_SPEAKER_MARGIN"] = "0"
	if _, err = LoadConfig(func(key string) string { return env[key] }); Code(err) != "runtime_not_configured" {
		t.Fatalf("%v", err)
	}
}

func TestLoadConfigReportsRedactedConfigurationFailures(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"python", "model", "runner"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	valid := map[string]string{
		"YOVOICE_SPEAKER_PYTHON":    filepath.Join(directory, "python"),
		"YOVOICE_SPEAKER_MODEL":     filepath.Join(directory, "model"),
		"YOVOICE_SPEAKER_RUNNER":    filepath.Join(directory, "runner"),
		"YOVOICE_SPEAKER_THRESHOLD": "0.6",
		"YOVOICE_SPEAKER_MARGIN":    "0.05",
		"YOVOICE_SPEAKER_TIMEOUT":   "2s",
	}

	defaults := make(map[string]string, len(valid))
	for key, value := range valid {
		defaults[key] = value
	}
	delete(defaults, "YOVOICE_SPEAKER_THRESHOLD")
	delete(defaults, "YOVOICE_SPEAKER_MARGIN")
	delete(defaults, "YOVOICE_SPEAKER_TIMEOUT")
	config, err := LoadConfig(func(key string) string { return defaults[key] })
	if err != nil || config.Threshold != .6 || config.Margin != .05 || config.Timeout != 120*time.Second {
		t.Fatalf("default configuration = %+v, %v", config, err)
	}

	cases := []struct {
		name   string
		update func(map[string]string)
		field  string
		reason string
	}{
		{
			name: "missing Python",
			update: func(env map[string]string) {
				env["YOVOICE_SPEAKER_PYTHON"] = ""
			},
			field:  "YOVOICE_SPEAKER_PYTHON",
			reason: "missing",
		},
		{
			name: "relative model",
			update: func(env map[string]string) {
				env["YOVOICE_SPEAKER_MODEL"] = "relative/model.onnx"
			},
			field:  "YOVOICE_SPEAKER_MODEL",
			reason: "not_absolute",
		},
		{
			name: "missing runner file",
			update: func(env map[string]string) {
				env["YOVOICE_SPEAKER_RUNNER"] = filepath.Join(directory, "missing-runner.py")
			},
			field:  "YOVOICE_SPEAKER_RUNNER",
			reason: "stat_not_exist",
		},
		{
			name: "invalid threshold",
			update: func(env map[string]string) {
				env["YOVOICE_SPEAKER_THRESHOLD"] = "not-a-number"
			},
			field:  "YOVOICE_SPEAKER_THRESHOLD",
			reason: "invalid_number",
		},
		{
			name: "out of range margin",
			update: func(env map[string]string) {
				env["YOVOICE_SPEAKER_MARGIN"] = "0"
			},
			field:  "YOVOICE_SPEAKER_MARGIN",
			reason: "out_of_range",
		},
		{
			name: "invalid timeout",
			update: func(env map[string]string) {
				env["YOVOICE_SPEAKER_TIMEOUT"] = "not-a-duration"
			},
			field:  "YOVOICE_SPEAKER_TIMEOUT",
			reason: "invalid_duration",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			env := make(map[string]string, len(valid))
			for key, value := range valid {
				env[key] = value
			}
			test.update(env)

			_, err := LoadConfig(func(key string) string { return env[key] })
			if Code(err) != "runtime_not_configured" {
				t.Fatalf("code = %q, error = %v", Code(err), err)
			}
			field, reason, ok := ConfigFailure(err)
			if !ok || field != test.field || reason != test.reason {
				t.Fatalf("configuration failure = (%q, %q, %t)", field, reason, ok)
			}
			if strings.Contains(err.Error(), directory) {
				t.Fatalf("public error leaked a private path: %v", err)
			}
		})
	}
}

func TestExtractValidatesRunnerProtocolAndFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the runner protocol fixture uses a POSIX shell; Windows is not exercised here")
	}
	directory := t.TempDir()
	model := filepath.Join(directory, "model.onnx")
	runner := filepath.Join(directory, "runner.py")
	python := filepath.Join(directory, "python")
	audio := filepath.Join(directory, "audio.wav")
	for path, content := range map[string]string{model: "model", runner: "runner", python: "python", audio: "audio"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	modelHash, err := fileSHA256(model)
	if err != nil {
		t.Fatal(err)
	}
	runnerHash, err := fileSHA256(runner)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		Python:       python,
		Model:        model,
		Runner:       runner,
		ModelSHA256:  modelHash,
		RunnerSHA256: runnerHash,
		Timeout:      time.Second,
	}

	response, err := json.Marshal(runnerResponse{
		ProtocolVersion: 1,
		ModelSHA256:     modelHash,
		RuntimeVersion:  RuntimeVersion,
		Dimension:       Dimension,
		Embedding:       unitVector(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(directory, "runner-success.sh")
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '"+string(response)+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	restore := execCommand
	execCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", script)
	}
	defer func() { execCommand = restore }()

	embedding, err := Extract(context.Background(), config, audio)
	if err != nil || len(embedding) != Dimension || math.Abs(embedding[0]-1) > 1e-12 {
		t.Fatalf("embedding=%v err=%v", embedding, err)
	}
	if err = os.WriteFile(model, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "runtime_failed" {
		t.Fatalf("model mutation = %v", err)
	}
}

func TestExtractRejectsBadResponseAndTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the runner protocol fixture uses a POSIX shell; Windows is not exercised here")
	}
	directory := t.TempDir()
	model := filepath.Join(directory, "model.onnx")
	runner := filepath.Join(directory, "runner.py")
	python := filepath.Join(directory, "python")
	audio := filepath.Join(directory, "audio.wav")
	for _, path := range []string{model, runner, python, audio} {
		if err := os.WriteFile(path, []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	modelHash, err := fileSHA256(model)
	if err != nil {
		t.Fatal(err)
	}
	runnerHash, err := fileSHA256(runner)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Python: python, Model: model, Runner: runner, ModelSHA256: modelHash, RunnerSHA256: runnerHash, Timeout: time.Second}

	script := filepath.Join(directory, "runner-bad.sh")
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '{\"protocolVersion\":2}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	restore := execCommand
	execCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", script)
	}
	defer func() { execCommand = restore }()
	if _, err = Extract(context.Background(), config, audio); Code(err) != "runtime_failed" {
		t.Fatalf("bad response = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '{\"protocolVersion\":1,\"unexpected\":true}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "runtime_failed" {
		t.Fatalf("unknown success field = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '{\"errorCode\":\"invalid_audio\"}'\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "audio" {
		t.Fatalf("allowed failure code = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '{\"errorCode\":\"invalid_feature\"}'\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "feature" {
		t.Fatalf("invalid feature code = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '{\"errorCode\":\"invalid_request\"}'\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "runtime_failed" {
		t.Fatalf("runtime failure code = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nprintf '%s\\n' '{\"errorCode\":\"/private/marker\"}'\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "runtime_failed" {
		t.Fatalf("unknown failure code = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nhead -c 70000 /dev/zero\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = Extract(context.Background(), config, audio); Code(err) != "runtime_failed" {
		t.Fatalf("oversized output = %v", err)
	}
	if err = os.WriteFile(script, []byte("cat >/dev/null\nsleep 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config.Timeout = 30 * time.Millisecond
	if _, err = Extract(context.Background(), config, audio); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
}

func TestHelpersRejectOversizedOutputAndExposeStableErrors(t *testing.T) {
	if _, err := readBounded(strings.NewReader("1234"), 3); Code(err) != "runtime_failed" {
		t.Fatalf("oversized output = %v", err)
	}
	if got, err := readBounded(strings.NewReader("ok"), 3); err != nil || string(got) != "ok" {
		t.Fatalf("bounded output = %q %v", got, err)
	}
	if Code(context.Canceled) != "cancelled" || Code(context.DeadlineExceeded) != "deadline" || Code(errors.New("raw")) != "persistence_failed" {
		t.Fatal("unexpected stable error code")
	}
	if got := Message(fail("feature")); got != "speaker recognition failed: feature" {
		t.Fatal(got)
	}
	profiles := []Profile{{ID: strings.Repeat("a", 32)}}
	if profile, found := Find(profiles, profiles[0].ID); !found || profile.ID != profiles[0].ID {
		t.Fatalf("profile=%+v found=%v", profile, found)
	}
	if _, found := Find(profiles, strings.Repeat("b", 32)); found {
		t.Fatal("unexpected profile")
	}
	if fmt.Sprint(fail("feature")) != "feature" {
		t.Fatal("error string changed")
	}
}

func TestConfigurationAndProfileValidationEdges(t *testing.T) {
	t.Setenv("PATH", "")
	if environment := cleanEnvironment(); len(environment) != 0 {
		t.Fatalf("empty PATH environment = %v", environment)
	}
	t.Setenv("PATH", "/bin")
	if environment := cleanEnvironment(); len(environment) != 1 || environment[0] != "PATH=/bin" {
		t.Fatalf("filtered environment = %v", environment)
	}

	if _, err := Normalize(make([]float64, Dimension)); Code(err) != "feature" {
		t.Fatalf("zero feature = %v", err)
	}
	if _, err := Normalize([]float64{1}); Code(err) != "feature" {
		t.Fatalf("short feature = %v", err)
	}
	invalid := unitVector(1)
	invalid[0] = math.NaN()
	if _, err := Normalize(invalid); Code(err) != "feature" {
		t.Fatalf("non-finite feature = %v", err)
	}

	profile, err := NewProfile(strings.Repeat("c", 32), "", "", testConfig(), unitVector(1))
	if err != nil || profile.Name != profile.ID {
		t.Fatalf("default profile = %+v %v", profile, err)
	}
	if _, err = NewProfile("", "Alice", "", testConfig(), unitVector(1)); Code(err) != "invalid_input" {
		t.Fatalf("missing ID = %v", err)
	}
	if _, err = NewProfile(strings.Repeat("d", 32), strings.Repeat("x", 101), "", testConfig(), unitVector(1)); Code(err) != "invalid_input" {
		t.Fatalf("long name = %v", err)
	}
}
