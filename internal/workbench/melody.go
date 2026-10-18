package workbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/catalog"
	"yovoice/internal/diag"
	"yovoice/internal/llm"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

// 旋律谱（YuE2 的 ABC 方言）：先出谱或从歌曲扒谱，改谱后按谱生成整首歌。
// 出谱与扒谱占用推理引擎，结果放在会话状态 Melody 里，由界面填入作品；谱面校验与比较在 score 包。

// coverSeconds 是扒谱接受的最长歌曲时长，与 YuE2 谱面上限一致。
const coverSeconds = int(schema.ScoreMaxSeconds)

// MelodyInspection 是界面显示旋律谱所需的信息：谱面问题、概要和转换后的轨道。
type MelodyInspection struct {
	Issue   *score.ABCIssue `json:"issue,omitempty"`
	Tempo   int             `json:"tempo,omitempty"`
	Meter   [2]int          `json:"meter,omitempty"`
	Key     string          `json:"key,omitempty"`
	Bars    int             `json:"bars,omitempty"`
	Seconds float64         `json:"seconds,omitempty"`
	Chords  bool            `json:"chords,omitempty"`
	Score   *schema.Score   `json:"score,omitempty"`
}

// InspectMelody 校验并概括旋律谱；谱面有误时只返回问题。
func InspectMelody(text string) MelodyInspection {
	s, e := score.ParseABC(text)
	if e != nil {
		var issue *score.ABCIssue
		if !errors.As(e, &issue) {
			issue = &score.ABCIssue{Code: "token", Detail: e.Error()}
		}
		return MelodyInspection{Issue: issue}
	}
	converted := s.Score()
	return MelodyInspection{Tempo: s.Tempo, Meter: s.Meter, Key: converted.Key, Bars: s.Bars(), Seconds: s.Seconds(), Chords: s.HasChords(), Score: &converted}
}

// TransformMelody 是不需要模型的快捷改谱：stripChords 去掉和弦，tempo 改速度。
func TransformMelody(text, op string, tempo int) (string, error) {
	var out string
	var e error
	switch op {
	case "stripChords":
		out, e = score.StripABCChords(text)
	case "tempo":
		out, e = score.SetABCTempo(text, tempo)
	default:
		return "", msg.Err(msg.ErrRequestInvalid, nil)
	}
	if e != nil {
		return "", melodyIssue(e)
	}
	return out, nil
}

func melodyIssue(e error) error {
	var issue *score.ABCIssue
	if errors.As(e, &issue) {
		return msg.Err(msg.ErrMelodyInvalid, msg.Params{"bar": issue.Bar, "voice": issue.Voice, "code": issue.Code, "detail": issue.Detail})
	}
	return e
}

// PlanMelody 让 YuE2 按风格与歌词写出旋律谱（只跑规划阶段），结果写入会话状态。
func (w *Workbench) PlanMelody(d schema.Draft) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	d.Abc = ""
	if e := schema.Validate(d); e != nil {
		return e
	}
	m, e := catalog.Lookup(d.ModelID)
	if e != nil || m.Family != "yue2" {
		return msg.Err(msg.ErrModelUnsupported, nil)
	}
	s := w.Store.Read()
	model, e := w.engineModel(s, d.ModelID)
	if e != nil {
		return e
	}
	if e = w.saveDraftKeepingMelody(d); e != nil {
		return e
	}
	_, e = w.start("generate", msg.ActivityPlanMelody, nil, &d.ModelID, func(ctx context.Context) error {
		abc, truncated, e := w.engine.PlanMelody(ctx, *s.RuntimePath, model, s.Preferences.Backend, d, func(code msg.Code, params msg.Params) { w.progress(code, params, 0, 1) })
		if e != nil {
			return e
		}
		diag.Log(w.Store.Root, "melody.planned", "project_id", d.ID, "bytes", len(abc), "truncated", truncated)
		return w.offerMelody(d.ID, "plan", abc, truncated)
	}, "", d.ID)
	if e == nil {
		w.tagMelody()
	}
	return e
}

