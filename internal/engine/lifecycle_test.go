package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/store"
)

func TestEngineLifecycle(t *testing.T) {
	root := t.TempDir()
	_, e := store.New(root)
	must(t, e)
	engine := New(root)
	defer engine.Stop()
	executable, e := os.Executable()
	must(t, e)
	model := filepath.Join(root, "model.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	d := schema.DefaultDraft()
	out := filepath.Join(root, "outputs", "test.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	must(t, engine.Generate(ctx, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "voice.wav", "", out, func(msg.Code, msg.Params) {}))
	pid := engine.process.Process.Pid
	if seconds, e := audio.Duration(out); e != nil || seconds != 1 {
		t.Fatal(seconds, e)
	}
	must(t, os.Remove(out))
	must(t, engine.Generate(ctx, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "voice.wav", "", out, func(msg.Code, msg.Params) {}))
	if engine.process.Process.Pid != pid {
		t.Fatal("引擎未复用")
	}

	// 旧草稿中的流式标记应被忽略，始终使用完整生成。
	must(t, json.Unmarshal([]byte(`{"modelId":"voxcpm2-q8","streaming":true}`), &d))
	must(t, engine.Generate(ctx, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "", "", out, func(msg.Code, msg.Params) {}))
	if engine.process.Process.Pid == pid {
		t.Fatal("切换模型类型后必须重新启动引擎")
	}
	config, err := os.ReadFile(filepath.Join(root, "runtime", "server.json"))
	must(t, err)
	if !bytes.Contains(config, []byte(`"family":"voxcpm2"`)) || !bytes.Contains(config, []byte(`"mode":"offline"`)) {
		t.Fatal(string(config))
	}
	d.Text = "等待取消"
	cancelled, stop := context.WithCancel(ctx)
	timer := time.AfterFunc(200*time.Millisecond, stop)
	defer timer.Stop()
	if e = engine.Generate(cancelled, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "voice.wav", "", out, func(msg.Code, msg.Params) {}); e == nil {
		t.Fatal("未取消")
	}
	if engine.process != nil {
		t.Fatal("取消后引擎仍存活")
	}
	if runtime.GOOS == "darwin" {
		a, e := catalog.RuntimeArchives("metal")
		must(t, e)
		if len(a) != 1 || !strings.Contains(a[0].Name, "macos") {
			t.Fatal(a)
		}
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		for _, backend := range []string{"cpu", "vulkan"} {
			a, err := catalog.RuntimeArchives(backend)
			must(t, err)
			if len(a) != 1 || a[0].Name != "audio-"+catalog.EngineVersion+"-bin-ubuntu-x64-"+backend+"-portable.tar.gz" || len(a[0].Hash) != 64 {
				t.Fatal(a)
			}
		}
		if _, err := catalog.RuntimeArchives("metal"); err == nil {
			t.Fatal("Linux 不应接受 Metal 后端")
		}
	}
}

func TestProfileGenerationDoesNotPersistNativeDiagnostics(t *testing.T) {
	root := t.TempDir()
	_, err := store.New(root)
	must(t, err)
	service := New(root)
	defer service.Stop()
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(root, "model.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	draft := schema.DefaultDraft()
	draft.Text = "私有标记失败"
	output := filepath.Join(root, "outputs", "profile.wav")
	err = service.GenerateProfile(context.Background(), executable, schema.InstalledModel{ID: draft.ModelID, Path: model}, "cpu", draft, "voice.wav", "", output, func(msg.Code, msg.Params) {})
	if err == nil {
		t.Fatalf("profile error = %v", err)
	}
	for _, name := range []string{"engine.log.out", "engine.log", "last-inference-error.txt"} {
		content, readErr := os.ReadFile(filepath.Join(root, "logs", name))
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), "/private/speaker-profile/marker") {
			t.Fatalf("profile diagnostics leaked to %s", name)
		}
	}
	if err = service.Generate(context.Background(), executable, schema.InstalledModel{ID: draft.ModelID, Path: model}, "cpu", draft, "voice.wav", "", output, func(msg.Code, msg.Params) {}); err == nil {
		t.Fatal("ordinary request should fail")
	}
	for _, name := range []string{"engine.log.out", "engine.log", "last-inference-error.txt"} {
		content, readErr := os.ReadFile(filepath.Join(root, "logs", name))
		must(t, readErr)
		if !strings.Contains(string(content), "/private/speaker-profile/marker") {
			t.Fatalf("ordinary diagnostics changed: %s", name)
		}
	}
}
