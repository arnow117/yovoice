package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"yovoice/internal/msg"
)

const maxBody = 4 << 20

// Config 是一次调用所需的服务配置。
type Config struct {
	Format     string
	BaseURL    string
	ModelsPath string
	Key        string
	Model      string
}

// Join 拼接服务地址与接口路径：路径为绝对 URL 时直接使用，已是地址后缀时不重复追加。
func Join(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return base
	}
	if u, e := url.Parse(path); e == nil && u.IsAbs() {
		return u.String()
	}
	path = "/" + strings.TrimLeft(path, "/")
	if strings.HasSuffix(base, path) {
		return base
	}
	return base + path
}

func endpoint(c Config) string {
	switch c.Format {
	case FormatResponses:
		return Join(c.BaseURL, "/responses")
	case FormatAnthropic:
		if strings.HasSuffix(strings.TrimRight(c.BaseURL, "/"), "/v1") {
			return Join(c.BaseURL, "/messages")
		}
		return Join(c.BaseURL, "/v1/messages")
	}
	return Join(c.BaseURL, "/chat/completions")
}

func azure(base string) bool {
	u, e := url.Parse(base)
	if e != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, ".openai.azure.com") || strings.HasSuffix(host, ".cognitiveservices.azure.com") || strings.HasSuffix(host, ".services.ai.azure.com")
}

// headers 与 nexus 的 applyProviderHeaders 一致：Bearer 通用，Azure 另加 api-key，Anthropic 协议用 x-api-key 与版本头。
func headers(r *http.Request, c Config) {
	key := strings.TrimSpace(c.Key)
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
		if azure(c.BaseURL) {
			r.Header.Set("api-key", key)
		}
	}
	if c.Format == FormatAnthropic {
		if key != "" {
			r.Header.Set("x-api-key", key)
		}
		r.Header.Set("anthropic-version", "2023-06-01")
	}
	r.Header.Set("Accept", "application/json")
}

// pingPayload 组装连通测试用的一次极短对话；推理模型需要一定输出额度才能给出回复。
func pingPayload(c Config) map[string]any {
	const (
		system    = "Reply with OK."
		user      = "ping"
		maxTokens = 256
	)
	switch c.Format {
	case FormatAnthropic:
		return map[string]any{"model": c.Model, "max_tokens": maxTokens, "system": system,
			"messages": []map[string]string{{"role": "user", "content": user}}}
	case FormatResponses:
		return map[string]any{"model": c.Model, "instructions": system, "max_output_tokens": maxTokens, "store": false,
			"input": []map[string]string{{"role": "user", "content": user}}}
	}
	body := map[string]any{"model": c.Model, "temperature": 0, "stream": false,
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}
	// Azure 与新版 OpenAI 推理模型只接受 max_completion_tokens。
	if azure(c.BaseURL) || strings.Contains(c.BaseURL, "api.openai.com") {
		body["max_completion_tokens"] = maxTokens
	} else {
		body["max_tokens"] = maxTokens
	}
	return body
}

// Ping 用一次极短对话验证地址、密钥与模型可用，供服务连通测试。
func Ping(ctx context.Context, client *http.Client, c Config) error {
	if strings.TrimSpace(c.Model) == "" {
		return msg.Err(msg.ErrAINotConfigured, nil)
	}
	raw, e := json.Marshal(pingPayload(c))
	if e != nil {
		return e
	}
	body, e := send(ctx, client, c, http.MethodPost, endpoint(c), raw)
	if e != nil {
		return e
	}
	return parse(c.Format, body)
}

