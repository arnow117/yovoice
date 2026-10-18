package workbench

import (
	"context"
	"errors"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"yovoice/internal/assistant"
	"yovoice/internal/diag"
	"yovoice/internal/download"
	"yovoice/internal/llm"
	"yovoice/internal/msg"
	"yovoice/internal/platform"
	"yovoice/internal/schema"
)

// AI 服务为可选功能：配置写入状态（不含密钥），密钥存于状态库的私有设置，不随状态广播；
// 调用不占用下载、推理所用的后台操作，同一时间只运行一个 AI 请求，可单独取消。

// AIProviderInput 是界面提交的服务配置；Key 为 nil 时保留原密钥，空串表示清除。
type AIProviderInput struct {
	ID         string  `json:"id"`
	Preset     string  `json:"preset"`
	Name       string  `json:"name"`
	Format     string  `json:"format"`
	BaseURL    string  `json:"baseURL"`
	ModelsPath string  `json:"modelsPath"`
	Model      string  `json:"model"`
	Key        *string `json:"key"`
}

type aiState struct {
	mu sync.Mutex
	// connect 是进行中的 OrcaRouter 一键授权。
	connect *connectSession
	secrets sync.Mutex
}

// aiKeysPrivate 是状态库私有设置中保存 AI 服务密钥的键，值为服务 ID 到密钥的映射。
const aiKeysPrivate = "aiKeys"

func (w *Workbench) readSecrets() (map[string]string, error) {
	keys := map[string]string{}
	if _, e := w.Store.Private(aiKeysPrivate, &keys); e != nil {
		return nil, e
	}
	return keys, nil
}

// writeSecret 保存或删除（value 为空）一个服务的密钥。
func (w *Workbench) writeSecret(id, value string) error {
	w.ai.secrets.Lock()
	defer w.ai.secrets.Unlock()
	keys, e := w.readSecrets()
	if e != nil {
		return e
	}
	if value == "" {
		delete(keys, id)
	} else {
		keys[id] = value
	}
	return w.Store.SetPrivate(aiKeysPrivate, keys)
}

func validEndpoint(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// SaveAIProvider 新建或更新服务。固定地址的预设忽略界面传来的地址与协议，本地服务只允许改地址。
func (w *Workbench) SaveAIProvider(in AIProviderInput) (schema.AIProvider, error) {
	w.edit.Lock()
	defer w.edit.Unlock()
	bad := msg.Err(msg.ErrAIProviderInvalid, nil)
	preset, ok := llm.LookupPreset(in.Preset)
	if !ok {
		return schema.AIProvider{}, bad
	}
	p := schema.AIProvider{ID: in.ID, Preset: preset.Key, Name: strings.TrimSpace(in.Name), Format: preset.Format, BaseURL: preset.BaseURL, ModelsPath: preset.ModelsPath, Model: strings.TrimSpace(in.Model)}
	switch preset.Endpoint {
	case "local":
		p.BaseURL = strings.TrimSpace(in.BaseURL)
	case "custom":
		p.BaseURL, p.ModelsPath, p.Format = strings.TrimSpace(in.BaseURL), strings.TrimSpace(in.ModelsPath), in.Format
	}
	if p.Name == "" {
		p.Name = preset.Name
	}
	if !slices.Contains(llm.Formats, p.Format) || !validEndpoint(p.BaseURL) || schema.TextLen(p.Name) > 60 || len(p.Model) > 200 || len(p.ModelsPath) > 500 {
		return schema.AIProvider{}, bad
	}
	if in.Key != nil && len(*in.Key) > 4096 {
		return schema.AIProvider{}, bad
	}
	s := w.Store.Read()
	index := slices.IndexFunc(s.AIProviders, func(x schema.AIProvider) bool { return x.ID == p.ID })
	if p.ID == "" {
		if len(s.AIProviders) >= schema.AIProviderLimit {
			return schema.AIProvider{}, bad
		}
		p.ID = schema.NewID()
	} else if index < 0 {
		return schema.AIProvider{}, bad
	} else {
		old := s.AIProviders[index]
		p.Models, p.KeyMask, p.LastTest = old.Models, old.KeyMask, old.LastTest
		// 地址或协议变了，之前的模型列表和测试结果不再可信。
		if old.BaseURL != p.BaseURL || old.Format != p.Format {
			p.Models, p.LastTest = nil, nil
		}
	}
	if in.Key != nil {
		key := strings.TrimSpace(*in.Key)
		if e := w.writeSecret(p.ID, key); e != nil {
			return schema.AIProvider{}, e
		}
		p.KeyMask, p.LastTest = llm.Mask(key), nil
	}
	e := w.Store.Update(func(s *schema.State) {
		if i := slices.IndexFunc(s.AIProviders, func(x schema.AIProvider) bool { return x.ID == p.ID }); i >= 0 {
			s.AIProviders[i] = p
		} else {
			s.AIProviders = append(s.AIProviders, p)
		}
		if s.AIProviderID == "" {
			s.AIProviderID = p.ID
		}
	}, true)
	if e == nil {
		diag.Log(w.Store.Root, "ai.provider_saved", "provider_id", p.ID, "preset", p.Preset, "format", p.Format)
	}
	return p, e
}

func (w *Workbench) DeleteAIProvider(id string) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	if !slices.ContainsFunc(w.Store.Read().AIProviders, func(x schema.AIProvider) bool { return x.ID == id }) {
		return msg.Err(msg.ErrAIProviderInvalid, nil)
	}
	if e := w.writeSecret(id, ""); e != nil {
		return e
	}
	return w.Store.Update(func(s *schema.State) {
		s.AIProviders = slices.DeleteFunc(s.AIProviders, func(x schema.AIProvider) bool { return x.ID == id })
		if s.AIProviderID == id {
			s.AIProviderID = ""
			if len(s.AIProviders) > 0 {
				s.AIProviderID = s.AIProviders[0].ID
			}
		}
	}, true)
}

