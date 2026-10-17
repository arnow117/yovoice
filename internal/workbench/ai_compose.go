package workbench

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// ScoreBrief 是“AI 写谱”的输入：一句话描述、目标时长（秒，0 表示由模型决定）和界面语言。
// FollowUp 与 Draft 用于“继续修改”：把编辑器里的当前乐谱连同修改意见交回助手。
type ScoreBrief struct {
	ProjectID string          `json:"projectId"`
	Brief     string          `json:"brief"`
	Seconds   int             `json:"seconds"`
	Locale    schema.UiLocale `json:"locale"`
	FollowUp  string          `json:"followUp,omitempty"`
	Draft     *schema.Score   `json:"draft,omitempty"`
}

// ComposedScore 是 AI 写谱的结果：乐谱和顺带起的曲名，曲名只在作品尚未命名时由界面采用。
type ComposedScore struct {
	schema.Score
	Title string `json:"title,omitempty"`
}

// scoreRepeat 让模型只写一遍循环段落，由这里展开成音符，长曲子不必逐个写出。
type scoreRepeat struct {
	From int   `json:"from"`
	To   int   `json:"to"`
	At   []int `json:"at"`
}

// expandRepeats 把 From..To 小节的音符复制到每个 At 起点；展开后的总数仍受乐谱音符上限约束。
func expandRepeats(track *schema.ScoreTrack, repeats []scoreRepeat, budget int) error {
	written := track.Notes
	for _, r := range repeats {
		if r.From < 1 || r.To < r.From || r.To > schema.ScoreMaxBars {
			return fmt.Errorf("track %q: repeats need 1 <= from <= to <= %d", track.ID, schema.ScoreMaxBars)
		}
		for _, at := range r.At {
			if at < 1 || at > schema.ScoreMaxBars {
				return fmt.Errorf("track %q: repeat targets must be bars 1-%d", track.ID, schema.ScoreMaxBars)
			}
			for _, n := range written {
				if n.Bar < r.From || n.Bar > r.To {
					continue
				}
				if len(track.Notes) >= budget {
					return fmt.Errorf("score needs 1-%d notes after repeats", schema.ScoreMaxNotes)
				}
				n.Bar += at - r.From
				track.Notes = append(track.Notes, n)
			}
		}
	}
	slices.SortStableFunc(track.Notes, func(a, b schema.ScoreNote) int {
		if a.Bar != b.Bar {
			return a.Bar - b.Bar
		}
		return cmp.Compare(a.Beat, b.Beat)
	})
	return nil
}

func (in ScoreBrief) valid() bool {
	brief := strings.TrimSpace(in.Brief)
	return brief != "" && schema.TextLen(brief) <= 2000 && schema.TextLen(strings.TrimSpace(in.FollowUp)) <= 2000 &&
		in.Seconds >= 0 && in.Seconds <= int(schema.ScoreMaxSeconds)
}

