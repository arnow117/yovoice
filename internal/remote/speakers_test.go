package remote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/recognition"
	"yovoice/internal/schema"
	"yovoice/internal/testkit"
	"yovoice/internal/workbench"
)

func TestSpeakerErrorsAndStrictJSON(t *testing.T) {
	for _, test := range []struct {
		code string
		want int
	}{
		{"runtime_not_configured", http.StatusServiceUnavailable},
		{"feature_mismatch", http.StatusConflict},
		{"input_too_large", http.StatusRequestEntityTooLarge},
	} {
		recorder := httptest.NewRecorder()
		speakerError(recorder, test.want, test.code)
		if recorder.Code != test.want || !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("%s: %d %s", test.code, recorder.Code, recorder.Body.String())
		}
	}
	var value struct {
		VoiceID string `json:"voiceId"`
	}
	if err := decodeStrictJSON(strings.NewReader(`{"voiceId":"a"}{}`), &value); err == nil {
		t.Fatal("must reject JSON suffix")
	}
	if err := decodeStrictJSON(strings.NewReader(`{"path":"/tmp/x"}`), &value); err == nil {
		t.Fatal("must reject unknown fields")
	}
}

func TestSpeakerRoutesApplyAuthenticationOriginAndInputBoundaries(t *testing.T) {
	service, err := workbench.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	api := &API{Workbench: service, Token: strings.Repeat("t", 32), Context: context.Background(), jobs: map[string]*generationJob{}}
	request := httptest.NewRequest(http.MethodPost, "/v1/speakers", strings.NewReader(`{"voiceId":"voice"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("authentication = %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/speakers", strings.NewReader(`{"voiceId":"voice"}`))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	request.Header.Set("Origin", "https://example.test")
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("origin = %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/speakers?path=/tmp/x", strings.NewReader("audio"))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("strict raw query = %d %#v", recorder.Code, recorder.Header())
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/speakers", strings.NewReader(`{"voiceId":"voice","name":" "}`))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"invalid_input"`) {
		t.Fatalf("explicit empty name = %d %s", recorder.Code, recorder.Body.String())
	}

	overflow := `{"voiceId":"` + strings.Repeat("a", speakerJSONLimit) + `"}`
	request = httptest.NewRequest(http.MethodPost, "/v1/speakers", strings.NewReader(overflow))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), `"input_too_large"`) {
		t.Fatalf("JSON limit = %d %s", recorder.Code, recorder.Body.String())
	}

	tailOverflow := `{"voiceId":"voice"}` + strings.Repeat(" ", speakerJSONLimit)
	request = httptest.NewRequest(http.MethodPost, "/v1/speakers", strings.NewReader(tailOverflow))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), `"input_too_large"`) {
		t.Fatalf("enroll JSON tail limit = %d %s", recorder.Code, recorder.Body.String())
	}

	for _, rawQuery := range []string{"path=%ZZ", "name=a;unknown=b"} {
		request = httptest.NewRequest(http.MethodPost, "/v1/speakers", strings.NewReader("audio"))
		request.URL.RawQuery = rawQuery
		request.Header.Set("Authorization", "Bearer "+api.Token)
		request.Header.Set("Content-Type", "application/octet-stream")
		recorder = httptest.NewRecorder()
		api.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"invalid_input"`) {
			t.Fatalf("malformed query %q = %d %s", rawQuery, recorder.Code, recorder.Body.String())
		}
	}

	api.busy.Lock()
	request = httptest.NewRequest(http.MethodPost, "/v1/speakers/identify", strings.NewReader("audio"))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	api.busy.Unlock()
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"busy"`) {
		t.Fatalf("busy = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestSpeakerListSurvivesWorkbenchRestart(t *testing.T) {
	root := t.TempDir()
	config := recognition.Config{ModelSHA256: strings.Repeat("a", 64), FeatureVersion: "test"}
	profile, err := recognition.NewProfile(strings.Repeat("1", 32), "A", "", config, unitRecognitionVector())
	if err != nil {
		t.Fatal(err)
	}
	if err = recognition.NewRepository(root).Save(context.Background(), []recognition.Profile{profile}); err != nil {
		t.Fatal(err)
	}
	service, err := workbench.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	api := &API{Workbench: service, Token: strings.Repeat("t", 32), Context: context.Background(), jobs: map[string]*generationJob{}}
	request := httptest.NewRequest(http.MethodGet, "/v1/speakers", nil)
	request.Header.Set("Authorization", "Bearer "+api.Token)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "embedding") || !strings.Contains(recorder.Body.String(), profile.ID) {
		t.Fatalf("restart list = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestSpeakerGenerationRouteUsesBindingAndRedactsPrivateDiagnostics(t *testing.T) {
	api, service, profile, voice := configuredProfileAPI(t)
	defer service.Close()
	voiceReference, err := service.MediaFile("voices", voice.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOVOICE_TEST_EXPECTED_VOICE_REF", voiceReference)
	t.Setenv("YOVOICE_TEST_EXPECTED_TEXT", "检查Index绑定")
	markerProof := filepath.Join(t.TempDir(), "marker-observed")
	t.Setenv("YOVOICE_TEST_MARKER_FILE", markerProof)

	request := speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"检查Index绑定","modelId":"index-2.5-q8","mode":"text","emotionText":"调用方情绪"}`)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "audio/wav" || recorder.Header().Get("X-Yovoice-Speaker-Profile") != profile.ID || len(recorder.Body.Bytes()) == 0 {
		t.Fatalf("profile generation = %d %#v %s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	historyBeforeRejections := service.Store.Read().History
	outputBeforeRejections := outputNames(t, service.Store.Root)
	assertNoProfileOutput := func() {
		if historyAfter := service.Store.Read().History; !reflect.DeepEqual(historyBeforeRejections, historyAfter) {
			t.Fatalf("profile rejection changed history: before=%+v after=%+v", historyBeforeRejections, historyAfter)
		}
		if outputAfter := outputNames(t, service.Store.Root); !reflect.DeepEqual(outputBeforeRejections, outputAfter) {
			t.Fatalf("profile rejection changed output files: before=%v after=%v", outputBeforeRejections, outputAfter)
		}
	}
	t.Setenv("YOVOICE_TEST_EXPECTED_TEXT", "")

	request = speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"正文","voiceId":"`+voice.ID+`"}`)
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"invalid_input"`) {
		t.Fatalf("profile primary voice conflict = %d %s", recorder.Code, recorder.Body.String())
	}
	assertNoProfileOutput()

	for _, body := range []string{
		`{"text":"正文","voiceId":null}`,
		`{"text":"正文","referenceText":""}`,
		`{"text":"正文"}{}`,
	} {
		request = speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, body)
		recorder = httptest.NewRecorder()
		api.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"invalid_input"`) {
			t.Fatalf("profile explicit conflict %s = %d %s", body, recorder.Code, recorder.Body.String())
		}
		assertNoProfileOutput()
	}

	tailOverflow := `{"text":"正文"}` + strings.Repeat(" ", 1<<20)
	request = speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, tailOverflow)
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), `"input_too_large"`) {
		t.Fatalf("generate JSON tail limit = %d %s", recorder.Code, recorder.Body.String())
	}
	assertNoProfileOutput()

	profileConfig := recognition.Config{ModelSHA256: profile.ModelSHA256, FeatureVersion: profile.FeatureVersion}
	unbound, err := recognition.NewProfile(strings.Repeat("5", 32), "unbound", "", profileConfig, unitRecognitionVector())
	if err != nil {
		t.Fatal(err)
	}
	deletedInput := filepath.Join(t.TempDir(), "deleted-bound-voice.wav")
	if err = os.WriteFile(deletedInput, wav(), 0600); err != nil {
		t.Fatal(err)
	}
	deletedVoice, err := service.ImportVoice(context.Background(), deletedInput, "deleted")
	if err != nil {
		t.Fatal(err)
	}
	deletedBinding, err := recognition.NewProfile(strings.Repeat("6", 32), "deleted", deletedVoice.ID, profileConfig, unitRecognitionVector())
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := recognition.NewRepository(service.Store.Root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = recognition.NewRepository(service.Store.Root).Save(context.Background(), append(profiles, unbound, deletedBinding)); err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteMedia("voices", deletedVoice.ID); err != nil {
		t.Fatal(err)
	}
	for _, rejection := range []struct {
		name   string
		id     string
		body   string
		status int
		code   string
	}{
		{name: "unbound", id: unbound.ID, body: `{"text":"正文"}`, status: http.StatusBadRequest, code: "unsupported_clone"},
		{name: "deleted voice", id: deletedBinding.ID, body: `{"text":"正文"}`, status: http.StatusNotFound, code: "voice_missing"},
		{name: "missing profile", id: strings.Repeat("7", 32), body: `{"text":"正文"}`, status: http.StatusNotFound, code: "profile"},
		{name: "non-clone model", id: profile.ID, body: `{"text":"正文","modelId":"kokoro-82m-q8"}`, status: http.StatusBadRequest, code: "unsupported_clone"},
	} {
		request = speakerRequest(http.MethodPost, "/v1/speakers/"+rejection.id+"/generate", api.Token, rejection.body)
		recorder = httptest.NewRecorder()
		api.ServeHTTP(recorder, request)
		if recorder.Code != rejection.status || !strings.Contains(recorder.Body.String(), `"`+rejection.code+`"`) {
			t.Fatalf("profile %s rejection = %d %s", rejection.name, recorder.Code, recorder.Body.String())
		}
		assertNoProfileOutput()
	}

	request = speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"正文","speed":10}`)
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"invalid_input"`) {
		t.Fatalf("schema validation = %d %s", recorder.Code, recorder.Body.String())
	}
	assertNoProfileOutput()

	request = speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"私有标记失败","modelId":"index-2.5-q8","mode":"text","emotionText":"调用方情绪"}`)
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"runtime_failed"`) {
		t.Fatalf("profile native failure = %d %s", recorder.Code, recorder.Body.String())
	}
	const marker = "/private/speaker-profile/marker"
	proof, err := os.ReadFile(markerProof)
	if err != nil || string(proof) != marker {
		t.Fatalf("fake engine did not emit the marker proof: %q %v", proof, err)
	}
	activity := service.Store.Read().Activity
	if activity == nil || activity.ErrorCode == nil || *activity.ErrorCode != msg.ErrGenerateFailed || len(activity.ErrorParams) != 0 {
		t.Fatalf("profile activity = %+v", activity)
	}
	for _, name := range []string{"service.log", "last-error.txt", "engine.log.out", "engine.log", "engine-error.log", "last-inference-error.txt"} {
		content, err := os.ReadFile(filepath.Join(service.Store.Root, "logs", name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(content), marker) {
			t.Fatalf("native marker leaked to %s", name)
		}
	}

	const persistenceMarker = "test-remote-profile-final-state-save-failure"
	testkit.FailSettingsUpdate(t, service.Store.Root, "activity", "completed", persistenceMarker)
	request = speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"状态保存失败","modelId":"index-2.5-q8"}`)
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	persistenceBody := recorder.Body.String()
	hasPersistenceCode := strings.Contains(persistenceBody, `"persistence_failed"`)
	leaksPersistenceDetail := strings.Contains(persistenceBody, persistenceMarker) ||
		strings.Contains(persistenceBody, service.Store.Root) ||
		strings.Contains(persistenceBody, "detail")
	if recorder.Code != http.StatusInternalServerError || !hasPersistenceCode || leaksPersistenceDetail {
		t.Fatalf("profile state save failure = %d %s", recorder.Code, persistenceBody)
	}
}

func TestSpeakerGenerationRouteDistinguishesDeadlineAndCancellation(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		api, service, profile, _ := configuredProfileAPI(t)
		defer service.Close()
		api.GenerationTimeout = time.Second

		request := speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"等待取消","modelId":"index-2.5-q8"}`)
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusGatewayTimeout || !strings.Contains(recorder.Body.String(), `"deadline"`) {
			t.Fatalf("profile deadline = %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		api, service, profile, _ := configuredProfileAPI(t)
		defer service.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		request := speakerRequest(http.MethodPost, "/v1/speakers/"+profile.ID+"/generate", api.Token, `{"text":"等待取消","modelId":"index-2.5-q8"}`).WithContext(ctx)
		recorder := httptest.NewRecorder()
		finished := make(chan struct{})
		go func() {
			api.ServeHTTP(recorder, request)
			close(finished)
		}()

		waitForProfileSynthesis(t, service)
		cancel()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatal("caller cancellation did not finish the request")
		}
		if recorder.Code != http.StatusRequestTimeout || !strings.Contains(recorder.Body.String(), `"cancelled"`) {
			t.Fatalf("profile caller cancellation = %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func waitForProfileSynthesis(t *testing.T, service *workbench.Workbench) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		activity := service.Store.Read().Activity
		if activity != nil && activity.Code == msg.ActivitySynthesizing {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("profile generation did not reach native synthesis")
}

func configuredProfileAPI(t *testing.T) (*API, *workbench.Workbench, recognition.Profile, schema.Voice) {
	t.Helper()
	service, err := workbench.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(service.Store.Root, "models", "fake.gguf")
	if err = os.WriteFile(model, []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(t.TempDir(), "reference.wav")
	if err = os.WriteFile(reference, wav(), 0600); err != nil {
		t.Fatal(err)
	}
	voice, err := service.ImportVoice(context.Background(), reference, "bound")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.UpdateVoice(voice.ID, voice.Name, "绑定原文"); err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Update(func(state *schema.State) {
		state.Models = []schema.InstalledModel{{ID: "index-2.5-q8", Path: model}}
		state.RuntimePath = &executable
		state.RuntimeBackend = ptr("cpu")
		state.RuntimeVersion = ptr(catalog.EngineVersion)
		state.Preferences.Backend = "cpu"
	}, true); err != nil {
		t.Fatal(err)
	}
	config := recognition.Config{ModelSHA256: strings.Repeat("a", 64), FeatureVersion: "test"}
	profile, err := recognition.NewProfile(strings.Repeat("4", 32), "bound", voice.ID, config, unitRecognitionVector())
	if err != nil {
		t.Fatal(err)
	}
	if err = recognition.NewRepository(service.Store.Root).Save(context.Background(), []recognition.Profile{profile}); err != nil {
		t.Fatal(err)
	}
	api := &API{Workbench: service, Token: strings.Repeat("t", 32), Context: context.Background(), jobs: map[string]*generationJob{}}
	return api, service, profile, voice
}

func speakerRequest(method, path, token, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func outputNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "outputs"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func unitRecognitionVector() []float64 {
	values := make([]float64, recognition.Dimension)
	values[0] = 1
	return values
}

func TestSpeakerInputErrorAndQueryValidation(t *testing.T) {
	recorder := httptest.NewRecorder()
	overflow := `{"voiceId":"` + strings.Repeat("a", speakerJSONLimit) + `"}`
	body := http.MaxBytesReader(recorder, io.NopCloser(strings.NewReader(overflow)), speakerJSONLimit)
	var value any
	err := decodeStrictJSON(body, &value)
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected MaxBytesError, got %v", err)
	}
	speakerInputError(recorder, err)
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), `"input_too_large"`) {
		t.Fatalf("oversize response = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	speakerWriteError(recorder, msg.Err(msg.ErrBusy, nil))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"busy"`) {
		t.Fatalf("busy response = %d %s", recorder.Code, recorder.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/speakers?unexpected=1", nil)
	if validSpeakerQuery(request, "name", "bindVoiceId") {
		t.Fatal("unexpected query key was accepted")
	}
	request = httptest.NewRequest(http.MethodPost, "/v1/speakers?name=a&name=b", nil)
	if validSpeakerQuery(request, "name", "bindVoiceId") {
		t.Fatal("repeated query key was accepted")
	}
}