// UseAIProvider 设定 AI 功能使用的服务与模型：写作栏可在已配置的服务和模型间切换；
// model 为空时沿用该服务原来的模型，id 为空表示停用 AI 功能。
func (w *Workbench) UseAIProvider(id, model string) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	model = strings.TrimSpace(model)
	if id != "" && !slices.ContainsFunc(w.Store.Read().AIProviders, func(x schema.AIProvider) bool { return x.ID == id }) || len(model) > 200 {
		return msg.Err(msg.ErrAIProviderInvalid, nil)
	}
	return w.Store.Update(func(s *schema.State) {
		s.AIProviderID = id
		if i := slices.IndexFunc(s.AIProviders, func(x schema.AIProvider) bool { return x.ID == id }); i >= 0 && model != "" {
			s.AIProviders[i].Model = model
		}
	}, true)
}

// aiConfig 读取服务配置与密钥；id 为空时使用当前服务。
func (w *Workbench) aiConfig(id string) (schema.AIProvider, llm.Config, error) {
	s := w.Store.Read()
	if id == "" {
		id = s.AIProviderID
	}
	i := slices.IndexFunc(s.AIProviders, func(x schema.AIProvider) bool { return x.ID == id })
	if i < 0 {
		return schema.AIProvider{}, llm.Config{}, msg.Err(msg.ErrAINotConfigured, nil)
	}
	p := s.AIProviders[i]
	w.ai.secrets.Lock()
	keys, e := w.readSecrets()
	w.ai.secrets.Unlock()
	if e != nil {
		return p, llm.Config{}, e
	}
	return p, llm.Config{Format: p.Format, BaseURL: p.BaseURL, ModelsPath: p.ModelsPath, Key: keys[p.ID], Model: p.Model}, nil
}

// updateAIProvider 在网络请求结束后回写结果；服务已被删除时忽略。
func (w *Workbench) updateAIProvider(id string, change func(*schema.AIProvider)) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	return w.Store.Update(func(s *schema.State) {
		if i := slices.IndexFunc(s.AIProviders, func(x schema.AIProvider) bool { return x.ID == id }); i >= 0 {
			change(&s.AIProviders[i])
		}
	}, true)
}

// AIModels 读取服务的模型列表并保存；尚未选择模型时选中第一个。
func (w *Workbench) AIModels(ctx context.Context, id string) ([]string, error) {
	p, c, e := w.aiConfig(id)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	models, e := llm.Models(ctx, w.client, c)
	if e != nil {
		return nil, e
	}
	models = models[:min(len(models), 500)]
	return models, w.updateAIProvider(p.ID, func(p *schema.AIProvider) {
		p.Models = models
		if p.Model == "" && len(models) > 0 {
			p.Model = models[0]
		}
	})
}

