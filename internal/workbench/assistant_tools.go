package workbench

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/diag"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 内置助手的工具集：只开放给助手进程，不暴露给对外 MCP。
// 作品工具（list/get/create/update/generate/open）立即反映在界面上；
// 不提供删除、下载模型、改设置与导入导出（第一版不做破坏性/高成本操作）。
var assistantTools = []string{
	"list_works", "get_work", "create_work", "update_work", "generate_work", "get_generation_status",
	"open_work", "open_page", "list_models", "list_voices", "status",
}

// newAssistantServer 构建只给内置助手的 MCP server：status/list_models/list_voices 与对外 MCP 共用定义，
// 作品工具只在这里注册。
func (w *Workbench) newAssistantServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "yovoice", Version: "1.0.0"}, nil)
	w.registerStatusTool(server)
	w.registerListModelsTool(server)
	w.registerListVoicesTool(server)
	mcp.AddTool(server, &mcp.Tool{Name: "list_works", Description: "列出作品（id、类型、标题、更新时间），可按类型过滤。类型：text（语音）、story（故事配音）、music（音乐）、score（编曲）。"}, w.listWorksTool)
	mcp.AddTool(server, &mcp.Tool{Name: "get_work", Description: "读取一个作品的完整可编辑内容：台词或风格、歌词、角色与音色分配、模型与参数、乐谱等。修改作品前先调用。"}, w.getWorkTool)
	mcp.AddTool(server, &mcp.Tool{Name: "create_work", Description: "按类型新建作品并返回 id，界面随即打开它。新作品为空内容，随后用 update_work 填写。"}, w.createWorkTool)
	mcp.AddTool(server, &mcp.Tool{Name: "update_work", Description: "按字段补丁修改作品：标题、台词/文本、歌词与纯音乐、演唱语言、模型与参数、故事配音的角色台词与音色分配、乐谱（与写谱同一套校验）。返回修改后的摘要。"}, w.updateWorkTool)
	mcp.AddTool(server, &mcp.Tool{Name: "generate_work", Description: "对一个作品发起生成（语音、音乐或编曲渲染），立即返回；生成前先用 list_models 确认模型已安装。进度与结果用 get_generation_status 查询。"}, w.generateWorkTool)
	mcp.AddTool(server, &mcp.Tool{Name: "get_generation_status", Description: "查询作品当前的生成进度与最近一次生成的结果（时长与是否完成）。"}, w.getGenerationStatusTool)
	mcp.AddTool(server, &mcp.Tool{Name: "open_work", Description: "让界面打开某个作品。"}, w.openWorkTool)
	mcp.AddTool(server, &mcp.Tool{Name: "open_page", Description: "让界面切换页面：text（语音生成）、story（故事配音）、music（音乐生成）、score（编曲）、characters（声音库）、settings（设置）。"}, w.openPageTool)
	return server
}

// workKindOf 把作品的原始 Kind 归并为界面使用的四种类型。
func workKindOf(d schema.Draft) string {
	switch d.Kind {
	case "music", "score":
		return d.Kind
	case "story", "subtitle":
		return "story"
	}
	if d.Subtitles != nil {
		return "story"
	}
	return "text"
}

// assistantPages 是 open_page 接受的页面。
var assistantPages = []string{"text", "story", "music", "score", "characters", "voices", "history", "settings"}

func (w *Workbench) listWorksTool(ctx context.Context, req *mcp.CallToolRequest, in struct {
	Kind string `json:"kind,omitempty" jsonschema:"只列某类作品：text、story、music 或 score；留空列全部"`
}) (*mcp.CallToolResult, any, error) {
	drafts := w.Store.Read().Drafts
	works := make([]map[string]any, 0, len(drafts))
	for _, d := range drafts {
		kind := workKindOf(d)
		if in.Kind != "" && in.Kind != kind {
			continue
		}
		works = append(works, map[string]any{"id": d.ID, "kind": kind, "title": d.Title, "updatedAt": d.UpdatedAt})
	}
	w.chatCard("list_works", fmt.Sprintf("查看了作品列表（%d 个）", len(works)), fmt.Sprintf("Viewed the works list (%d)", len(works)), "done")
	return nil, map[string]any{"works": works}, nil
}

