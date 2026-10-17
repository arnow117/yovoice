package assistant

import (
	"bufio"
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
	"yovoice/internal/msg"
	"yovoice/internal/platform"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 测试用测试二进制自身扮演 ocho：子进程经 YOVOICE_FAKE_OCHO_PLAN 切换为假运行时，
// 按计划执行 MCP 握手、工具调用、结果与崩溃等动作。
func TestMain(m *testing.M) {
	if plan := os.Getenv(fakePlanEnv); plan != "" {
		os.Exit(fakeOchoMain(plan))
	}
	os.Exit(m.Run())
}

const (
	fakePlanEnv = "YOVOICE_FAKE_OCHO_PLAN"
	fakeDumpEnv = "YOVOICE_FAKE_OCHO_DUMP"
)

type fakeStep struct {
	MCP           string          `json:"mcp,omitempty"`    // 发送一个 JSON-RPC 请求并等响应
	Notify        string          `json:"notify,omitempty"` // 发送一个 JSON-RPC 通知
	Call          string          `json:"call,omitempty"`   // 发送 tools/call
	Args          json.RawMessage `json:"args,omitempty"`
	Assistant     string          `json:"assistant,omitempty"`    // 发一条助手文本
	AssistantPID  bool            `json:"assistantPid,omitempty"` // 发一条内容为本进程 PID 的助手文本
	SleepMS       int             `json:"sleepMs,omitempty"`
	Result        string          `json:"result,omitempty"`        // 结束本轮
	Crash         int             `json:"crash,omitempty"`         // 立即以状态码退出
	WaitInterrupt bool            `json:"waitInterrupt,omitempty"` // 等宿主 interrupt 后回 error_interrupted
	BadLine       string          `json:"badLine,omitempty"`       // 向 stdout 输出一行非 JSON
}

type fakePlan struct {
	CrashEarly  int          `json:"crashEarly,omitempty"`
	HoldForever bool         `json:"holdForever,omitempty"`
	Turns       [][]fakeStep `json:"turns,omitempty"`
}

// fakeOchoMain 是假 ocho 的入口：先完成与宿主的 initialize 握手和 MCP 握手，
// 之后的每个 user 消息按 Turns 里对应的一段计划执行。
func fakeOchoMain(planPath string) int {
	data, err := os.ReadFile(planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake ocho:", err)
		return 2
	}
	var plan fakePlan
	if err = json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintln(os.Stderr, "fake ocho:", err)
		return 2
	}
	if dump := os.Getenv(fakeDumpEnv); dump != "" {
		_ = os.WriteFile(dump, []byte("ARGV\n"+strings.Join(os.Args, "\n")+"\nENV\n"+strings.Join(os.Environ(), "\n")+"\n"), 0600)
	}
	if plan.CrashEarly > 0 {
		os.Exit(plan.CrashEarly)
	}
	if plan.HoldForever {
		time.Sleep(time.Hour)
		return 0
	}
	out := bufio.NewWriter(os.Stdout)
	write := func(v any) {
		payload, _ := json.Marshal(v)
		_, _ = out.Write(append(payload, '\n'))
		_ = out.Flush()
	}
	in := bufio.NewReader(os.Stdin)
	nextID := 100
	waitingInterrupt := false
	// 轮次进度落在计划文件旁：进程重启（崩溃恢复测试）后从下一个 turn 继续。
	statePath := planPath + ".state"
	turnBytes, _ := os.ReadFile(statePath)
	turn := 0
	_, _ = fmt.Sscanf(string(turnBytes), "%d", &turn)
	// queued 存放等待 MCP 响应期间提前收到的行（如宿主的 user 消息），主循环稍后处理。
	queued := []map[string]any{}
	// waitControl 只读 stdin，等待指定 control 响应；期间收到的其他行排队留给主循环。
	waitControl := func(id string) map[string]any {
		for {
			raw, err := in.ReadString('\n')
			if err != nil {
				os.Exit(0)
			}
			var line map[string]any
			if json.Unmarshal([]byte(raw), &line) != nil {
				continue
			}
			if line["type"] != "control_response" {
				queued = append(queued, line)
				continue
			}
			body, _ := line["response"].(map[string]any)
			if body["request_id"] == id {
				return body
			}
		}
	}
	// next 供主循环用：先消费排队的行，再读 stdin。
	next := func() (map[string]any, bool) {
		if len(queued) > 0 {
			line := queued[0]
			queued = queued[1:]
			return line, true
		}
		raw, err := in.ReadString('\n')
		if err != nil {
			return nil, false
		}
		var line map[string]any
		if json.Unmarshal([]byte(raw), &line) != nil {
			return nil, true
		}
		return line, true
	}
	controlSuccess := func(id string, response map[string]any) {
		write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": id, "response": response}})
	}
	// sendMCP 发送 mcp_message 并等宿主的 control_response，返回其中的 JSON-RPC 响应。
	sendMCP := func(payload map[string]any) map[string]any {
		id := fmt.Sprintf("fake-%d", nextID)
		nextID++
		write(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "mcp_message", "server_name": "yovoice", "message": payload}})
		body := waitControl(id)
		if body["subtype"] != "success" {
			fmt.Fprintf(os.Stderr, "fake ocho: mcp control failed: %v\n", body)
			os.Exit(3)
		}
		response, _ := body["response"].(map[string]any)
		mcpResponse, _ := response["mcp_response"].(map[string]any)
		return mcpResponse
	}
	callMCP := func(method string, params map[string]any) map[string]any {
		id := float64(nextID)
		nextID++
		payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			payload["params"] = params
		}
		return sendMCP(payload)
	}
	notifyMCP := func(method string) {
		sendMCP(map[string]any{"jsonrpc": "2.0", "method": method})
	}
	for {
		line, ok := next()
		if !ok {
			return 0
		}
		if line == nil {
			continue
		}
		switch line["type"] {
		case "control_request":
			id, _ := line["request_id"].(string)
			request, _ := line["request"].(map[string]any)
			subtype, _ := request["subtype"].(string)
			switch subtype {
			case "initialize":
				controlSuccess(id, map[string]any{"pid": os.Getpid()})
				// 真实 ocho 在初始化期间完成 MCP 握手，这里同样先握手再等用户消息。
				if response := callMCP("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "fake-ocho", "version": "1"}}); response == nil {
					fmt.Fprintln(os.Stderr, "fake ocho: initialize response missing")
					os.Exit(3)
				}
				notifyMCP("notifications/initialized")
				tools := callMCP("tools/list", map[string]any{})
				result, _ := tools["result"].(map[string]any)
				fmt.Fprintf(os.Stderr, "fake ocho tools: %v\n", result["tools"])
			case "set_model":
				controlSuccess(id, map[string]any{})
			case "interrupt":
				controlSuccess(id, map[string]any{})
				if waitingInterrupt {
					waitingInterrupt = false
					write(map[string]any{"type": "result", "subtype": "error_interrupted"})
				}
			default:
				write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": id, "error": "unsupported"}})
			}
		case "user":
			if len(plan.Turns) == 0 {
				continue
			}
			steps := plan.Turns[min(turn, len(plan.Turns)-1)]
			turn++
			_ = os.WriteFile(statePath, []byte(fmt.Sprintf("%d", turn)), 0600)
			for _, step := range steps {
				switch {
				case step.MCP != "":
					callMCP(step.MCP, nil)
				case step.Notify != "":
					notifyMCP(step.Notify)
				case step.Call != "":
					var args any
					if len(step.Args) > 0 {
						_ = json.Unmarshal(step.Args, &args)
					}
					// 与真实 ocho 一致：模型发起工具调用时先输出 tool_use 块，结果以 tool_result 块回给模型。
					useID := fmt.Sprintf("toolu-%d", nextID)
					write(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": useID, "name": "mcp__yovoice__" + step.Call, "input": args}}}})
					// JSON-RPC 层用不带前缀的工具名，ocho 的 mcp__yovoice__ 前缀只在流消息里出现。
					response := callMCP("tools/call", map[string]any{"name": step.Call, "arguments": args})
					result, _ := response["result"].(map[string]any)
					isError, _ := result["isError"].(bool)
					if _, failed := response["error"]; failed {
						isError = true
					}
					write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": useID, "is_error": isError}}}})
				case step.Assistant != "" || step.AssistantPID:
					text := step.Assistant
					if step.AssistantPID {
						text = fmt.Sprint(os.Getpid())
					}
					write(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
				case step.SleepMS > 0:
					time.Sleep(time.Duration(step.SleepMS) * time.Millisecond)
				case step.BadLine != "":
					_, _ = fmt.Fprintln(out, step.BadLine)
					_ = out.Flush()
				case step.Crash > 0:
					_ = out.Flush()
					os.Exit(step.Crash)
				case step.WaitInterrupt:
					waitingInterrupt = true
				case step.Result != "":
					write(map[string]any{"type": "result", "subtype": step.Result})
				}
			}
		case "control_response":
			// 假 ocho 不等待宿主主动发来的响应；忽略。
		}
	}
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
	env := append([]string{fakePlanEnv + "=" + planPath}, extraEnv...)
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
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{{{
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
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{{{
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
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{{{
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
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{{{
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
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{{{
		BadLine: "这不是 JSON",
	}}}})
	var ce *msg.CallError
	_, err := manager.Ask(context.Background(), *cfg, "写一段", nil)
	if !errors.As(err, &ce) || ce.Code != msg.ErrAIResponse {
		t.Fatal("协议乱码应返回 aiResponse", err)
	}
}

func TestCrashBeforeHandshake(t *testing.T) {
	manager, cfg := fakeManager(t, fakePlan{CrashEarly: 1})
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
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{{{
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
		fakePlanEnv + "=" + planPath,
		fakeDumpEnv + "=" + dump,
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
	turn := func() []fakeStep { return []fakeStep{{AssistantPID: true}, {Result: "success"}} }
	manager, cfg := fakeManager(t, fakePlan{Turns: [][]fakeStep{turn(), turn(), turn()}})
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
	cmd.Env = append(os.Environ(), fakePlanEnv+"="+planPath)
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
