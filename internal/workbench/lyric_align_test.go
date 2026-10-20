package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// alignWorkbench 准备一个带歌词和音频的生成版本；installed 决定是否装好可选的歌词对齐模型。
func alignWorkbench(t *testing.T, installed bool) (*Workbench, string) {
	wb, d := yueWorkbench(t)
	d.Lyrics = "[verse]\n晚风 轻\n[chorus]\n夏夜"
	if installed {
		model := filepath.Join(wb.Store.Root, "models", "Lyric-Align-GGUF")
		for _, file := range []string{"BS-RoFormer-ep368-GGUF/bs-roformer-ep368-q8_0.gguf", "Qwen3-ForcedAligner-0.6B-GGUF/qwen3-forced-aligner-0.6b-q8_0.gguf"} {
			must(t, os.MkdirAll(filepath.Dir(filepath.Join(model, file)), 0700))
			must(t, os.WriteFile(filepath.Join(model, file), nil, 0600))
		}
		must(t, wb.Store.Update(func(s *schema.State) {
			s.Models = append(s.Models, schema.InstalledModel{ID: "lyric-align-q8", Path: model})
		}, true))
	}
	must(t, os.MkdirAll(filepath.Join(wb.Store.Root, "outputs"), 0700))
	must(t, audio.WriteMono(filepath.Join(wb.Store.Root, "outputs", "song.wav"), make([]float32, 3*44100), 44100))
	id := "0123456789abcdef0123456789abcdef"
	must(t, wb.Store.Update(func(s *schema.State) {
		s.History = []schema.Generation{{ID: id, FileName: "song.wav", Duration: 3, Settings: d, LyricTimes: []float64{0, 1}}}
	}, true))
	return wb, id
}

// 没装对齐模型时直接提示下载，不启动任务；对齐是可选能力。
func TestAlignLyricsNeedsModel(t *testing.T) {
	wb, id := alignWorkbench(t, false)
	if e := wb.AlignLyrics(id, nil); !isCode(e, msg.ErrAlignModelRequired) {
		t.Fatalf("%v", e)
	}
}

// 先分离人声再对齐：逐字结果写进版本，并替换掉旧的手动校准。
func TestAlignLyrics(t *testing.T) {
	wb, id := alignWorkbench(t, true)
	must(t, wb.AlignLyrics(id, nil))
	<-wb.Done()
	s := wb.Store.Read()
	if s.Activity.Status != "completed" || s.Activity.Task != "align" {
		t.Fatalf("%+v %v %v", s.Activity, *s.Activity.ErrorCode, s.Activity.ErrorParams)
	}
	g := s.History[0]
	if g.LyricTimes != nil || len(g.LyricWords) != 5 || g.LyricWords[0].Word != "晚" || g.LyricWords[1].Start != 0.25 || g.LyricWords[4].End != 1.188 {
		t.Fatalf("%+v %+v", g.LyricTimes, g.LyricWords)
	}
	// 分段对齐时结果按段起点平移（测试引擎分出的人声只有一秒）。
	must(t, wb.AlignLyrics(id, []LyricChunk{{Text: "晚风 轻", Start: 0, End: 0.5}, {Text: "夏夜", Start: 0.5, End: 1}}))
	<-wb.Done()
	if w := wb.Store.Read().History[0].LyricWords; len(w) != 5 || w[3].Word != "夏" || w[3].Start != 0.5 {
		t.Fatalf("%+v", w)
	}
	// 手动校准反过来清掉对齐结果。
	must(t, wb.SetLyricTimes(id, []float64{0, 2}))
	if g := wb.Store.Read().History[0]; g.LyricWords != nil {
		t.Fatalf("%+v", g.LyricWords)
	}
	if e := wb.AlignLyrics(id, []LyricChunk{{Text: "夏夜", Start: 2, End: 1}}); !isCode(e, msg.ErrRequestInvalid) {
		t.Fatalf("%v", e)
	}
}

func isCode(e error, code msg.Code) bool {
	c, ok := e.(*msg.CallError)
	return ok && c.Code == code
}

func TestAlignLanguage(t *testing.T) {
	for text, want := range map[string]string{"夏夜晚风": "Chinese", "さくら 桜": "Japanese", "사랑해": "Korean", "summer night": "English", "белая ночь": "Russian"} {
		if got := alignLanguage(text); got != want {
			t.Fatalf("%s: %s", text, got)
		}
	}
}