// workContent 把作品转成给模型看的内容，去掉时间线等对编辑无用的大字段。
func workContent(d schema.Draft) map[string]any {
	out := map[string]any{"id": d.ID, "kind": workKindOf(d), "title": d.Title, "text": d.Text, "updatedAt": d.UpdatedAt}
	settings := map[string]any{"modelId": d.ModelID, "mode": d.Mode, "language": d.Language, "speed": d.Speed}
	for name, value := range map[string]any{"voiceId": d.VoiceID, "emotionVoiceId": d.EmotionVoiceID, "speaker": d.Speaker, "voiceMode": d.VoiceMode, "voiceDescription": d.VoiceDescription, "referenceText": d.ReferenceText, "lyrics": d.Lyrics, "instrumental": d.Instrumental, "synthesisLanguage": d.SynthesisLanguage, "seed": d.Seed, "temperature": d.Temperature} {
		if value != nil && value != "" && value != false {
			settings[name] = value
		}
	}
	out["settings"] = settings
	if d.Subtitles != nil {
		speakers := make([]map[string]any, 0, len(d.Subtitles.Speakers))
		for _, s := range d.Subtitles.Speakers {
			speaker := map[string]any{"id": s.ID, "name": s.SourceName}
			if s.CharacterID != "" {
				speaker["characterId"] = s.CharacterID
			}
			if s.Settings != nil {
				if s.Settings.VoiceID != nil && *s.Settings.VoiceID != "" {
					speaker["voiceId"] = *s.Settings.VoiceID
				}
				if s.Settings.ModelID != "" && s.Settings.ModelID != d.ModelID {
					speaker["modelId"] = s.Settings.ModelID
				}
			}
			speakers = append(speakers, speaker)
		}
		cues := make([]map[string]any, 0, len(d.Subtitles.Cues))
		for _, c := range d.Subtitles.Cues {
			cues = append(cues, map[string]any{"id": c.ID, "speakerId": c.SpeakerID, "text": c.Text})
		}
		out["speakers"] = speakers
		out["cues"] = cues
	}
	if d.Score != nil {
		out["score"] = d.Score
	}
	return out
}

// workIDInput 是以作品 ID 为参数的工具输入。
type workIDInput struct {
	ID string `json:"id" jsonschema:"作品 ID"`
}

func (w *Workbench) getWorkTool(ctx context.Context, req *mcp.CallToolRequest, in workIDInput) (*mcp.CallToolResult, any, error) {
	d, err := w.assistantWork(in.ID)
	if err != nil {
		return nil, nil, err
	}
	w.chatCard("get_work", fmt.Sprintf("读取了《%s》", d.Title), fmt.Sprintf("Read %q", d.Title), "done")
	return nil, workContent(d), nil
}

// assistantWork 按 ID 找到作品；找不到时给出与界面一致的错误。
func (w *Workbench) assistantWork(id string) (schema.Draft, error) {
	for _, d := range w.Store.Read().Drafts {
		if d.ID == id {
			return d, nil
		}
	}
	return schema.Draft{}, fmt.Errorf("没有找到作品 %s，请先用 list_works 查看现有的作品 ID", id)
}

// createWorkInput 的类型与 createDraft 的界面默认一致：语音用默认语音模型，音乐用 ACE-Step，编曲用音色库。
type createWorkInput struct {
	Kind  string `json:"kind" jsonschema:"作品类型：text、story、music 或 score"`
	Title string `json:"title,omitempty" jsonschema:"作品标题，留空由用户稍后命名"`
}

