// Package testkit 提供多个包共用的测试辅助，仅由测试引用，不进入发布程序。
package testkit

import (
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func Must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// FailSettingsUpdate 在隔离 SQLite 库安装 trigger，拒绝 JSON 载荷包含指定状态的
// settings 更新。它覆盖 Store 的真实事务写路径，不改变生产备份语义。
func FailSettingsUpdate(t testing.TB, root, key, status, marker string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(root, "yovoice.db"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(abs)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	quote := func(value string) string {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	pattern := "%\"status\":\"" + status + "\"%"
	trigger := "CREATE TRIGGER test_fail_settings_update " +
		"BEFORE UPDATE ON settings " +
		"WHEN NEW.key = " + quote(key) + " AND NEW.data LIKE " + quote(pattern) + " " +
		"BEGIN SELECT RAISE(ABORT, " + quote(marker) + "); END"
	if _, err = db.Exec(trigger); err != nil {
		t.Fatal(err)
	}
}

// WAV 返回一秒 16 kHz 单声道静音。
func WAV() []byte {
	b := make([]byte, 44+32000)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 32000)
	return b
}

// VocalWAV 是模拟分离出的人声：4 秒 16 kHz 单声道，0.5–1 秒和 2.5–3 秒各有一段正弦音，其余静音。
func VocalWAV() []byte {
	const rate = 16000
	b := make([]byte, 44+4*rate*2)
	copy(b, WAV()[:44])
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	for i := 0; i < 4*rate; i++ {
		if t := float64(i) / rate; (t >= .5 && t < 1) || (t >= 2.5 && t < 3) {
			binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(8000*math.Sin(2*math.Pi*220*t))))
		}
	}
	return b
}

// RepoFile 定位仓库内文件，跨端一致性检查不依赖测试的工作目录。
func RepoFile(t testing.TB, parts ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(append([]string{filepath.Dir(file), "..", ".."}, parts...)...)
}

// RunFakeEngine 在测试二进制被当作推理引擎启动时提供模拟服务，并在服务结束后退出进程。
// 正文为“等待取消”时挂起至请求取消；以“模拟生成失败”结尾时返回错误。
func RunFakeEngine() {
	if len(os.Args) <= 2 || os.Args[1] != "--config" {
		return
	}
	b, e := os.ReadFile(os.Args[2])
	if e != nil {
		os.Exit(2)
	}
	var config struct {
		Port   int `json:"port"`
		Models []struct {
			Task    string         `json:"task"`
			Session map[string]any `json:"session_options"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &config) != nil {
		os.Exit(2)
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{}")) })
	http.HandleFunc("/v1/tasks/run", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Request map[string]any `json:"request"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		text, _ := p.Request["text"].(string)
		if strings.HasSuffix(text, "私有标记失败") {
			const marker = "/private/speaker-profile/marker"
			if markerFile := os.Getenv("YOVOICE_TEST_MARKER_FILE"); markerFile != "" {
				if err := os.WriteFile(markerFile, []byte(marker), 0600); err != nil {
					http.Error(w, "marker proof write failed", http.StatusInternalServerError)
					return
				}
			}
			_, _ = fmt.Fprintln(os.Stdout, marker)
			_, _ = fmt.Fprintln(os.Stderr, marker)
			http.Error(w, marker, http.StatusInternalServerError)
			return
		}
		if expectedText := os.Getenv("YOVOICE_TEST_EXPECTED_TEXT"); expectedText != "" && text != expectedText {
			http.Error(w, "unexpected profile text", http.StatusBadRequest)
			return
		}
		if text == "等待取消" {
			<-r.Context().Done()
			return
		}
		if strings.HasSuffix(text, "模拟生成失败") {
			http.Error(w, "模拟生成失败", http.StatusInternalServerError)
			return
		}
		if text == "返回损坏响应" {
			_, _ = w.Write([]byte(`{"audio":`))
			return
		}
		if text == "检查Vox绑定" {
			voice, voiceOK := p.Request["voice_ref"].(string)
			referenceText, referenceTextOK := p.Request["reference_text"].(string)
			if !voiceOK || voice == "" || voice != os.Getenv("YOVOICE_TEST_EXPECTED_VOICE_REF") || !referenceTextOK || referenceText != "绑定原文" {
				http.Error(w, "unexpected Vox profile request", http.StatusBadRequest)
				return
			}
		}
		if text == "检查Index绑定" {
			voice, voiceOK := p.Request["voice_ref"].(string)
			options, optionsOK := p.Request["options"].(map[string]any)
			if !voiceOK || voice == "" || voice != os.Getenv("YOVOICE_TEST_EXPECTED_VOICE_REF") || p.Request["reference_text"] != nil || !optionsOK || options["emotion_text"] != "调用方情绪" {
				http.Error(w, "unexpected Index profile request", http.StatusBadRequest)
				return
			}
		}
		options, _ := p.Request["options"].(map[string]any)
		language, _ := p.Request["language"].(string)
		// 歌词对齐：分离任务返回人声分轨；对齐任务把每个非空白字符记作一个字，每字 0.25 秒（16 kHz 样本数）。
		switch config.Models[0].Task {
		case "sep":
			if config.Models[0].Session["bs_roformer.num_overlap"] != "1" {
				http.Error(w, "分离应单遍推理", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"named_audio_outputs": []any{map[string]any{"id": "vocals", "audio": base64.StdEncoding.EncodeToString(VocalWAV())}}})
			return
		case "align":
			words := []any{}
			for _, r := range strings.Join(strings.Fields(text), "") {
				n := len(words)
				words = append(words, map[string]any{"word": string(r), "start_sample": n * 4000, "end_sample": n*4000 + 3000, "confidence": 1})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"text": text, "language": language, "words": words})
			return
		}
		result := map[string]any{}
		// YuE2 自己规划（cot 非 off 且未给谱）和 SheetSage2 扒谱会附带旋律谱。
		// VoxCPM2 continuation 也带 audio 作为提示，但仍是 TTS，必须返回 WAV。
		_, given := options["abc"]
		scoreOnly := config.Models[0].Task == "midi"
		plansScore := config.Models[0].Task == "gen" && options["cot"] != nil && options["cot"] != "off" && !given
		if scoreOnly || plansScore {
			result["artifacts"] = []any{map[string]any{"id": "score", "kind": "custom", "payload": base64.StdEncoding.EncodeToString([]byte(ABC)), "meta": map[string]string{"format": "abc", "truncated": "false"}}}
		}
		if !scoreOnly && options["stop_after"] != "abc" {
			result["audio"] = base64.StdEncoding.EncodeToString(WAV())
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	_ = http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", config.Port), nil)
	os.Exit(0)
}

// ABC 是模拟引擎返回的 YuE2 旋律谱：两小节，带和弦与段落。
const ABC = `X:1
T:
M:4/4
L:1/32
Q:1/4=120
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Ins Melody" snm="Inst."
K:C
% verse
V: Vocal
"C"E8G8c16|"G"B8G8D16|
V: Ins
Z2|
`
