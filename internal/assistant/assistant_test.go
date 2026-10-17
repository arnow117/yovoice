package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/msg"
	"yovoice/internal/platform"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMain(m *testing.M) {
	fakeocho.Run()
	os.Exit(m.Run())
}

// testServer 返回带 echo/submit 工具的进程内 MCP server：submit 第一次调用被拒绝，之后成功。
func testServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "yovoice", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "回显输入"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Text string `json:"text"`
	}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"echo": in.Text}, nil
	})
	rejected := false
	mcp.AddTool(server, &mcp.Tool{Name: "submit", Description: "提交结果"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		OK bool `json:"ok"`
	}) (*mcp.CallToolResult, any, error) {
		if !rejected {
			rejected = true
			return nil, nil, errors.New("first submission is invalid: fix it")
		}
		return nil, map[string]any{"stored": true}, nil
	})
	return server
}

// fakeManager 用测试二进制作为假 ocho 启动 Manager，并把计划写入临时文件。
func fakeManager(t *testing.T, plan any, extraEnv ...string) (*Manager, *Config) {
	t.Helper()
	data, err := json.Marshal(plan)
	must(t, err)
	planPath := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(planPath, data, 0600))
	executable, err := os.Executable()
	must(t, err)
	root := t.TempDir()
	manager := New(root, Options{Tools: []string{"echo", "submit"}, NewServer: testServer})
	t.Cleanup(manager.Stop)
	t.Cleanup(func() {
		if t.Failed() {
			if log, e := os.ReadFile(filepath.Join(root, "logs", "assistant.log")); e == nil {
				t.Logf("child log:\n%s", log)
			}
		}
	})
	env := append([]string{fakeocho.PlanEnv + "=" + planPath}, extraEnv...)
	return manager, &Config{Executable: executable, Model: "fake-model", Env: env}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// 完整往返：握手、MCP 桥接（tools/list、tools/call）、事件与文本汇总。
func TestAskBridgesMCPAndCollectsText(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{{
		Call: "echo", Args: json.RawMessage(`{"text":"你好"}`),
	}, {
		Assistant: "完成了。",
	}, {
		Result: "success",
	}}}})
	var events []Event
	result, err := manager.Ask(context.Background(), *cfg, "写一段", func(e Event) { events = append(events, e) })
	must(t, err)
	if result.Subtype != "success" || !strings.Contains(result.Text, "完成了。") {
		t.Fatalf("结果不对：%+v", result)
	}
	found := false
	for _, e := range events {
		if e.Kind == EventToolResult && e.Tool == "echo" && !e.ToolError {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有 echo 工具结果事件：%+v", events)
	}
}

// submit 首次被拒再成功：两次工具结果事件，第一次出错、第二次通过。
func TestSubmitRejectedThenAccepted(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{{
		Call: "submit", Args: json.RawMessage(`{"ok":true}`),
	}, {
		Call: "submit", Args: json.RawMessage(`{"ok":true}`),
	}, {
		Result: "success",
	}}}})
	var results []Event
	_, err := manager.Ask(context.Background(), *cfg, "写一段", func(e Event) {
		if e.Kind == EventToolResult {
			results = append(results, e)
		}
	})
	must(t, err)
	if len(results) != 2 || !results[0].ToolError || results[0].Tool != "submit" || results[1].ToolError {
		t.Fatalf("两次提交的事件不对：%+v", results)
	}
}

// 中断：取消 ctx 后宿主发 interrupt，假 ocho 回 error_interrupted，Ask 返回取消错误且进程可继续用。
func TestAskInterrupt(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{{
		WaitInterrupt: true,
	}}, {{
		Result: "success",
	}}}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	if _, err := manager.Ask(ctx, *cfg, "写一段", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("应返回取消错误", err)
	}
	if _, err := manager.Ask(context.Background(), *cfg, "再来", nil); err != nil {
		t.Fatal("中断后进程应可继续", err)
	}
}

// 进程崩溃与协议乱码都返回明确错误码，且下次 Ask 能重新启动。
func TestCrashAndRecover(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{{
		Crash: 1,
	}}, {{
		Result: "success",
	}}}})
	var ce *msg.CallError
	_, err := manager.Ask(context.Background(), *cfg, "写一段", nil)
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResponse {
		t.Fatal("崩溃应返回 aiResponse", err)
	}
	if _, err = manager.Ask(context.Background(), *cfg, "再试", nil); err != nil {
		t.Fatal("崩溃后应能重新启动", err)
	}
}

func TestProtocolGarbage(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{{
		BadLine: "这不是 JSON",
	}}}})
	var ce *msg.CallError
	_, err := manager.Ask(context.Background(), *cfg, "写一段", nil)
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResponse {
		t.Fatal("协议乱码应返回 aiResponse", err)
	}
}

func TestCrashBeforeHandshake(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{CrashEarly: 1})
	var ce *msg.CallError
	_, err := manager.Ask(context.Background(), *cfg, "写一段", nil)
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResponse {
		t.Fatal("握手前崩溃应返回 aiResponse", err)
	}
}