func (w *Workbench) createWorkTool(ctx context.Context, req *mcp.CallToolRequest, in createWorkInput) (*mcp.CallToolResult, any, error) {
	if !slices.Contains([]string{"text", "story", "music", "score"}, in.Kind) {
		return nil, nil, fmt.Errorf("kind 必须是 text、story、music 或 score")
	}
	d := schema.DefaultDraft()
	d.Text, d.Kind, d.Title = "", in.Kind, strings.TrimSpace(in.Title)
	if d.Title == "" {
		d.Title = "未命名作品"
	}
	s := w.Store.Read()
	switch in.Kind {
	case "music":
		d.ModelID = "ace-step-1.5-turbo-bf16"
	case "score":
		d.ModelID = "musescore-general-sf2"
		d.Score = &schema.Score{Tempo: 100, TimeSignature: []int{4, 4}}
	default:
		// 语音与故事作品默认选已安装的语音模型，避免生成时才发现缺模型；
		// 故事作品的角色台词由 update_work 的 subtitles 写入。
		for _, m := range s.Models {
			if !strings.HasPrefix(m.ID, "ace-step-") && !strings.HasPrefix(m.ID, "yue2-") && !strings.HasSuffix(m.ID, "-sf2") {
				d.ModelID = m.ID
				break
			}
		}
	}
	w.edit.Lock()
	err := w.saveDraft(d)
	w.edit.Unlock()
	if err != nil {
		return nil, nil, err
	}
	w.chatCard("create_work", fmt.Sprintf("新建了《%s》", d.Title), fmt.Sprintf("Created %q", d.Title), "done")
	w.chatUpdate(d.ID)
	w.chatNav(d.ID, "")
	diag.Log(w.Store.Root, "assistant.work_created", "project_id", d.ID, "kind", in.Kind)
	return nil, map[string]any{"id": d.ID, "kind": in.Kind, "title": d.Title}, nil
}

// updateWorkInput 是作品补丁：只改给出的字段，全部经 SaveDraft 的校验落盘。
type updateWorkInput struct {
	ID            string             `json:"id" jsonschema:"作品 ID"`
	Title         *string            `json:"title,omitempty" jsonschema:"标题"`
	Text          *string            `json:"text,omitempty" jsonschema:"台词或正文（语音/故事）；音乐作品是风格描述"`
	Lyrics        *string            `json:"lyrics,omitempty" jsonschema:"歌词，段落标记独占一行；纯音乐时只允许段落标记"`
	Instrumental  *bool              `json:"instrumental,omitempty" jsonschema:"纯音乐（无演唱）"`
	Language      *string            `json:"language,omitempty" jsonschema:"音乐作品的演唱语言 ISO 代码，如 zh、en"`
	ModelID       *string            `json:"modelId,omitempty" jsonschema:"模型 ID，先用 list_models 确认已安装"`
	VoiceID       *string            `json:"voiceId,omitempty" jsonschema:"参考音色 ID，先用 list_voices 查询；传空字符串表示清除"`
	Speed         *float64           `json:"speed,omitempty" jsonschema:"语速 0.5-2"`
	Seed          *int               `json:"seed,omitempty" jsonschema:"随机种子"`
	Subtitles     *workSubtitles     `json:"subtitles,omitempty" jsonschema:"故事配音的角色与台词，整体替换"`
	SpeakerVoices []workSpeakerVoice `json:"speakerVoices,omitempty" jsonschema:"按角色分配参考音色"`
	Score         *scoreInput        `json:"score,omitempty" jsonschema:"完整乐谱（编曲），与写谱任务同一套校验"`
}

type workSubtitles struct {
	Speakers []workSubtitleSpeaker `json:"speakers" jsonschema:"角色列表，name 必填"`
	Cues     []workSubtitleCue     `json:"cues" jsonschema:"台词列表，speaker 为角色名或 1 开始的序号"`
}

type workSubtitleSpeaker struct {
	Name    string `json:"name" jsonschema:"角色名"`
	VoiceID string `json:"voiceId,omitempty" jsonschema:"该角色使用的参考音色 ID"`
}

type workSubtitleCue struct {
	Speaker string `json:"speaker" jsonschema:"角色名或 1 开始的序号"`
	Text    string `json:"text" jsonschema:"这句台词"`
}

type workSpeakerVoice struct {
	Speaker string `json:"speaker" jsonschema:"角色名或 1 开始的序号"`
	VoiceID string `json:"voiceId" jsonschema:"参考音色 ID；空字符串表示清除"`
}