// CoverMelody 用 SheetSage2 从上传的歌曲扒出旋律谱，用于翻唱。扒出的和弦默认去掉，
// 按谱生成时改用 melody 模式，让新风格自由编配伴奏（YuE2 官方翻唱流程）。
func (w *Workbench) CoverMelody(projectID string, r io.Reader) error {
	if !schema.ValidID(projectID) {
		return msg.Err(msg.ErrDraftIDInvalid, nil)
	}
	upload, e := w.receive(r)
	if e != nil {
		return e
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	s := w.Store.Read()
	i := slices.IndexFunc(s.Models, func(m schema.InstalledModel) bool {
		p, e := catalog.Lookup(m.ID)
		return e == nil && p.Family == "sheetsage2"
	})
	if i < 0 {
		_ = os.Remove(upload)
		return msg.Err(msg.ErrCoverModelRequired, nil)
	}
	model, e := w.engineModel(s, s.Models[i].ID)
	if e != nil {
		_ = os.Remove(upload)
		return e
	}
	converter, e := audio.Converter()
	if e != nil {
		_ = os.Remove(upload)
		return e
	}
	_, e = w.start("generate", msg.ActivityTranscribe, nil, &model.ID, func(ctx context.Context) error {
		defer os.Remove(upload)
		wav := upload + ".wav"
		defer os.Remove(wav)
		if e := audio.ConvertWithLimit(ctx, converter, upload, wav, coverSeconds); e != nil {
			return e
		}
		if seconds, e := audio.Duration(wav); e != nil {
			return e
		} else if seconds > float64(coverSeconds) {
			return msg.Err(msg.ErrAudioDuration, nil)
		}
		abc, e := w.engine.Transcribe(ctx, *s.RuntimePath, model, s.Preferences.Backend, wav, func(code msg.Code, params msg.Params) { w.progress(code, params, 0, 1) })
		if e != nil {
			return e
		}
		if stripped, e := score.StripABCChords(abc); e == nil {
			abc = stripped
		}
		diag.Log(w.Store.Root, "melody.transcribed", "project_id", projectID, "bytes", len(abc))
		return w.offerMelody(projectID, "cover", abc, false)
	}, "", projectID)
	if e == nil {
		w.tagMelody()
	} else {
		_ = os.Remove(upload)
	}
	return e
}

// tagMelody 标记当前生成操作是出谱或扒谱；引擎进度会改写消息码，界面靠这个标记区分。
func (w *Workbench) tagMelody() {
	_ = w.Store.Update(func(s *schema.State) {
		if s.Activity != nil {
			s.Activity.Task = "melody"
		}
	}, false)
}

// engineModel 检查推理引擎与模型可用，返回已安装的模型。要求调用方持有编辑锁。
func (w *Workbench) engineModel(s schema.State, id string) (schema.InstalledModel, error) {
	if !RuntimeReady(s) {
		return schema.InstalledModel{}, msg.Err(msg.ErrRuntimeRequired, nil)
	}
	i := slices.IndexFunc(s.Models, func(m schema.InstalledModel) bool { return m.ID == id })
	if i < 0 {
		return schema.InstalledModel{}, msg.Err(msg.ErrModelRequired, nil)
	}
	if e := modelEngineReady(s, id); e != nil {
		return schema.InstalledModel{}, e
	}
	if installing(s, id) {
		return schema.InstalledModel{}, msg.Err(msg.ErrBusy, nil)
	}
	if _, e := os.Stat(s.Models[i].Path); e != nil {
		return schema.InstalledModel{}, msg.Err(msg.ErrModelMoved, nil)
	}
	return s.Models[i], nil
}

// saveDraftKeepingMelody 保存出谱前的作品，但保留已有的旋律谱，出谱结果由界面决定是否替换。
func (w *Workbench) saveDraftKeepingMelody(d schema.Draft) error {
	s := w.Store.Read()
	if i := slices.IndexFunc(s.Drafts, func(v schema.Draft) bool { return v.ID == d.ID }); i >= 0 {
		d.Abc = s.Drafts[i].Abc
	}
	return w.saveDraft(d)
}

func (w *Workbench) offerMelody(projectID, source, abc string, truncated bool) error {
	result := schema.MelodyResult{ID: schema.NewID(), ProjectID: projectID, Source: source, Abc: abc, Truncated: truncated, CreatedAt: time.Now().UTC()}
	return w.Store.Update(func(s *schema.State) { s.Melody = &result }, false)
}

// DismissMelody 在界面填入或放弃出谱结果后清除。
func (w *Workbench) DismissMelody(id string) error {
	return w.Store.Update(func(s *schema.State) {
		if s.Melody != nil && s.Melody.ID == id {
			s.Melody = nil
		}
	}, false)
}

// MelodyBrief 是“AI 改谱”的输入：当前谱（为空时按歌词写一份）、风格与歌词，以及一句话要求。
type MelodyBrief struct {
	ProjectID string          `json:"projectId"`
	Brief     string          `json:"brief"`
	Abc       string          `json:"abc"`
	Style     string          `json:"style"`
	Lyrics    string          `json:"lyrics"`
	Locale    schema.UiLocale `json:"locale"`
}

// MelodyEdit 是 AI 改谱的结果：完整的新谱、需要时一并调整的风格、一句改动说明和按小节比较的改动。
type MelodyEdit struct {
	Abc     string            `json:"abc"`
	Style   string            `json:"style,omitempty"`
	Summary string            `json:"summary,omitempty"`
	Changes *score.ABCChanges `json:"changes,omitempty"`
}

// melodyPrompt 沿用 YuE2 官方 yue2-music skill（Apache-2.0）的改谱约束：保持原生方言、先定不变量、按音符而非字符修改。
const melodyPrompt = `You edit melody scores for YuE2, a song model that sings lyrics over a score written in a strict native ABC dialect.

Native dialect, all required:
- Header lines exactly in this order: X:1 / T: (blank) / M:<n>/<d> / L:1/<power of two> (keep the source value, usually 1/32) / Q:1/4=<integer BPM> / V: Vocal clef=treble name="Vocal Melody" snm="Vocal" / V: Ins clef=treble name="Ins Melody" snm="Inst." / K:<major or minor key such as G, Bb, F#m>.
- Music is written in groups of 1 to 4 measures: a "V: Vocal" line and its music line, then a "V: Ins" line and its music line with the same number of measures. Each music line ends with "|".
- Sections are comment lines such as "%% verse", "%% chorus", "%% bridge", "%% interlude" before a group.
- Both voices are monophonic melodies. Ins carries instrumental themes or solos and rests while the vocal sings; it is not a chord staff.
- Chord symbols are quoted and only in Vocal, also while Vocal rests: "C", "Am7", "F#m7/C#". Allowed qualities: major (no suffix), m, dim, aug, 7, maj7, m7, dim7, m7b5, sus4, sus2, 6, m6, 7sus4, m(maj7); optional /bass.
- With L:1/32 a 4/4 measure holds exactly 32 units (3/4 and 6/8 hold 24). Duration multipliers are only 1,2,3,4,6,8,12,16,24,32,48; write other lengths as tied notes like C8-C2. z is a rest; Z, Z2, Z3, Z4 are whole resting measures.
- c is an octave above C, c' above c, C, below C. ^ sharp, _ flat, = natural; accidentals last to the barline and apply to the same letter in every octave. Key signature applies to unmarked notes.
- A tie (-) joins equal pitches; a chord change inside a held note splits it with a tie: "C"E16-"Am7"E16. A tie never enters a rest and the score never ends on a tie.
- Not allowed: lyric w: lines, tuplets, grace notes, bracketed note stacks, slurs, repeat signs, decorations, %%%% directives, extra voices.

How to edit:
- Change only what the request asks for and keep everything else note for note: other sections, the other voice, chords, tempo, key and meter. Keep section comments and bar grouping.
- Keep the vocal phrases singable for the lyrics: roughly one note per syllable, held notes on long vowels, rests where lines breathe.
- Check that every measure adds up to the meter before answering.
- When the request changes the arrangement feel (genre, instruments, tempo, vocal character), also return an updated style line in English tags; otherwise leave the style unchanged.

Answer in exactly this shape and nothing else:
SUMMARY: <one short sentence in %s describing what changed>
STYLE: <updated style tags, or the word unchanged>
` + "```abc\n<the complete score>\n```"

var abcFence = regexp.MustCompile("(?s)```(?:abc)?\\s*\\n(.*?)```")

// decodeMelody 取出模型回答中的谱、风格与改动说明，谱面不合方言时返回具体问题。
func decodeMelody(text string) (MelodyEdit, error) {
	m := abcFence.FindStringSubmatch(text)
	if m == nil {
		return MelodyEdit{}, errors.New("no ```abc fenced score found")
	}
	edit := MelodyEdit{Abc: strings.TrimSpace(m[1]) + "\n"}
	for _, line := range strings.Split(text[:strings.Index(text, m[0])], "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "SUMMARY:"); ok {
			edit.Summary = truncateRunes(strings.TrimSpace(v), 200)
		} else if v, ok := strings.CutPrefix(line, "STYLE:"); ok {
			if v = strings.TrimSpace(v); !strings.EqualFold(v, "unchanged") && v != "" {
				edit.Style = truncateRunes(v, 512)
			}
		}
	}
	if _, e := score.ParseABC(edit.Abc); e != nil {
		return MelodyEdit{}, e
	}
	return edit, nil
}

