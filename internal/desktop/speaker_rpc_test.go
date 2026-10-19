package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"yovoice/internal/msg"
	"yovoice/internal/recognition"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

// 声纹 RPC 只接受内存上传或已保存声音，调用失败后两类临时输入都不能写入
// Voices、档案或普通 State。未配置的隔离 Workbench 能在不启动模型的情况下
// 验证该路由和公开恢复错误。
func TestSpeakerCallRoutesAcceptedSourcesWithoutPersistentSideEffects(t *testing.T) {
	wb, err := workbench.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wb.Close)

	stateBefore := wb.Store.Read()
	encodedWAV := base64.StdEncoding.EncodeToString(wav())
	voiceID := strings.Repeat("a", 32)
	tests := []struct {
		name   string
		method string
		source map[string]string
	}{
		{
			name:   "enroll base64 upload",
			method: "speaker.enroll",
			source: map[string]string{"base64": encodedWAV},
		},
		{
			name:   "enroll saved voice",
			method: "speaker.enroll",
			source: map[string]string{"voiceId": voiceID},
		},
		{
			name:   "identify base64 upload",
			method: "speaker.identify",
			source: map[string]string{"base64": encodedWAV},
		},
		{
			name:   "identify saved voice",
			method: "speaker.identify",
			source: map[string]string{"voiceId": voiceID},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, marshalErr := json.Marshal(map[string]any{
				"requestId": testSpeakerRequestID,
				"source":    test.source,
			})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}

			_, callErr := Call(context.Background(), wb, test.method, request)
			requireSpeakerCallError(t, callErr, msg.ErrSpeakerRuntime)
			if strings.Contains(callErr.Error(), wb.Store.Root) {
				t.Fatalf("public speaker error leaked the private data root: %v", callErr)
			}
		})
	}

	profiles, err := wb.ListSpeakers()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Fatalf("source calls persisted speaker profiles: %+v", profiles)
	}
	if stateAfter := wb.Store.Read(); !reflect.DeepEqual(stateBefore, stateAfter) {
		t.Fatalf("source calls changed ordinary State: before=%+v after=%+v", stateBefore, stateAfter)
	}
	for _, pattern := range []string{"upload-*", "recognition-*"} {
		leftovers, globErr := filepath.Glob(filepath.Join(wb.Store.Root, "downloads", pattern))
		if globErr != nil {
			t.Fatal(globErr)
		}
		if len(leftovers) != 0 {
			t.Fatalf("source calls left temporary inputs for %s: %v", pattern, leftovers)
		}
	}
}