func (w *Workbench) updateWorkTool(ctx context.Context, req *mcp.CallToolRequest, in updateWorkInput) (*mcp.CallToolResult, any, error) {
	d, err := w.assistantWork(in.ID)
	if err != nil {
		return nil, nil, err
	}
	kind := workKindOf(d)
	var changed []string
	patch := func(name string) { changed = append(changed, name) }
	if in.Title != nil {
		d.Title = truncateRunes(strings.TrimSpace(*in.Title), 120)
		patch("标题")
	}
	if in.Text != nil {
		d.Text = *in.Text
		patch("文本")
	}
	if in.Instrumental != nil {
		d.Instrumental = *in.Instrumental
		patch("纯音乐")
	}
	if in.Lyrics != nil {
		d.Lyrics = truncateRunes(*in.Lyrics, 4000)
		if d.Instrumental {
			for _, line := range strings.Split(d.Lyrics, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
					return nil, nil, fmt.Errorf("instrumental lyrics may contain section markers only (lines like [verse]), found %q", line)
				}
			}
		}
		patch("歌词")
	}
	if in.Language != nil {
		language := strings.TrimSpace(*in.Language)
		if language != "" && !slices.Contains(schema.MusicLanguages, language) {
			return nil, nil, fmt.Errorf("不支持的演唱语言 %s，可用：%s", language, strings.Join(schema.MusicLanguages, ", "))
		}
		d.SynthesisLanguage = language
		patch("演唱语言")
	}
	if in.ModelID != nil {
		model := strings.TrimSpace(*in.ModelID)
		if model != "" {
			if e := w.assistantModelCheck(model, kind); e != nil {
				return nil, nil, e
			}
		}
		d.ModelID = model
		patch("模型")
	}
	if in.VoiceID != nil {
		if voice := strings.TrimSpace(*in.VoiceID); voice != "" && !w.voiceExists(voice) {
			return nil, nil, fmt.Errorf("没有找到参考音色 %s，请先用 list_voices 查询", voice)
		}
		d.VoiceID = stringPtr(*in.VoiceID)
		patch("音色")
	}
	if in.Speed != nil {
		d.Speed = *in.Speed
		patch("语速")
	}
	if in.Seed != nil {
		d.Seed = in.Seed
		patch("种子")
	}
	if in.Subtitles != nil {
		if kind != "story" {
			return nil, nil, fmt.Errorf("subtitles 只适用于故事配音作品，这个作品是 %s", kind)
		}
		if e := applyWorkSubtitles(&d, *in.Subtitles); e != nil {
			return nil, nil, e
		}
		patch("角色与台词")
	}
	for _, assignment := range in.SpeakerVoices {
		if voice := strings.TrimSpace(assignment.VoiceID); voice != "" && !w.voiceExists(voice) {
			return nil, nil, fmt.Errorf("没有找到参考音色 %s，请先用 list_voices 查询", voice)
		}
	}
	if len(in.SpeakerVoices) > 0 {
		if kind != "story" || d.Subtitles == nil {
			return nil, nil, fmt.Errorf("speakerVoices 只适用于已有角色的故事配音作品")
		}
		if e := applySpeakerVoices(&d, in.SpeakerVoices); e != nil {
			return nil, nil, e
		}
		patch("角色音色")
	}
	if in.Score != nil {
		if kind != "score" {
			return nil, nil, fmt.Errorf("score 只适用于编曲作品，这个作品是 %s", kind)
		}
		score, e := buildScore(*in.Score)
		if e != nil {
			return nil, nil, e
		}
		d.Score = &score
		patch("乐谱")
	}
	if len(changed) == 0 {
		return nil, nil, fmt.Errorf("没有要修改的字段")
	}
	w.edit.Lock()
	err = w.saveDraft(d)
	w.edit.Unlock()
	if err != nil {
		return nil, nil, err
	}
	w.chatCard("update_work", fmt.Sprintf("修改了《%s》的%s", d.Title, strings.Join(changed, "、")), fmt.Sprintf("Updated %q: %s", d.Title, strings.Join(changed, ", ")), "done")
	w.chatUpdate(d.ID)
	summary := map[string]any{"id": d.ID, "title": d.Title, "changed": changed}
	if in.Score != nil && d.Score != nil {
		notes := 0
		for _, track := range d.Score.Tracks {
			notes += len(track.Notes)
		}
		summary["tracks"], summary["notes"] = len(d.Score.Tracks), notes
	}
	return nil, summary, nil
}