// TestAIProvider 用一次简短对话验证地址、密钥和模型，结果记录在服务上。
func (w *Workbench) TestAIProvider(ctx context.Context, id string) (schema.AITest, error) {
	p, c, e := w.aiConfig(id)
	if e != nil {
		return schema.AITest{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	e = llm.Ping(ctx, w.client, c)
	if errors.Is(e, context.Canceled) {
		return schema.AITest{}, e
	}
	result := schema.AITest{OK: e == nil, At: time.Now().UTC()}
	var ce *msg.CallError
	if errors.As(e, &ce) {
		result.Code, result.Params = ce.Code, ce.Params
	} else if e != nil {
		result.Code, result.Params = msg.ErrAIResponse, msg.Params{"status": 0, "detail": llm.Sanitize(e.Error(), c.Key)}
	}
	diag.Log(w.Store.Root, "ai.provider_tested", "provider_id", p.ID, "ok", result.OK, "code", result.Code)
	return result, w.updateAIProvider(p.ID, func(p *schema.AIProvider) { p.LastTest = &result })
}

// aiTimeout 返回单次 AI 请求的超时，未设置时用默认值。
func aiTimeout(p schema.Preferences) time.Duration {
	seconds := p.AITimeout
	if seconds < schema.AITimeoutMin || seconds > schema.AITimeoutMax {
		seconds = schema.AITimeoutDefault
	}
	return time.Duration(seconds) * time.Second
}

// providerEnv 把服务配置翻译成 ocho 的模型服务环境变量；密钥只经环境变量传给子进程。
func providerEnv(p schema.AIProvider, c llm.Config) []string {
	var env []string
	set := func(name, value string) { env = append(env, name+"="+value) }
	switch p.Format {
	case llm.FormatAnthropic:
		set("ANTHROPIC_BASE_URL", c.BaseURL)
		if c.Key != "" {
			set("ANTHROPIC_API_KEY", c.Key)
		}
	case llm.FormatResponses:
		set("OCHO_API_PROVIDER", "responses")
		set("OPENAI_BASE_URL", c.BaseURL)
		if c.Key != "" {
			set("OPENAI_API_KEY", c.Key)
		}
	default:
		set("OCHO_API_PROVIDER", "openai")
		set("OPENAI_BASE_URL", c.BaseURL)
		if c.Key != "" {
			set("OPENAI_API_KEY", c.Key)
		}
	}
	return env
}

// proxyEnvNames 是透传给助手子进程的代理环境变量名；小写形式一并透传，匹配常见代理客户端的约定。
var proxyEnvNames = []string{
	"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy",
}

// proxyEnv 按偏好设置生成助手子进程的代理环境变量：启用时指向配置的地址（本地地址仍直连，
// 与下载代理一致），显式关闭时全部不设，未配置时从宿主环境透传，方便依赖系统代理的用户。
func proxyEnv(p schema.Preferences) []string {
	if p.ProxyEnabled != nil && !*p.ProxyEnabled {
		return nil
	}
	address := strings.TrimSpace(p.ProxyURL)
	if _, err := download.ParseProxy(address); err != nil {
		return nil
	}
	if address == "" {
		var env []string
		for _, name := range proxyEnvNames {
			if value := os.Getenv(name); value != "" {
				env = append(env, name+"="+value)
			}
		}
		return env
	}
	// NO_PROXY 兜住本机服务（如 Ollama）：代理只应接管远程模型请求。
	return []string{"HTTPS_PROXY=" + address, "HTTP_PROXY=" + address, "ALL_PROXY=" + address,
		"NO_PROXY=localhost,127.0.0.1,::1", "no_proxy=localhost,127.0.0.1,::1"}
}

// assistantConfig 由当前服务配置组装某个会话的助手子进程配置；ocho 缺失与服务未配置分别报错。
// 每个会话有独立的 OCHO_CONFIG_DIR（切换会话即重启子进程），长期记忆目录所有会话共享。
func (w *Workbench) assistantConfig(sessionID string) (assistant.Config, error) {
	executable, err := platform.OchoExecutable()
	if err != nil {
		return assistant.Config{}, msg.Err(msg.ErrAssistantMissing, nil)
	}
	p, c, err := w.aiConfig("")
	if err != nil {
		return assistant.Config{}, err
	}
	if c.Model == "" {
		return assistant.Config{}, msg.Err(msg.ErrAINotConfigured, nil)
	}
	// 会话目录预先建好：ocho 首次启动要往里写自己的配置。
	if err = os.MkdirAll(assistantSessionDir(w.Store.Root, sessionID), 0700); err != nil {
		return assistant.Config{}, err
	}
	env := append(providerEnv(p, c), proxyEnv(w.Store.Read().Preferences)...)
	env = append(env, "OCHO_CONFIG_DIR="+assistantSessionDir(w.Store.Root, sessionID), "OCHO_MEMORY_DIR="+assistantMemoryDir(w.Store.Root))
	return assistant.Config{Executable: executable, Model: c.Model, Env: env}, nil
}

// stopAI 取消进行中的一键授权，服务关闭时调用。
func (w *Workbench) stopAI() {
	w.ai.mu.Lock()
	connect := w.ai.connect
	w.ai.mu.Unlock()
	if connect != nil {
		w.closeConnect(connect)
	}
}
