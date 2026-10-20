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
		spans := voicedSpans(voice, alignRate)
		// 去掉长空白后整首唱的部分不超过对齐模型的上限时，整首歌词一次对齐，不依赖界面估计的分段边界。
		if spanLength(spans, 0, len(voice)) <= int(alignChunkSeconds*alignRate) {
			chunks = []LyricChunk{{Text: lyrics, Start: 0, End: float64(len(voice)) / alignRate}}
		}
		var words []schema.LyricWord
		for j, c := range chunks {
			w.progress(msg.ActivityAlignLyrics, msg.Params{"done": j + 1, "total": len(chunks)}, int64(j), int64(len(chunks)))
			from, to := min(len(voice), int(c.Start*alignRate)), min(len(voice), int(c.End*alignRate))
			if to-from < alignRate/10 {
				continue
			}
			// 只把有人声的部分接起来送去对齐：间奏和长停顿去掉后，字不会被对到没人唱的地方，音频也更短。
			pieces := clipSpans(spans, from, to)
			if spanLength(pieces, from, to) < alignRate/2 {
				pieces = [][2]int{{from, to}}
			}
			var joined []float32
			for _, p := range pieces {
				joined = append(joined, voice[p[0]:p[1]]...)
			}
			part := filepath.Join(dir, "part.wav")
			if e = audio.WriteMono(part, joined, alignRate); e != nil {
				return e
			}
			aligned, e := w.engine.AlignWords(ctx, *s.RuntimePath, model, s.Preferences.Backend, part, alignText(c.Text, language), language, func(code msg.Code, params msg.Params) {})
			if e != nil {
				return e
			}
			for _, a := range aligned {
				start, end := sourceTime(pieces, a.Start, false), sourceTime(pieces, max(a.Start, a.End), true)
				words = append(words, schema.LyricWord{Word: a.Word, Start: round3(start), End: round3(max(start, end))})
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

// 人声检测：20 ms 一帧，响度低于全曲较响部分 30 dB 的算空白；短于 voicedGap 的停顿并入演唱，
// 每段前后各留 voicedPad 余量，避免切掉字头和尾音。返回有人声的样本区间。
const voicedGap, voicedPad = .8, .3

func voicedSpans(samples []float32, rate int) [][2]int {
	frame := rate / 50
	if frame <= 0 || len(samples) < frame {
		return nil
	}
	levels := make([]float64, len(samples)/frame)
	for i := range levels {
		var sum float64
		for _, v := range samples[i*frame : (i+1)*frame] {
			sum += float64(v) * float64(v)
		}
		levels[i] = math.Sqrt(sum / float64(frame))
	}
	sorted := slices.Clone(levels)
	slices.Sort(sorted)
	threshold := max(sorted[len(sorted)*95/100]*.03, 1e-4)
	var spans [][2]int
	gap, pad := int(voicedGap*float64(rate)), int(voicedPad*float64(rate))
	for i := 0; i < len(levels); {
		if levels[i] < threshold {
			i++
			continue
		}
		j := i
		for j < len(levels) && levels[j] >= threshold {
			j++
		}
		start, end := max(0, i*frame-pad), min(len(samples), j*frame+pad)
		if n := len(spans); n > 0 && start-spans[n-1][1] < gap {
			spans[n-1][1] = end
		} else {
			spans = append(spans, [2]int{start, end})
		}
		i = j
	}
	return spans
}

// clipSpans 取出落在 [from, to) 里的人声区间。
func clipSpans(spans [][2]int, from, to int) [][2]int {
	var out [][2]int
	for _, s := range spans {
		if a, b := max(s[0], from), min(s[1], to); b > a {
			out = append(out, [2]int{a, b})
		}
	}
	return out
}

// spanLength 是 [from, to) 里人声区间的总样本数。
func spanLength(spans [][2]int, from, to int) int {
	n := 0
	for _, s := range clipSpans(spans, from, to) {
		n += s[1] - s[0]
	}
	return n
}

// sourceTime 把拼接后音频里的时间（秒）换回原曲时间。正好落在两段交界时，开始时间算后一段，结束时间算前一段。
func sourceTime(pieces [][2]int, seconds float64, end bool) float64 {
	at := int(math.Round(seconds * alignRate))
	for _, p := range pieces {
		n := p[1] - p[0]
		if at < n || (end && at == n) {
			return float64(p[0]+max(0, at)) / alignRate
		}
		at -= n
	}
	last := pieces[len(pieces)-1]
	return float64(last[1]) / alignRate
}

// alignText 整理送去对齐的歌词。对齐模型除中文外按空格分词，日文歌词通常不带空格，整句会被当成一个词，
// 所以日文在每个假名和汉字之间加空格，逐字给出时间。
func alignText(text, language string) string {
	if language != "Japanese" {
		return text
	}
	var b strings.Builder
	for _, r := range text {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			b.WriteRune(' ')
			b.WriteRune(r)
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