// applyWorkSubtitles 整体替换故事配音的角色与台词：每个角色继承作品当前的声音设置。
func applyWorkSubtitles(d *schema.Draft, in workSubtitles) error {
	if len(in.Speakers) == 0 || len(in.Cues) == 0 {
		return fmt.Errorf("subtitles 需要 speakers 与 cues，且都不能为空")
	}
	names := make([]string, len(in.Speakers))
	for i, s := range in.Speakers {
		names[i] = strings.TrimSpace(s.Name)
		if names[i] == "" || schema.TextLen(names[i]) > 60 {
			return fmt.Errorf("角色名不能为空且不超过 60 字：speakers[%d]", i)
		}
	}
	index := func(speaker string) (int, error) {
		if i := slices.Index(names, strings.TrimSpace(speaker)); i >= 0 {
			return i, nil
		}
		var ordinal int
		if _, e := fmt.Sscanf(strings.TrimSpace(speaker), "%d", &ordinal); e == nil && ordinal >= 1 && ordinal <= len(names) {
			return ordinal - 1, nil
		}
		return -1, fmt.Errorf("台词的 speaker %q 不在角色列表里", speaker)
	}
	var joined strings.Builder
	for i, cue := range in.Cues {
		text := strings.TrimSpace(cue.Text)
		if text == "" {
			return fmt.Errorf("台词不能为空：cues[%d]", i)
		}
		if _, e := index(cue.Speaker); e != nil {
			return e
		}
		joined.WriteString(text)
		if i < len(in.Cues)-1 {
			joined.WriteString("\n")
		}
	}
	if schema.TextLen(joined.String()) > 12000 {
		return fmt.Errorf("台词总长超过 12000 字")
	}
	settings := d.SynthesisSettings
	document := schema.SubtitleDocument{}
	for i, name := range names {
		speaker := schema.SubtitleSpeaker{ID: schema.NewID(), SourceName: name, Settings: &settings}
		if voice := strings.TrimSpace(in.Speakers[i].VoiceID); voice != "" {
			speaker.Settings.VoiceID = stringPtr(voice)
		}
		document.Speakers = append(document.Speakers, speaker)
	}
	// 台词时间先按每句 2 秒顺排（与界面新建台词一致），用户随后可在时间线上调整。
	for i, cue := range in.Cues {
		speaker, _ := index(cue.Speaker)
		document.Cues = append(document.Cues, schema.SubtitleCue{ID: schema.NewID(), SpeakerID: document.Speakers[speaker].ID, Text: strings.TrimSpace(cue.Text), Start: int64(i) * 2000, End: int64(i+1) * 2000})
	}
	d.Subtitles, d.Text = &document, joined.String()
	return nil
}

// applySpeakerVoices 只改角色音色，不动台词；speaker 按角色名或 1 开始的序号定位。
func applySpeakerVoices(d *schema.Draft, assignments []workSpeakerVoice) error {
	for _, assignment := range assignments {
		matched := false
		for i := range d.Subtitles.Speakers {
			speaker := &d.Subtitles.Speakers[i]
			ordinal := 0
			if _, e := fmt.Sscanf(strings.TrimSpace(assignment.Speaker), "%d", &ordinal); e == nil && ordinal == i+1 || speaker.SourceName == strings.TrimSpace(assignment.Speaker) {
				if speaker.Settings == nil {
					settings := d.SynthesisSettings
					speaker.Settings = &settings
				}
				if voice := strings.TrimSpace(assignment.VoiceID); voice != "" {
					speaker.Settings.VoiceID = stringPtr(voice)
				} else {
					speaker.Settings.VoiceID = nil
				}
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("speakerVoices 里的 %q 不在角色列表里", assignment.Speaker)
		}
	}
	return nil
}

// generateWorkInput 的动作名按作品类型显示（语音/音乐/渲染）。
func (w *Workbench) generateWorkTool(ctx context.Context, req *mcp.CallToolRequest, in workIDInput) (*mcp.CallToolResult, any, error) {
	d, err := w.assistantWork(in.ID)
	if err != nil {
		return nil, nil, err
	}
	action, actionEN := "生成语音", "generate voice"
	switch workKindOf(d) {
	case "music":
		action, actionEN = "生成音乐", "generate music"
	case "score":
		action, actionEN = "渲染配乐", "render the arrangement"
	}
	w.chatCard("generate_work", fmt.Sprintf("正在%s《%s》…", action, d.Title), fmt.Sprintf("%s for %q…", actionEN, d.Title), "running")
	if err = w.Generate(d); err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"started": true, "hint": "生成在后台进行，用 get_generation_status 查询进度与结果"}, nil
}