// StartScore 在后台写谱，立即返回任务。
func (w *Workbench) StartScore(in ScoreBrief) (schema.AITask, error) {
	if !in.valid() {
		return schema.AITask{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.startAI("score", in.ProjectID, strings.TrimSpace(in.Brief), func(ctx context.Context, task schema.AITask) (any, error) {
		return w.composeScore(ctx, task.ID, in)
	})
}

// ComposeScore 由内置助手写谱并校验；结果取自助手以 submit_score 提交的乐谱。
func (w *Workbench) ComposeScore(ctx context.Context, in ScoreBrief) (ComposedScore, error) {
	return w.composeScore(ctx, "", in)
}

func (w *Workbench) composeScore(ctx context.Context, taskID string, in ScoreBrief) (ComposedScore, error) {
	if !in.valid() {
		return ComposedScore{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	draft := &aiDraft{kind: "score", taskID: taskID, projectID: in.ProjectID, brief: strings.TrimSpace(in.Brief),
		followUp: strings.TrimSpace(in.FollowUp), seconds: in.Seconds, locale: in.Locale, scoreDraft: in.Draft}
	if err := w.askAssistant(ctx, draft, scoreMessage(taskID, in)); err != nil {
		return ComposedScore{}, err
	}
	if score := draft.score(); score != nil {
		return *score, nil
	}
	return ComposedScore{}, msg.Err(msg.ErrAIResultInvalid, msg.Params{"detail": "assistant ended without submitting a score"})
}

// scoreMessage 组装发给助手的写谱任务消息；followUp 非空时为继续修改。
func scoreMessage(taskID string, in ScoreBrief) string {
	brief := strings.TrimSpace(in.Brief)
	if followUp := strings.TrimSpace(in.FollowUp); followUp != "" {
		return fmt.Sprintf("请继续修改这份乐谱。任务类型：score。任务 ID：%s。\n原始需求：%s\n修改意见：%s\n请先 get_draft 查看当前草稿，修改后用 submit_score 重新提交。", taskID, brief, followUp)
	}
	return fmt.Sprintf("请写谱。任务类型：score。任务 ID：%s。\n需求：%s", taskID, brief)
}

// LyricsBrief 是“AI 写歌词”的输入。FollowUp 与 Draft 用于“继续修改”。
type LyricsBrief struct {
	ProjectID    string          `json:"projectId"`
	Brief        string          `json:"brief"`
	Instrumental bool            `json:"instrumental"`
	Language     string          `json:"language"`
	Locale       schema.UiLocale `json:"locale"`
	FollowUp     string          `json:"followUp,omitempty"`
	Draft        *Lyrics         `json:"draft,omitempty"`
}

// Lyrics 是写给音乐作品的风格描述与带段落标记的歌词。
type Lyrics struct {
	Title    string `json:"title"`
	Style    string `json:"style"`
	Lyrics   string `json:"lyrics"`
	Language string `json:"language"`
}

func (in LyricsBrief) valid() bool {
	brief := strings.TrimSpace(in.Brief)
	return brief != "" && schema.TextLen(brief) <= 2000 && schema.TextLen(strings.TrimSpace(in.FollowUp)) <= 2000
}

// StartLyrics 在后台写歌词，立即返回任务。
func (w *Workbench) StartLyrics(in LyricsBrief) (schema.AITask, error) {
	if !in.valid() {
		return schema.AITask{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.startAI("lyrics", in.ProjectID, strings.TrimSpace(in.Brief), func(ctx context.Context, task schema.AITask) (any, error) {
		return w.writeLyrics(ctx, task.ID, in)
	})
}

// WriteLyrics 由内置助手写风格描述与歌词；结果取自助手以 submit_lyrics 提交的值。
func (w *Workbench) WriteLyrics(ctx context.Context, in LyricsBrief) (Lyrics, error) {
	return w.writeLyrics(ctx, "", in)
}

func (w *Workbench) writeLyrics(ctx context.Context, taskID string, in LyricsBrief) (Lyrics, error) {
	if !in.valid() {
		return Lyrics{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	draft := &aiDraft{kind: "lyrics", taskID: taskID, projectID: in.ProjectID, brief: strings.TrimSpace(in.Brief),
		followUp: strings.TrimSpace(in.FollowUp), instrumental: in.Instrumental, musicLanguage: in.Language, locale: in.Locale, lyricsDraft: in.Draft}
	if err := w.askAssistant(ctx, draft, lyricsMessage(taskID, in)); err != nil {
		return Lyrics{}, err
	}
	if lyrics := draft.lyrics(); lyrics != nil {
		return *lyrics, nil
	}
	return Lyrics{}, msg.Err(msg.ErrAIResultInvalid, msg.Params{"detail": "assistant ended without submitting lyrics"})
}

// lyricsMessage 组装发给助手的写词任务消息；followUp 非空时为继续修改。
func lyricsMessage(taskID string, in LyricsBrief) string {
	brief := strings.TrimSpace(in.Brief)
	if followUp := strings.TrimSpace(in.FollowUp); followUp != "" {
		return fmt.Sprintf("请继续修改这首歌词。任务类型：lyrics。任务 ID：%s。\n原始需求：%s\n修改意见：%s\n请先 get_draft 查看当前草稿，修改后用 submit_lyrics 重新提交。", taskID, brief, followUp)
	}
	task := fmt.Sprintf("请写词。任务类型：lyrics。任务 ID：%s。\n需求：%s", taskID, brief)
	if in.Instrumental {
		task += "\n这是纯音乐：歌词只保留段落标记。"
	}
	return task
}
