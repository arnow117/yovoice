package score

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const sampleABC = `X:1
T:
M:4/4
L:1/32
Q:1/4=90
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Ins Melody" snm="Inst."
K:D
% intro
V: Vocal
"D"z32|"A"z32|
V: Ins
d8f8a16|a8f8e16|
% verse
V: Vocal
"D"D8F8A8F8|"G"G8B8d16-|"G"d16"A"c16|"D"d32|
V: Ins
Z4|
`

func TestParseABC(t *testing.T) {
	s, e := ParseABC(sampleABC)
	if e != nil {
		t.Fatal(e)
	}
	if s.Tempo != 90 || s.Key != "D" || s.Bars() != 6 || !s.HasChords() {
		t.Fatalf("unexpected header: %+v bars=%d", s, s.Bars())
	}
	if got := s.Seconds(); got != 16 {
		t.Fatalf("seconds = %v", got)
	}
	vocal := s.Vocal().Notes
	// K:D 下 F 为升 F；跨小节的 d16- 与 d16 合并为一个音。
	if len(vocal) != 9 || vocal[1].Pitch != 66 || vocal[6].Pitch != 74 || vocal[6].Length != ABCTicksPerWhole {
		t.Fatalf("vocal notes = %+v", vocal)
	}
	if vocal[7].Pitch != 73 { // c 在 D 大调中为升 C
		t.Fatalf("c# = %d", vocal[7].Pitch)
	}
	if !slices.Equal(s.Sections, []ABCSection{{"intro", 1}, {"verse", 3}}) {
		t.Fatalf("sections = %+v", s.Sections)
	}
	score := s.Score()
	ids := []string{}
	for _, track := range score.Tracks {
		ids = append(ids, track.ID)
	}
	if !slices.Equal(ids, []string{"vocal", "ins", "chords", "bass"}) || score.Key != "D major" || len(score.Sections) != 2 || score.Sections[1].End != 6 {
		t.Fatalf("score = %v %+v", ids, score.Sections)
	}
	// 跨两个小节的和弦按小节切开。
	if bass := score.Tracks[3].Notes; len(bass) != 7 || bass[0].Pitch != 38 {
		t.Fatalf("bass = %+v", bass)
	}
}

func TestParseABCAccidentalsAndTies(t *testing.T) {
	// 临时记号按字母跨八度生效到小节末；跨小节线接续的音不写记号时保持升号，之后的音回到调号。
	text := strings.Replace(sampleABC, `"D"D8F8A8F8|"G"G8B8d16-|"G"d16"A"c16|"D"d32|`, `^C8c8C8z8|^F32-|F8F24|z32|`, 1)
	s, e := ParseABC(strings.Replace(text, "K:D", "K:C", 1))
	if e != nil {
		t.Fatal(e)
	}
	got := []int{}
	for _, n := range s.Vocal().Notes {
		got = append(got, n.Pitch)
	}
	if !slices.Equal(got, []int{61, 73, 61, 66, 65}) {
		t.Fatalf("pitches = %v", got)
	}
}

func TestParseABCIssues(t *testing.T) {
	cases := []struct {
		name, from, to, code string
		bar                  int
		voice                string
	}{
		{"short bar", `"D"d32|`, `"D"d24|`, "duration", 6, "Vocal"},
		{"long bar", `"D"d32|`, `"D"d32z8|`, "overflow", 6, "Vocal"},
		{"bad length", `"D"d32|`, `"D"d20z12|`, "duration", 6, "Vocal"},
		{"chord", `"D"d32|`, `"Dmaj9"d32|`, "chord", 6, "Vocal"},
		{"triplet", `"D"d32|`, `(3ddd z8|`, "token", 6, "Vocal"},
		{"tie into rest", `"G"d16"A"c16|`, `z32|`, "tie", 5, "Vocal"},
		{"group count", "Z4|", "Z3|", "group", 0, ""},
		{"header", "Q:1/4=90", "Q:90", "tempo", 0, ""},
	}
	for _, c := range cases {
		_, e := ParseABC(strings.Replace(sampleABC, c.from, c.to, 1))
		var got *ABCIssue
		if !errors.As(e, &got) || got.Code != c.code || (c.bar > 0 && got.Bar != c.bar) || got.Voice != c.voice {
			t.Errorf("%s: %#v", c.name, got)
		}
	}
}

func TestStripChordsAndTempo(t *testing.T) {
	text, e := StripABCChords(sampleABC)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(text, `"D"`) || !strings.Contains(text, `name="Vocal Melody"`) {
		t.Fatalf("strip = %s", text)
	}
	s, _ := ParseABC(text)
	if s.HasChords() {
		t.Fatal("chords remain")
	}
	slower, e := SetABCTempo(text, 60)
	if e != nil {
		t.Fatal(e)
	}
	if s, _ = ParseABC(slower); s.Tempo != 60 || s.Seconds() != 24 {
		t.Fatalf("tempo = %d", s.Tempo)
	}
	if _, e = SetABCTempo(text, 10); e == nil {
		t.Fatal("tempo out of range accepted")
	}
}

func TestCompareABC(t *testing.T) {
	before, _ := ParseABC(sampleABC)
	// 第 4 小节把连音写法换成两个连起来的八分音符：发声不变。
	same, e := ParseABC(strings.Replace(sampleABC, `"G"G8B8d16-|`, `"G"G8B8d8-d8-|`, 1))
	if e != nil {
		t.Fatal(e)
	}
	if c := CompareABC(before, same); len(c.Vocal)+len(c.Ins)+len(c.Chords) != 0 || c.Tempo {
		t.Fatalf("unexpected changes %+v", c)
	}
	after, _ := ParseABC(strings.Replace(strings.Replace(sampleABC, `"D"D8F8A8F8|`, `"Bm"D8F8A8B8|`, 1), "Q:1/4=90", "Q:1/4=100", 1))
	c := CompareABC(before, after)
	if !c.Tempo || !slices.Equal(c.Vocal, []int{3}) || !slices.Equal(c.Chords, []int{3}) || len(c.Ins) != 0 {
		t.Fatalf("changes = %+v", c)
	}
}