// parse 检查连通测试的响应结构是否可读。
func parse(format string, body []byte) error {
	switch format {
	case FormatAnthropic:
		var v struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		}
		if e := json.Unmarshal(body, &v); e != nil {
			return e
		}
		return nil
	case FormatResponses:
		var v struct {
			Output []struct {
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"output"`
		}
		if e := json.Unmarshal(body, &v); e != nil {
			return e
		}
		return nil
	}
	var v struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if e := json.Unmarshal(body, &v); e != nil {
		return e
	}
	if len(v.Choices) == 0 {
		return errors.New("no choices")
	}
	return nil
}

// Models 读取服务的模型列表（OpenAI 与 Anthropic 都返回 {data:[{id}]}）。
func Models(ctx context.Context, client *http.Client, c Config) ([]string, error) {
	if strings.TrimSpace(c.ModelsPath) == "" {
		return nil, msg.Err(msg.ErrAIModelsPath, nil)
	}
	body, e := send(ctx, client, c, http.MethodGet, Join(c.BaseURL, c.ModelsPath), nil)
	if e != nil {
		return nil, e
	}
	var v struct {
		Data []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"data"`
		Models []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"models"`
	}
	if e = json.Unmarshal(body, &v); e != nil {
		return nil, msg.Err(msg.ErrAIResponse, msg.Params{"status": 200, "detail": Sanitize(string(body), c.Key)})
	}
	var ids []string
	for _, m := range append(v.Data, v.Models...) {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			id = strings.TrimSpace(m.Model)
		}
		if id == "" {
			id = strings.TrimSpace(m.Name)
		}
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

func send(ctx context.Context, client *http.Client, c Config, method, target string, raw []byte) ([]byte, error) {
	u, e := url.Parse(target)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, msg.Err(msg.ErrAIProviderInvalid, nil)
	}
	var reader io.Reader
	if raw != nil {
		reader = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, target, reader)
	if e != nil {
		return nil, msg.Err(msg.ErrAIProviderInvalid, nil)
	}
	headers(req, c)
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, e := client.Do(req)
	if e != nil {
		if errors.Is(e, context.Canceled) {
			return nil, context.Canceled
		}
		return nil, msg.Err(msg.ErrAINetwork, msg.Params{"detail": Sanitize(networkDetail(e), c.Key)})
	}
	defer res.Body.Close()
	body, e := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if e != nil {
		if errors.Is(e, context.Canceled) {
			return nil, context.Canceled
		}
		return nil, msg.Err(msg.ErrAINetwork, msg.Params{"detail": Sanitize(networkDetail(e), c.Key)})
	}
	if len(body) > maxBody {
		return nil, msg.Err(msg.ErrAIResponse, msg.Params{"status": res.StatusCode, "detail": "response too large"})
	}
	if e := freeTierError(res.StatusCode, body); e != nil {
		return nil, e
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, msg.Err(msg.ErrAIAuth, msg.Params{"status": res.StatusCode})
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, msg.Err(msg.ErrAIRateLimit, nil)
	case res.StatusCode < 200 || res.StatusCode > 299:
		return nil, msg.Err(msg.ErrAIResponse, msg.Params{"status": res.StatusCode, "detail": errorDetail(body, c.Key)})
	}
	return body, nil
}

func networkDetail(e error) string {
	var ne net.Error
	if errors.As(e, &ne) && ne.Timeout() {
		return "timeout"
	}
	var ue *url.Error
	if errors.As(e, &ue) {
		return ue.Err.Error()
	}
	return e.Error()
}

// errorDetail 优先取服务返回的 error.message，否则截取正文。
// freeTierError 识别 OrcaRouter 免费模型的拒绝（error.code 为 free_rate_limited），按 metadata.reason 区分：
// 窗口已满（附等待秒数）、免费通道繁忙、单次提示超长（重试无用）与账号未满足免费条件。其他响应返回 nil。
func freeTierError(status int, body []byte) error {
	if status != http.StatusTooManyRequests && status != http.StatusBadRequest {
		return nil
	}
	var v struct {
		Error struct {
			Code     string `json:"code"`
			Metadata struct {
				Reason     string  `json:"reason"`
				RetryAfter float64 `json:"retry_after_seconds"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &v) != nil || v.Error.Code != "free_rate_limited" {
		return nil
	}
	switch v.Error.Metadata.Reason {
	case "err_free_prompt_cap":
		return msg.Err(msg.ErrAIFreePromptCap, nil)
	case "err_free_access_denied":
		return msg.Err(msg.ErrAIFreeAccess, nil)
	}
	if seconds := int(v.Error.Metadata.RetryAfter + 0.5); seconds > 0 {
		return msg.Err(msg.ErrAIFreeRate, msg.Params{"seconds": seconds})
	}
	return msg.Err(msg.ErrAIFreeBusy, nil)
}

func errorDetail(body []byte, key string) string {
	var v struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &v) == nil && len(v.Error) > 0 {
		var e struct {
			Message string `json:"message"`
		}
		var s string
		if json.Unmarshal(v.Error, &e) == nil && e.Message != "" {
			return Sanitize(e.Message, key)
		}
		if json.Unmarshal(v.Error, &s) == nil && s != "" {
			return Sanitize(s, key)
		}
	}
	return Sanitize(string(body), key)
}

var bearer = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._\-]+`)

// Sanitize 去掉错误信息中的密钥与鉴权头，并截断到 300 字符。
func Sanitize(text string, secrets ...string) string {
	text = bearer.ReplaceAllString(text, "${1}<redacted>")
	for _, s := range secrets {
		if s = strings.TrimSpace(s); s != "" {
			text = strings.ReplaceAll(text, s, "<redacted>")
		}
	}
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > 300 {
		text = string(r[:300]) + "…"
	}
	return text
}

// Mask 只保留密钥首尾，用于界面展示。
func Mask(key string) string {
	r := []rune(strings.TrimSpace(key))
	if len(r) == 0 {
		return ""
	}
	if len(r) <= 10 {
		return strings.Repeat("•", len(r))
	}
	return fmt.Sprintf("%s••••••••%s", string(r[:4]), string(r[len(r)-4:]))
}
