package desktop

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

func TestHTTPBoundaryAndEvents(t *testing.T) {
	w, e := workbench.New(t.TempDir())
	must(t, e)
	defer w.Close()
	secret := strings.Repeat("a", 64)
	handler := &Server{Workbench: w, Assets: t.TempDir(), Secret: secret}
	server := httptest.NewServer(handler)
	defer server.Close()
	request := func(path, origin string, auth bool) *http.Request {
		r, e := http.NewRequest("GET", server.URL+path, nil)
		must(t, e)
		if auth {
			r.AddCookie(&http.Cookie{Name: "vw-" + secret[:12], Value: secret})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	for _, r := range []*http.Request{request("/", "", false), request("/", "https://untrusted.invalid", true)} {
		res, e := server.Client().Do(r)
		must(t, e)
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatal(res.StatusCode)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := request("/api/state-events", "", true).WithContext(ctx)
	res, e := server.Client().Do(r)
	must(t, e)
	defer res.Body.Close()
	buffer := make([]byte, 8192)
	n, e := res.Body.Read(buffer)
	must(t, e)
	if !bytes.Contains(buffer[:n], []byte(`"event":"state"`)) {
		t.Fatal(string(buffer[:n]))
	}
	d := schema.DefaultDraft()
	d.Title = "事件更新"
	must(t, w.SaveDraft(d))
	n, e = res.Body.Read(buffer)
	must(t, e)
	if !bytes.Contains(buffer[:n], []byte("事件更新")) {
		t.Fatal(string(buffer[:n]))
	}
	file := filepath.Join(w.Store.Root, "input.wav")
	must(t, os.WriteFile(file, wav(), 0600))
	v, e := w.ImportVoice(context.Background(), file, "")
	must(t, e)
	r = request("/media/voices/"+v.FileName, "", true)
	r.Header.Set("Range", "bytes=0-43")
	audio, e := server.Client().Do(r)
	must(t, e)
	b, e := io.ReadAll(audio.Body)
	audio.Body.Close()
	must(t, e)
	if audio.StatusCode != 206 || len(b) != 44 {
		t.Fatal(audio.StatusCode, len(b))
	}
}

func TestSpeakerOperationRegistryOnlyCancelsItsOwnLiveRequest(t *testing.T) {
	var registry speakerOperationRegistry
	requestA := strings.Repeat("a", 32)
	requestB := strings.Repeat("b", 32)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	entryA := registry.register(requestA, cancelA)
	if entryA == nil {
		t.Fatal("first request was not registered")
	}
	if duplicate := registry.register(requestA, func() {}); duplicate != nil {
		t.Fatal("duplicate request replaced the original cancellation handle")
	}
	if concurrent := registry.register(requestB, func() {}); concurrent != nil {
		t.Fatal("different requestId bypassed the single expensive speaker operation limit")
	}
	if !registry.cancel(requestA) {
		t.Fatal("registered request was not cancelled")
	}
	<-ctxA.Done()
	registry.finish(requestA, entryA)
	if registry.cancel(requestA) {
		t.Fatal("finished request retained a cancellation tombstone")
	}

	// A finished request releases the single slot. Its late finish must never
	// delete the successor's cancellation handle.
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	entryB := registry.register(requestB, cancelB)
	if entryB == nil {
		t.Fatal("successor request was not registered")
	}
	registry.finish(requestA, entryA)
	if !registry.cancel(requestB) {
		t.Fatal("late completion of A affected B registration")
	}
	<-ctxB.Done()
}