func (in MelodyBrief) valid() bool {
	brief := strings.TrimSpace(in.Brief)
	return brief != "" && schema.TextLen(brief) <= 2000 && len(in.Abc) <= score.ABCMaxLength && schema.TextLen(in.Lyrics) <= 4000 && schema.TextLen(in.Style) <= 512
}

// StartMelodyEdit 在后台改谱，立即返回任务。
func (w *Workbench) StartMelodyEdit(in MelodyBrief) (schema.AITask, error) {
	if !in.valid() {
		return schema.AITask{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	if strings.TrimSpace(in.Abc) != "" {
		if _, e := score.ParseABC(in.Abc); e != nil {
			return schema.AITask{}, melodyIssue(e)
		}
	}
	return w.startAI("melody", in.ProjectID, strings.TrimSpace(in.Brief), func(ctx context.Context) (any, error) { return w.EditMelody(ctx, in) })
}

// EditMelody 让模型按要求改谱（没有谱时按歌词写一份），校验不过时把具体小节的问题发回修正，最多两轮。
func (w *Workbench) EditMelody(ctx context.Context, in MelodyBrief) (MelodyEdit, error) {
	if !in.valid() {
		return MelodyEdit{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	var before *score.ABCScore
	ask := fmt.Sprintf("Style: %s\n\nLyrics:\n%s\n\n", strings.TrimSpace(in.Style), strings.TrimSpace(in.Lyrics))
	if strings.TrimSpace(in.Abc) != "" {
		var e error
		if before, e = score.ParseABC(in.Abc); e != nil {
			return MelodyEdit{}, melodyIssue(e)
		}
		ask += "Current score:\n```abc\n" + strings.TrimSpace(in.Abc) + "\n```\n\nRequest: " + strings.TrimSpace(in.Brief)
	} else {
		ask += "There is no score yet. Write a complete score for these lyrics: one section comment per lyric section, a singable vocal line with chords, and short instrumental themes in Ins between vocal phrases.\n\nRequest: " + strings.TrimSpace(in.Brief)
	}
	request := llm.Request{System: fmt.Sprintf(melodyPrompt, scoreLanguage(in.Locale)), Messages: []llm.Message{{Role: "user", Content: ask}}, MaxTokens: 16000, Temperature: 0.4}
	var issue string
	for attempt := 0; attempt < 3; attempt++ {
		result, e := w.complete(ctx, request)
		if e != nil {
			return MelodyEdit{}, e
		}
		var edit MelodyEdit
		if result.Truncated {
			e = errors.New("output was cut off by the length limit")
		} else {
			edit, e = decodeMelody(result.Text)
		}
		if e == nil {
			if before != nil {
				after, _ := score.ParseABC(edit.Abc)
				changes := score.CompareABC(before, after)
				edit.Changes = &changes
			}
			return edit, nil
		}
		issue = e.Error()
		w.logAIOutput("melody", issue, result.Text)
		w.setAIStage("repairing")
		fix := "The score is invalid: " + issue + ". Fix it and answer again in the same shape with the complete score."
		if result.Truncated {
			fix = "Your answer was cut off. Answer again with the complete score and nothing else besides SUMMARY and STYLE."
		}
		request.Messages = append(request.Messages, llm.Message{Role: "assistant", Content: result.Text}, llm.Message{Role: "user", Content: fix})
	}
	return MelodyEdit{}, msg.Err(msg.ErrAIResultInvalid, msg.Params{"detail": issue})
}