func TestSpeakerCallExposesPublicResultsAndStrictlyRejectsMalformedRequests(t *testing.T) {
	wb, err := workbench.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wb.Close)

	runtime, err := Call(context.Background(), wb, "speaker.runtime", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := runtime.(workbench.SpeakerRuntime); !ok || actual.Configured {
		t.Fatalf("runtime = %#v", runtime)
	}

	list, err := Call(context.Background(), wb, "speaker.list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	profiles, ok := list.([]recognition.PublicProfile)
	if !ok || len(profiles) != 0 {
		t.Fatalf("speaker.list = %#v", list)
	}

	profileID := strings.Repeat("b", 32)
	deleted, err := Call(context.Background(), wb, "speaker.delete", json.RawMessage(`{"id":"`+profileID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	deleteReply, ok := deleted.(map[string]any)
	if !ok || deleteReply["id"] != profileID || deleteReply["deleted"] != true {
		t.Fatalf("speaker.delete = %#v", deleted)
	}

	draft := schema.DefaultDraft()
	generateRequest, err := json.Marshal(speakerGenerateRequest{
		RequestID: testSpeakerRequestID,
		ProfileID: profileID,
		Draft:     draft,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, generateErr := Call(context.Background(), wb, "speaker.generate", generateRequest)
	requireSpeakerCallError(t, generateErr, msg.ErrSpeakerProfile)

	var cancelledRequestID string
	cancelContext := context.WithValue(context.Background(), speakerCancelContextKey{}, func(requestID string) bool {
		cancelledRequestID = requestID
		return true
	})
	cancelled, err := Call(cancelContext, wb, "speaker.cancel", json.RawMessage(`{"requestId":"`+testSpeakerRequestID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	cancelReply, ok := cancelled.(map[string]bool)
	if !ok || !cancelReply["cancelRequested"] || cancelledRequestID != testSpeakerRequestID {
		t.Fatalf("speaker.cancel = %#v, request ID = %q", cancelled, cancelledRequestID)
	}

	malformed := []struct {
		name   string
		method string
		data   json.RawMessage
	}{
		{
			name:   "runtime unknown field",
			method: "speaker.runtime",
			data:   json.RawMessage(`{"extra":true}`),
		},
		{
			name:   "list trailing value",
			method: "speaker.list",
			data:   json.RawMessage(`{} {}`),
		},
		{
			name:   "enroll unknown source field",
			method: "speaker.enroll",
			data:   json.RawMessage(`{"requestId":"` + testSpeakerRequestID + `","source":{"base64":"YQ==","path":"private.wav"}}`),
		},
		{
			name:   "delete unknown field",
			method: "speaker.delete",
			data:   json.RawMessage(`{"id":"` + profileID + `","extra":true}`),
		},
		{
			name:   "generate caller supplied binding",
			method: "speaker.generate",
			data: json.RawMessage(`{"requestId":"` + testSpeakerRequestID + `","profileId":"` + profileID + `","draft":{` +
				`"voiceId":"` + strings.Repeat("c", 32) + `"}}`),
		},
		{
			name:   "cancel invalid request ID",
			method: "speaker.cancel",
			data:   json.RawMessage(`{"requestId":"not-a-request-id"}`),
		},
	}
	for _, test := range malformed {
		t.Run(test.name, func(t *testing.T) {
			_, callErr := Call(context.Background(), wb, test.method, test.data)
			requireSpeakerCallError(t, callErr, msg.ErrSpeakerRequestInvalid)
		})
	}

	state := wb.Store.Read()
	if len(state.Voices) != 0 || len(state.History) != 0 {
		t.Fatalf("speaker RPC wrote public State: %+v", state)
	}
}

func TestSpeakerHTTPRejectsConcurrentRequestBeforeDispatch(t *testing.T) {
	handler, _, call := newServer(t)
	firstRequestID := strings.Repeat("d", 32)
	secondRequestID := strings.Repeat("e", 32)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(releaseFirst)

	unexpectedDispatch := make(chan string, 1)
	handler.callOverride = func(ctx context.Context, method string, data json.RawMessage) (any, error) {
		requestID, _ := speakerRequestID(method, data)
		if requestID != firstRequestID {
			unexpectedDispatch <- requestID
			return nil, errors.New("second speaker request reached application dispatch")
		}
		close(started)
		select {
		case <-release:
			return map[string]string{"status": "finished"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	firstFinished := make(chan error, 1)
	go func() {
		_, callErr := call("speaker.identify", map[string]any{
			"requestId": firstRequestID,
			"source":    map[string]string{"voiceId": strings.Repeat("a", 32)},
		})
		firstFinished <- callErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first speaker request did not enter the operation registry")
	}

	reply, callErr := call("speaker.identify", map[string]any{
		"requestId": secondRequestID,
		"source":    map[string]string{"voiceId": strings.Repeat("b", 32)},
	})
	if callErr == nil {
		t.Fatalf("concurrent speaker request unexpectedly succeeded: %#v", reply)
	}
	errorReply, ok := reply["error"].(map[string]any)
	if !ok || errorReply["code"] != string(msg.ErrSpeakerBusy) {
		t.Fatalf("concurrent speaker error = %#v", reply)
	}
	select {
	case requestID := <-unexpectedDispatch:
		t.Fatalf("busy request reached application dispatch: %q", requestID)
	default:
	}

	releaseFirst()
	select {
	case callErr := <-firstFinished:
		if callErr != nil {
			t.Fatalf("first speaker request = %v", callErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first speaker request did not finish after release")
	}
	if handler.speakerOperations.cancel(firstRequestID) {
		t.Fatal("completed speaker request retained a cancellation handle")
	}
	state := handler.Workbench.Store.Read()
	if len(state.Voices) != 0 || len(state.History) != 0 {
		t.Fatalf("blocked speaker request mutated public State: %+v", state)
	}
}

func TestSpeakerDeleteStorageFailureIsPublicAndKeepsProfile(t *testing.T) {
	// Windows 的目录权限不保证阻止 CreateTemp/Rename；这个测试只在 POSIX 上
	// 使用真实目录权限注入私有档案写入失败，不能把该注入误称为跨平台行为。
	if runtime.GOOS == "windows" {
		t.Skip("directory permission storage-failure injection requires POSIX semantics")
	}

	wb, err := workbench.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer wb.Close()

	profileID := strings.Repeat("f", 32)
	config := recognition.Config{
		ModelSHA256:    strings.Repeat("a", 64),
		FeatureVersion: "storage-failure-test",
	}
	embedding := make([]float64, recognition.Dimension)
	embedding[0] = 1
	profile, err := recognition.NewProfile(profileID, "保存失败档案", "", config, embedding)
	if err != nil {
		t.Fatal(err)
	}
	if err := recognition.NewRepository(wb.Store.Root).Save(context.Background(), []recognition.Profile{profile}); err != nil {
		t.Fatal(err)
	}

	profilesBefore, err := wb.ListSpeakers()
	if err != nil || len(profilesBefore) != 1 || profilesBefore[0].ID != profileID {
		t.Fatalf("stored profiles before deletion = %+v, %v", profilesBefore, err)
	}
	stateBefore := wb.Store.Read()

	if err := os.Chmod(wb.Store.Root, 0500); err != nil {
		t.Fatal(err)
	}
	restored := false
	restoreRoot := func() {
		if restored {
			return
		}
		restored = true
		if err := os.Chmod(wb.Store.Root, 0700); err != nil {
			t.Fatalf("restore workbench root permissions: %v", err)
		}
	}
	t.Cleanup(restoreRoot)

	_, callErr := Call(context.Background(), wb, "speaker.delete", json.RawMessage(`{"id":"`+profileID+`"}`))
	restoreRoot()
	requireSpeakerCallError(t, callErr, msg.ErrSpeakerStorage)

	profilesAfter, err := wb.ListSpeakers()
	if err != nil || !reflect.DeepEqual(profilesAfter, profilesBefore) {
		t.Fatalf("storage failure changed profiles: before=%+v after=%+v err=%v", profilesBefore, profilesAfter, err)
	}
	if stateAfter := wb.Store.Read(); !reflect.DeepEqual(stateAfter, stateBefore) {
		t.Fatalf("storage failure changed public State: before=%+v after=%+v", stateBefore, stateAfter)
	}
}

func requireSpeakerCallError(t *testing.T, err error, code msg.Code) {
	t.Helper()
	var callErr *msg.CallError
	if !errors.As(err, &callErr) || callErr.Code != code {
		t.Fatalf("speaker error = %#v, want code %s", err, code)
	}
}
