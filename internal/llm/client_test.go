package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/testkit"
)

// 预设与界面副本逐字节一致。
func TestPresetsMatchWeb(t *testing.T) {
	web, err := os.ReadFile(testkit.RepoFile(t, "web", "src", "shared", "lib", "ai-presets.json"))
	testkit.Must(t, err)
	if !bytes.Equal(presetsJSON, web) {
		t.Fatal("internal/llm/presets.json 与 web/src/shared/lib/ai-presets.json 不一致")
	}
	for _, p := range Presets {
		if p.Key == "" || p.Name == "" || (p.Endpoint != "custom" && !strings.HasPrefix(p.BaseURL, "http")) {
			t.Errorf("预设 %+v 不完整", p)
		}
	}
}

func TestJoin(t *testing.T) {
	for _, c := range [][3]string{
		{"https://api.openai.com/v1/", "/models", "https://api.openai.com/v1/models"},
		{"https://api.openai.com/v1", "models", "https://api.openai.com/v1/models"},
		{"https://x.test/api/v1/models", "/models", "https://x.test/api/v1/models"},
		{"https://x.test/v1", "https://y.test/list", "https://y.test/list"},
		{"https://x.test/v1", "", "https://x.test/v1"},
	} {
		if got := Join(c[0], c[1]); got != c[2] {
			t.Errorf("Join(%q, %q) = %q, 期望 %q", c[0], c[1], got, c[2])
		}
	}
}

// 三种协议的路径、鉴权头、请求体与回复解析（连通测试）。
func TestPingProtocols(t *testing.T) {
	cases := []struct {
		format, path, reply string
		check               func(*http.Request, map[string]any) error
	}{
		{FormatChat, "/v1/chat/completions", `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`, func(r *http.Request, body map[string]any) error {
			if r.Header.Get("Authorization") != "Bearer sk-test-key-1234567" || r.Header.Get("x-api-key") != "" {
				return errors.New("chat 鉴权头不对")
			}
			if messages := body["messages"].([]any); len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || body["max_tokens"] != float64(256) {
				return errors.New("chat 请求体不对")
			}
			return nil
		}},
		{FormatAnthropic, "/v1/messages", `{"content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"OK"}],"stop_reason":"end_turn"}`, func(r *http.Request, body map[string]any) error {
			if r.Header.Get("x-api-key") != "sk-test-key-1234567" || r.Header.Get("anthropic-version") == "" || body["system"] != "Reply with OK." {
				return errors.New("anthropic 请求不对")
			}
			return nil
		}},
		{FormatResponses, "/v1/responses", `{"status":"completed","output":[{"content":[{"type":"output_text","text":"OK"}]}]}`, func(r *http.Request, body map[string]any) error {
			if body["instructions"] != "Reply with OK." || body["store"] != false {
				return errors.New("responses 请求体不对")
			}
			return nil
		}},
	}
	for _, c := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if r.URL.Path != c.path {
				t.Errorf("%s 路径 %s，期望 %s", c.format, r.URL.Path, c.path)
			}
			if e := c.check(r, body); e != nil {
				t.Error(e)
			}
			_, _ = w.Write([]byte(c.reply))
		}))
		base := server.URL + "/v1"
		if c.format == FormatAnthropic {
			base = server.URL
		}
		err := Ping(context.Background(), server.Client(), Config{Format: c.format, BaseURL: base, Key: "sk-test-key-1234567", Model: "m"})
		server.Close()
		if err != nil {
			t.Errorf("%s: %v", c.format, err)
		}
	}
}

// OrcaRouter 免费模型的四种拒绝按 metadata.reason 区分，窗口已满时带回等待秒数。
func TestFreeTierRejections(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   msg.Code
		params msg.Params
	}{
		{429, `{"error":{"code":"free_rate_limited","metadata":{"reason":"err_free_rate","retry_after_seconds":37}}}`, msg.ErrAIFreeRate, msg.Params{"seconds": 37}},
		{429, `{"error":{"code":"free_rate_limited","metadata":{"reason":"err_free_rate"}}}`, msg.ErrAIFreeBusy, nil},
		{400, `{"error":{"code":"free_rate_limited","type":"invalid_request_error","metadata":{"reason":"err_free_prompt_cap","retryable":false}}}`, msg.ErrAIFreePromptCap, nil},
		{429, `{"error":{"code":"free_rate_limited","metadata":{"reason":"err_free_access_denied","retryable":false}}}`, msg.ErrAIFreeAccess, nil},
		{429, `{"error":{"message":"slow down"}}`, msg.ErrAIRateLimit, nil},
	}
	for _, c := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		err := Ping(context.Background(), server.Client(), Config{Format: FormatChat, BaseURL: server.URL, Key: "k", Model: "orcarouter/free"})
		server.Close()
		var ce *msg.CallError
		if !errors.As(err, &ce) || ce.Code != c.code || (c.params != nil && ce.Params["seconds"] != c.params["seconds"]) {
			t.Errorf("%s：%v", c.body, err)
		}
	}
}

// HTTP 状态映射为界面错误码，错误详情中不出现密钥。
func TestErrorsAreClassifiedAndSanitized(t *testing.T) {
	key := "sk-secret-value-123456"
	for status, code := range map[int]msg.Code{401: msg.ErrAIAuth, 403: msg.ErrAIAuth, 429: msg.ErrAIRateLimit, 400: msg.ErrAIResponse, 500: msg.ErrAIResponse} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key ` + key + ` Bearer ` + key + `"}}`))
		}))
		err := Ping(context.Background(), server.Client(), Config{Format: FormatChat, BaseURL: server.URL, Key: key, Model: "m"})
		server.Close()
		var ce *msg.CallError
		if !errors.As(err, &ce) || ce.Code != code {
			t.Errorf("%d: %v", status, err)
			continue
		}
		if raw, _ := json.Marshal(ce.Params); strings.Contains(string(raw), key) {
			t.Errorf("%d 错误详情泄露密钥：%s", status, raw)
		}
	}
	err := Ping(context.Background(), http.DefaultClient, Config{Format: FormatChat, BaseURL: "http://127.0.0.1:1", Model: "m"})
	var ce *msg.CallError
	if !errors.As(err, &ce) || ce.Code != msg.ErrAINetwork {
		t.Fatal("连接失败应返回 aiNetwork", err)
	}
}

func TestModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[{"id":"b"},{"id":"a"},{"id":"a"},{"name":"c"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()
	c := Config{Format: FormatChat, BaseURL: server.URL, ModelsPath: "/models", Model: "m"}
	models, err := Models(context.Background(), server.Client(), c)
	if err != nil || strings.Join(models, ",") != "a,b,c" {
		t.Fatal(models, err)
	}
	c.ModelsPath = ""
	if _, err = Models(context.Background(), server.Client(), c); err == nil {
		t.Fatal("没有模型列表路径时应报错")
	}
}

func TestMask(t *testing.T) {
	if Mask("sk-abcdefghijklmnop") != "sk-a••••••••mnop" || Mask("short") != "•••••" || Mask("") != "" {
		t.Fatal(Mask("sk-abcdefghijklmnop"))
	}
}