func TestMissingExecutable(t *testing.T) {
	manager := New(t.TempDir(), Options{Tools: []string{"echo"}, NewServer: func() *mcp.Server { return mcp.NewServer(&mcp.Implementation{Name: "yovoice", Version: "test"}, nil) }})
	_, err := manager.Ask(context.Background(), Config{Executable: filepath.Join(t.TempDir(), "missing"), Model: "m"}, "hi", nil)
	var ce *msg.CallError
	if !errors.As(err, &ce) || ce.Code != msg.ErrAssistantMissing {
		t.Fatal("缺失应返回 assistantMissing", err)
	}
}

// 同一时间只允许一轮 Ask。
func TestAskBusy(t *testing.T) {
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{{{
		WaitInterrupt: true,
	}}}})
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, err := manager.Ask(ctx, *cfg, "第一轮", nil)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	_, err := manager.Ask(context.Background(), *cfg, "第二轮", nil)
	var ce *msg.CallError
	if !errors.As(err, &ce) || ce.Code != msg.ErrBusy {
		t.Fatal("并发应返回 busy", err)
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("第一轮应被取消", err)
	}
}

// 密钥只进环境变量，不进命令行；Skill 写入工作区；pid 文件随进程启停。
func TestSecretsStayOutOfCommandline(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(planPath, []byte(`{"turns":[[{"result":"success"}]]}`), 0600))
	dump := filepath.Join(t.TempDir(), "dump.txt")
	executable, err := os.Executable()
	must(t, err)
	manager := New(root, Options{Tools: []string{"echo"}, NewServer: func() *mcp.Server { return mcp.NewServer(&mcp.Implementation{Name: "yovoice", Version: "test"}, nil) }})
	cfg := Config{Executable: executable, Model: "m", Env: []string{
		fakeocho.PlanEnv + "=" + planPath,
		fakeocho.DumpEnv + "=" + dump,
		"ANTHROPIC_API_KEY=sk-test-secret-000",
	}}
	_, err = manager.Ask(context.Background(), cfg, "写一段", nil)
	must(t, err)
	raw, err := os.ReadFile(dump)
	must(t, err)
	dumpText := string(raw)
	argv := dumpText[strings.Index(dumpText, "ARGV")+5 : strings.Index(dumpText, "ENV")]
	if strings.Contains(argv, "sk-test-secret-000") {
		t.Fatal("密钥出现在命令行")
	}
	if !strings.Contains(dumpText, "ANTHROPIC_API_KEY=sk-test-secret-000") {
		t.Fatal("密钥应在子进程环境变量中")
	}
	if !strings.Contains(argv, "--append-system-prompt-file") {
		t.Fatal("命令行应包含系统说明文件参数")
	}
	if _, err = os.Stat(filepath.Join(root, "assistant", "workspace", ".agents", "skills", "yovoice-score", "SKILL.md")); err != nil {
		t.Fatal("Skill 应写入工作区", err)
	}
	if _, err = os.Stat(pidPath(root)); err != nil {
		t.Fatal("运行期间应有 pid 文件", err)
	}
	manager.Stop()
	if _, err = os.Stat(pidPath(root)); !os.IsNotExist(err) {
		t.Fatal("结束后应删除 pid 文件", err)
	}
}

// 换模型用 set_model 复用进程，换环境变量重启进程。
func TestModelChangeReusesAndEnvChangeRestarts(t *testing.T) {
	turn := func() []fakeocho.Step { return []fakeocho.Step{{AssistantPID: true}, {Result: "success"}} }
	manager, cfg := fakeManager(t, fakeocho.Plan{Turns: [][]fakeocho.Step{turn(), turn(), turn()}})
	ask := func(c Config) string {
		result, err := manager.Ask(context.Background(), c, "写一段", nil)
		must(t, err)
		return result.Text
	}
	first := ask(*cfg)
	// 换模型：同一进程。
	withModel := *cfg
	withModel.Model = "fake-model-2"
	if second := ask(withModel); second != first {
		t.Fatalf("换模型应复用进程：%s != %s", first, second)
	}
	// 换环境变量：重启进程。
	withEnv := withModel
	withEnv.Env = append(append([]string{}, withModel.Env...), "OPENAI_API_KEY=sk-another")
	if third := ask(withEnv); third == first {
		t.Fatal("换环境变量应重启进程")
	}
}

// 残留进程回收：pid 文件指向的进程命令行包含本数据目录特征时被结束。
func TestReapOrphan(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(planPath, []byte(`{"holdForever":true}`), 0600))
	executable, err := os.Executable()
	must(t, err)
	// 手工启动一个残留进程：命令行带上本数据目录的 system.md 路径以模拟真实启动参数。
	marker := systemPromptPath(root)
	cmd := exec.Command(executable, "--print", "--append-system-prompt-file", marker)
	cmd.Env = append(os.Environ(), fakeocho.PlanEnv+"="+planPath)
	platform.ConfigureProcess(cmd)
	must(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	must(t, os.MkdirAll(filepath.Join(root, "assistant"), 0700))
	must(t, os.WriteFile(pidPath(root), []byte(fmt.Sprint(cmd.Process.Pid)), 0600))
	New(root, Options{})
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("残留进程应被回收")
	}
	if _, err = os.Stat(pidPath(root)); !os.IsNotExist(err) {
		t.Fatal("回收后应删除 pid 文件", err)
	}
}
