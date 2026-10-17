package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// assistantTools 是助手进程的工具白名单：只开放任务上下文与提交工具，
// 加上供了解环境用的 status/list_models，不开放任何内置文件、命令或网络工具。
var assistantTools = []string{"get_draft", "submit_score", "submit_lyrics", "list_models", "status"}

// newAssistantServer 构建只给内置助手的 MCP server：status/list_models 与对外 MCP 共用同一份定义，
// get_draft/submit_* 只在这里注册，不暴露给对外 MCP。
func (w *Workbench) newAssistantServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "yovoice", Version: "1.0.0"}, nil)
	w.registerStatusTool(server)
	w.registerListModelsTool(server)
	mcp.AddTool(server, &mcp.Tool{Name: "get_draft", Description: "读取当前任务的上下文：任务类型与需求、目标时长、语言、编辑器里的当前草稿（继续修改时）、可用音色库与演唱语言。每个任务开始时先调用。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		draft := w.ai.draft.Load()
		if draft == nil {
			return nil, nil, fmt.Errorf("当前没有进行中的任务")
		}
		out := draft.context()
		out["soundFonts"] = w.installedSoundFonts()
		return nil, out, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "submit_score", Description: "提交完整乐谱。校验失败会返回具体错误，按错误修改后重新提交；任务必须以一次成功的提交结束。"}, w.submitScoreTool)
	mcp.AddTool(server, &mcp.Tool{Name: "submit_lyrics", Description: "提交标题、风格标签、歌词与演唱语言。style 必填；纯音乐时歌词只允许段落标记。校验失败会返回具体错误，按错误修改后重新提交。"}, w.submitLyricsTool)
	return server
}

// installedSoundFonts 列出已安装、可供编曲渲染的音色库。
func (w *Workbench) installedSoundFonts() []string {
	var fonts []string
	for _, m := range w.Store.Read().Models {
		if definition, err := catalog.Lookup(m.ID); err == nil && definition.Family == "soundfont" {
			fonts = append(fonts, m.ID)
		}
	}
	return fonts
}

// aiDraft 是当前助手任务的上下文：get_draft 读取它，submit_* 把结果写进它。
type aiDraft struct {
	mu              sync.Mutex
	kind            string // score 或 lyrics
	taskID          string
	projectID       string
	brief           string
	followUp        string
	seconds         int
	instrumental    bool
	musicLanguage   string
	locale          schema.UiLocale
	scoreDraft      *schema.Score
	lyricsDraft     *Lyrics
	submittedScore  *ComposedScore
	submittedLyrics *Lyrics
}

// context 返回 get_draft 的结构化内容（音色库列表由调用方补充）。
func (d *aiDraft) context() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	task := map[string]any{"id": d.taskID, "kind": d.kind, "projectId": d.projectID, "brief": d.brief}
	if d.followUp != "" {
		task["followUp"] = d.followUp
	}
	out := map[string]any{"task": task, "instrumental": d.instrumental, "uiLocale": d.locale, "musicLanguages": schema.MusicLanguages}
	if d.kind == "score" {
		out["seconds"] = d.seconds
		if d.scoreDraft != nil {
			out["scoreDraft"] = *d.scoreDraft
		}
	} else {
		if d.musicLanguage != "" {
			out["language"] = d.musicLanguage
		}
		if d.lyricsDraft != nil {
			out["lyricsDraft"] = *d.lyricsDraft
		}
	}
	return out
}

func (d *aiDraft) score() *ComposedScore {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.submittedScore
}

func (d *aiDraft) lyrics() *Lyrics {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.submittedLyrics
}

// submitScoreInput 与 ComposedScore 的 JSON 结构一致；notes 同时接受紧凑数组与完整对象。
type submitScoreInput struct {
	Title         string                `json:"title" jsonschema:"曲名，简短，用界面语言书写"`
	Tempo         float64               `json:"tempo" jsonschema:"速度 30-300（BPM）"`
	TimeSignature []int                 `json:"timeSignature" jsonschema:"拍号 [分子,分母]，如 [4,4]"`
	Key           string                `json:"key,omitempty" jsonschema:"调性，如 D minor"`
	Sections      []schema.ScoreSection `json:"sections,omitempty" jsonschema:"段落，可带自己的 tempo 做渐慢"`
	Tracks        []submitScoreTrack    `json:"tracks"`
}

// submitScoreTrack 是提交乐谱里的一个声部。
type submitScoreTrack struct {
	ID       string             `json:"id" jsonschema:"声部唯一 ID"`
	Name     string             `json:"name" jsonschema:"声部名称，用界面语言书写"`
	Role     string             `json:"role,omitempty" jsonschema:"melody/piano/strings/bass/drums/pad/arp/other"`
	Program  int                `json:"program" jsonschema:"General MIDI 音色号 0-127"`
	Drums    bool               `json:"drums,omitempty" jsonschema:"鼓组时为 true"`
	Level    float64            `json:"level,omitempty" jsonschema:"电平 dBFS，一般省略"`
	Pan      float64            `json:"pan,omitempty" jsonschema:"-1 到 1"`
	Reverb   float64            `json:"reverb,omitempty" jsonschema:"0 到 1"`
	Humanize *schema.Humanize   `json:"humanize,omitempty" jsonschema:"力度与时间微调"`
	Dynamics []schema.ScoreRamp `json:"dynamics,omitempty" jsonschema:"音量渐变"`
	Notes    []any              `json:"notes" jsonschema:"音符数组，每项为 [小节,拍,MIDI音高,拍数,力度] 五个数字或同字段对象；小节与拍从 1 开始，拍可为小数"`
	Repeats  []scoreRepeat      `json:"repeats,omitempty" jsonschema:"把 from..to 小节的音符复制到各 at 起点"`
}