func (w *Workbench) getGenerationStatusTool(ctx context.Context, req *mcp.CallToolRequest, in workIDInput) (*mcp.CallToolResult, any, error) {
	d, err := w.assistantWork(in.ID)
	if err != nil {
		return nil, nil, err
	}
	s := w.Store.Read()
	out := map[string]any{"id": d.ID}
	if a := s.Activity; a != nil && a.ProjectID == d.ID {
		progress := map[string]any{"status": a.Status}
		if a.Status == "running" {
			progress["description"] = a.Code
		}
		if a.ErrorCode != nil {
			progress["errorCode"], progress["errorParams"] = *a.ErrorCode, a.ErrorParams
		}
		out["activity"] = progress
	}
	for _, g := range s.History {
		if g.Settings.ID == d.ID {
			out["latest"] = map[string]any{"id": g.ID, "title": g.Title, "duration": g.Duration, "createdAt": g.CreatedAt}
			break
		}
	}
	w.chatCard("get_generation_status", fmt.Sprintf("查看了《%s》的生成进度", d.Title), fmt.Sprintf("Checked generation status of %q", d.Title), "done")
	return nil, out, nil
}

func (w *Workbench) openWorkTool(ctx context.Context, req *mcp.CallToolRequest, in workIDInput) (*mcp.CallToolResult, any, error) {
	d, err := w.assistantWork(in.ID)
	if err != nil {
		return nil, nil, err
	}
	w.chatCard("open_work", fmt.Sprintf("打开《%s》", d.Title), fmt.Sprintf("Opened %q", d.Title), "done")
	w.chatNav(d.ID, "")
	return nil, map[string]any{"opened": true}, nil
}

// pageCardLabels 是 open_page 卡片摘要里的页面名。
var pageCardLabels = map[string][2]string{
	"text": {"语音生成页", "voice generation"}, "story": {"故事配音页", "story dubbing"}, "music": {"音乐生成页", "music generation"},
	"score": {"编曲页", "arrangement"}, "characters": {"声音库页", "voice library"}, "voices": {"参考音色页", "reference library"},
	"history": {"历史页", "history"}, "settings": {"设置页", "settings"},
}

type pageInput struct {
	Page string `json:"page" jsonschema:"页面：text、story、music、score、characters 或 settings"`
}

func (w *Workbench) openPageTool(ctx context.Context, req *mcp.CallToolRequest, in pageInput) (*mcp.CallToolResult, any, error) {
	if !slices.Contains(assistantPages, in.Page) {
		return nil, nil, fmt.Errorf("page 必须是 %s 之一", strings.Join(assistantPages, ", "))
	}
	names := pageCardLabels[in.Page]
	w.chatCard("open_page", "切换到"+names[0], "Switched to "+names[1], "done")
	w.chatNav("", in.Page)
	return nil, map[string]any{"opened": true}, nil
}

// voiceExists 判断参考音色 ID 是否已上传。
func (w *Workbench) voiceExists(id string) bool {
	return slices.ContainsFunc(w.Store.Read().Voices, func(v schema.Voice) bool { return v.ID == id })
}

// assistantModelCheck 校验模型存在、类型匹配且已安装；未安装的模型让用户去设置里下载。
func (w *Workbench) assistantModelCheck(model, kind string) error {
	definition, e := catalog.Lookup(model)
	if e != nil {
		return fmt.Errorf("未知模型 %s，请用 list_models 查询可用模型", model)
	}
	// 目录 family：语音类各不相同，音乐是 ace_step/yue2，编曲渲染用 soundfont。
	music, soundfont := definition.Family == "ace_step" || definition.Family == "yue2", definition.Family == "soundfont"
	if kind == "score" && !soundfont || kind == "music" && !music || (kind == "text" || kind == "story") && (music || soundfont) {
		return fmt.Errorf("模型 %s（%s）不适用于%s作品", model, definition.Family, kind)
	}
	for _, installed := range w.Store.Read().Models {
		if installed.ID == model {
			return nil
		}
	}
	return fmt.Errorf("模型 %s 尚未安装，请让用户在“设置 › 模型”中下载，助手不能代为下载", model)
}

func stringPtr(s string) *string { return &s }
