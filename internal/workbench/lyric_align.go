package workbench

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"yovoice/internal/audio"
	"yovoice/internal/catalog"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 逐字对齐：Qwen3 Forced Aligner 一次最多对齐约 5 分钟，更长的歌按段对齐；整首与段数设上限，避免误传超长音频。
const alignChunkSeconds, maxAlignSeconds, maxAlignChunks = 300.0, 1200.0, 64

// BS-RoFormer 只接受 44.1 kHz 输入；对齐模型使用 16 kHz。
const separateRate, alignRate = 44100, 16000

// LyricChunk 是一段要对齐的歌词和它在歌曲里的大致范围（秒）。界面按现有的逐行时间切段，短歌只有一段。
type LyricChunk struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// sungLyrics 去掉段落标记行，返回要唱的歌词，每行一句。
func sungLyrics(d schema.Draft) string {
	if d.Instrumental {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(d.Lyrics, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !(strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// alignLanguage 按歌词文字选对齐语言：有假名算日语，有谚文算韩语，有汉字算中文，西里尔字母算俄语，其余按英语。
func alignLanguage(text string) string {
	han := false
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			return "Japanese"
		case unicode.Is(unicode.Hangul, r):
			return "Korean"
		case unicode.Is(unicode.Han, r):
			han = true
		case unicode.Is(unicode.Cyrillic, r) && !han:
			return "Russian"
		}
	}
	if han {
		return "Chinese"
	}
	return "English"
}

// AlignLyrics 用可选下载的歌词对齐模型给一个生成版本做逐字对齐：先用 BS-RoFormer 分出人声，再逐段用 Qwen3 Forced Aligner
// 把歌词对到人声上。结果写进 LyricWords 并替换手动校准；chunks 为空时整首作为一段。
func (w *Workbench) AlignLyrics(id string, chunks []LyricChunk) error {
	path, e := w.MediaFile("outputs", id)
	if e != nil {
		return e
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	s := w.Store.Read()
	i := slices.IndexFunc(s.History, func(g schema.Generation) bool { return g.ID == id })
	if i < 0 {
		return msg.Err(msg.ErrAudioMissing, nil)
	}
	generation := s.History[i]
	lyrics, duration := sungLyrics(generation.Settings), generation.Duration
	if lyrics == "" {
		return msg.Err(msg.ErrAlignLyrics, nil)
	}
	if len(chunks) == 0 {
		chunks = []LyricChunk{{Text: lyrics, Start: 0, End: duration}}
	}
	if len(chunks) > maxAlignChunks || duration > maxAlignSeconds {
		return msg.Err(msg.ErrAudioDuration, nil)
	}
	for j, c := range chunks {
		if strings.TrimSpace(c.Text) == "" || schema.TextLen(c.Text) > 12000 || math.IsNaN(c.Start) || math.IsNaN(c.End) || c.Start < 0 || c.End <= c.Start || c.End > duration+1 ||
			c.End-c.Start > alignChunkSeconds || (j > 0 && c.Start < chunks[j-1].Start) {
			return msg.Err(msg.ErrRequestInvalid, nil)
		}
	}
	m := slices.IndexFunc(s.Models, func(m schema.InstalledModel) bool {
		p, e := catalog.Lookup(m.ID)
		return e == nil && p.Family == "lyric_align"
	})
	if m < 0 {
		return msg.Err(msg.ErrAlignModelRequired, nil)
	}
	model, e := w.engineModel(s, s.Models[m].ID)
	if e != nil {
		return e
	}
	_, e = w.start("generate", msg.ActivitySeparateVocals, nil, &model.ID, func(ctx context.Context) error {
		if e := os.MkdirAll(filepath.Join(w.Store.Root, "runtime"), 0700); e != nil {
			return e
		}
		dir, e := os.MkdirTemp(filepath.Join(w.Store.Root, "runtime"), "align-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(dir)
		progress := func(code msg.Code, params msg.Params) { w.progress(code, params, 0, 1) }
		samples, rate, e := audio.ReadMono(path)
		if e != nil {
			return e
		}
		mix, vocals := filepath.Join(dir, "mix.wav"), filepath.Join(dir, "vocals.wav")
		if e = audio.WriteMono(mix, audio.Resample(samples, rate, separateRate), separateRate); e != nil {
			return e
		}
		if e = w.engine.SeparateVocals(ctx, *s.RuntimePath, model, s.Preferences.Backend, mix, vocals, progress); e != nil {
			return e
		}
		if samples, rate, e = audio.ReadMono(vocals); e != nil {
			return e
		}
		voice := audio.Resample(samples, rate, alignRate)
		language := alignLanguage(lyrics)
		var words []schema.LyricWord
		for j, c := range chunks {
			w.progress(msg.ActivityAlignLyrics, msg.Params{"done": j + 1, "total": len(chunks)}, int64(j), int64(len(chunks)))
			from, to := min(len(voice), int(c.Start*alignRate)), min(len(voice), int(c.End*alignRate))
			if to-from < alignRate/10 {
				continue
			}
			part := filepath.Join(dir, "part.wav")
			if e = audio.WriteMono(part, voice[from:to], alignRate); e != nil {
				return e
			}
			aligned, e := w.engine.AlignWords(ctx, *s.RuntimePath, model, s.Preferences.Backend, part, c.Text, language, func(code msg.Code, params msg.Params) {})
			if e != nil {
				return e
			}
			for _, a := range aligned {
				words = append(words, schema.LyricWord{Word: a.Word, Start: round3(c.Start + a.Start), End: round3(c.Start + max(a.Start, a.End))})
			}
		}
		if len(words) == 0 {
			return msg.Err(msg.ErrAlignEmpty, nil)
		}
		diag.Log(w.Store.Root, "lyrics.aligned", "generation_id", id, "chunks", len(chunks), "words", len(words), "language", language)
		return w.Store.Update(func(s *schema.State) {
			for i := range s.History {
				if s.History[i].ID == id {
					s.History[i].LyricWords, s.History[i].LyricTimes = words, nil
				}
			}
		}, true)
	}, "", generation.Settings.ID)
	if e == nil {
		_ = w.Store.Update(func(s *schema.State) {
			if s.Activity != nil {
				s.Activity.Task = "align"
			}
		}, false)
	}
	return e
}

// round3 保留到毫秒，状态文件里不写无意义的长小数。
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