// submitScoreTool 校验并保存提交的乐谱，写进当前任务的结果。
func (w *Workbench) submitScoreTool(ctx context.Context, req *mcp.CallToolRequest, in submitScoreInput) (*mcp.CallToolResult, any, error) {
	draft := w.ai.draft.Load()
	if draft == nil || draft.kind != "score" {
		return nil, nil, fmt.Errorf("当前没有进行中的写谱任务")
	}
	score, err := buildScore(in)
	if err != nil {
		return nil, nil, err
	}
	draft.mu.Lock()
	draft.submittedScore = &score
	draft.mu.Unlock()
	notes := 0
	for _, t := range score.Tracks {
		notes += len(t.Notes)
	}
	return nil, map[string]any{"title": score.Title, "tracks": len(score.Tracks), "notes": notes}, nil
}

// buildScore 把工具参数转成 ComposedScore 并做与界面一致的校验。
func buildScore(in submitScoreInput) (ComposedScore, error) {
	s := schema.Score{Tempo: in.Tempo, TimeSignature: in.TimeSignature, Key: strings.TrimSpace(in.Key), Sections: in.Sections}
	notes := 0
	for _, t := range in.Tracks {
		track := schema.ScoreTrack{ID: t.ID, Name: t.Name, Role: t.Role, Program: t.Program, Drums: t.Drums, Level: t.Level, Pan: t.Pan, Reverb: t.Reverb, Humanize: t.Humanize, Dynamics: t.Dynamics}
		for _, raw := range t.Notes {
			note, err := parseScoreNote(raw)
			if err != nil {
				return ComposedScore{}, fmt.Errorf("track %q: %v", track.ID, err)
			}
			track.Notes = append(track.Notes, note)
		}
		if err := expandRepeats(&track, t.Repeats, schema.ScoreMaxNotes-notes); err != nil {
			return ComposedScore{}, err
		}
		notes += len(track.Notes)
		s.Tracks = append(s.Tracks, track)
	}
	if issue := schema.ScoreIssue(&s); issue != "" {
		return ComposedScore{}, fmt.Errorf("%s", issue)
	}
	return ComposedScore{Score: s, Title: truncateRunes(strings.TrimSpace(in.Title), 120)}, nil
}

// parseScoreNote 接受紧凑数组 [bar, beat, pitch, length, velocity] 与完整对象两种音符写法。
func parseScoreNote(raw any) (schema.ScoreNote, error) {
	switch v := raw.(type) {
	case []any:
		if len(v) != 5 {
			return schema.ScoreNote{}, fmt.Errorf("each note array needs exactly 5 numbers [bar, beat, pitch, length, velocity]")
		}
		values := make([]float64, 5)
		for i, item := range v {
			f, ok := item.(float64)
			if !ok {
				return schema.ScoreNote{}, fmt.Errorf("note array values must be numbers")
			}
			values[i] = f
		}
		return schema.ScoreNote{Bar: int(values[0]), Beat: values[1], Pitch: int(values[2]), Length: values[3], Velocity: int(values[4])}, nil
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return schema.ScoreNote{}, err
		}
		var note schema.ScoreNote
		if err = json.Unmarshal(data, &note); err != nil {
			return schema.ScoreNote{}, fmt.Errorf("invalid note %s", data)
		}
		return note, nil
	}
	return schema.ScoreNote{}, fmt.Errorf("invalid note %v", raw)
}

// submitLyricsTool 校验并保存提交的歌词，写进当前任务的结果。
func (w *Workbench) submitLyricsTool(ctx context.Context, req *mcp.CallToolRequest, in struct {
	Title    string `json:"title" jsonschema:"标题，用演唱语言书写"`
	Style    string `json:"style" jsonschema:"一行英文逗号分隔的风格标签，必填"`
	Lyrics   string `json:"lyrics" jsonschema:"歌词，段落标记独占一行；纯音乐时只允许段落标记"`
	Language string `json:"language" jsonschema:"演唱语言的 ISO 代码，如 zh、en"`
}) (*mcp.CallToolResult, any, error) {
	draft := w.ai.draft.Load()
	if draft == nil || draft.kind != "lyrics" {
		return nil, nil, fmt.Errorf("当前没有进行中的写词任务")
	}
	if strings.TrimSpace(in.Style) == "" {
		return nil, nil, fmt.Errorf("style is required: one line of comma-separated English tags")
	}
	l := Lyrics{
		Title:    truncateRunes(strings.TrimSpace(in.Title), 120),
		Style:    truncateRunes(strings.TrimSpace(in.Style), 512),
		Lyrics:   truncateRunes(strings.TrimSpace(in.Lyrics), 4000),
		Language: strings.TrimSpace(in.Language),
	}
	if !slices.Contains(schema.MusicLanguages, l.Language) {
		l.Language = ""
	}
	// 纯音乐没有唱词，歌词只允许段落标记描述结构。
	draft.mu.Lock()
	instrumental := draft.instrumental
	draft.mu.Unlock()
	if instrumental {
		for _, line := range strings.Split(l.Lyrics, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
				return nil, nil, fmt.Errorf("instrumental lyrics may contain section markers only (lines like [verse]), found %q", line)
			}
		}
	}
	draft.mu.Lock()
	draft.submittedLyrics = &l
	draft.mu.Unlock()
	return nil, l, nil
}
