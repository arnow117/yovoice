package schema

import (
	"time"
	"yovoice/internal/msg"
)

// 助手对话状态：消息列表持久化（面板重开可见），进行中的一轮与界面事件只随状态广播、不入库。

// AssistantMessage 是助手对话中的一条消息：用户消息只有 Text；助手消息含文本与工具卡片；
// Role 为 divider 时是新对话的分隔，正文为空。
type AssistantMessage struct {
	ID          string              `json:"id"`
	Role        string              `json:"role"` // user、assistant 或 divider
	Text        string              `json:"text,omitempty"`
	Tools       []AssistantToolCard `json:"tools,omitempty"`
	Status      string              `json:"status,omitempty"` // 助手消息的结束状态：completed、cancelled、failed
	ErrorCode   msg.Code            `json:"errorCode,omitempty"`
	ErrorParams msg.Params          `json:"errorParams,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
}

// AssistantToolCard 是消息内一次工具调用的摘要；Args 为截断后的参数 JSON，点开卡片时查看。
type AssistantToolCard struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary,omitempty"` // 动作摘要，如“修改了《示例小步舞曲》的乐谱”
	Args    string `json:"args,omitempty"`
	State   string `json:"state,omitempty"` // running、done 或 error
}

// AssistantChat 是持久化的对话记录，SessionID 标识当前段对话，新对话后更换。
type AssistantChat struct {
	SessionID string             `json:"sessionId"`
	Messages  []AssistantMessage `json:"messages"`
}

// AssistantChatLimit 是对话记录的保存上限，超出时丢弃最旧的消息。
const AssistantChatLimit = 200

// AssistantTurn 是正在运行的一轮对话的实时状态：流式文本与工具卡片。
type AssistantTurn struct {
	Text      string              `json:"text,omitempty"`
	Tools     []AssistantToolCard `json:"tools,omitempty"`
	Model     string              `json:"model,omitempty"`
	StartedAt time.Time           `json:"startedAt"`
}

// AssistantNav 是助手要求界面跳转的一次事件，ID 变化即为新事件；WorkID 与 Page 二选一。
type AssistantNav struct {
	ID     string `json:"id"`
	WorkID string `json:"workId,omitempty"`
	Page   string `json:"page,omitempty"`
}

// AssistantUpdate 是助手最近一次保存的作品修改事件，界面据此刷新正打开的编辑器。
type AssistantUpdate struct {
	ID     string `json:"id"`
	WorkID string `json:"workId"`
}

// AssistantLive 是只随状态广播的助手实时信息：进行中的一轮、排队条数与界面事件。
type AssistantLive struct {
	Turn   *AssistantTurn   `json:"turn,omitempty"`
	Queued int              `json:"queued,omitempty"`
	Nav    *AssistantNav    `json:"nav,omitempty"`
	Update *AssistantUpdate `json:"update,omitempty"`
}
