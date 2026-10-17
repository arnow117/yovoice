package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yovoice/internal/engine"
	"yovoice/internal/schema"
)

// 声纹页把 music/score 草稿转换为独立 TTS 草稿后，桌面 DTO 仍须能通过
// 同一份 schema 校验。这个检查不启动识别或 TTS 模型。
func TestSpeakerGenerateRequestAcceptsCompletedIndexTTSSettings(t *testing.T) {
	draft := schema.DefaultDraft()
	draft.ID = strings.Repeat("d", 32)
	draft.ModelID = "index-2.5-q8"
	draft.Text = "speaker synthesis text"
	draft.Language = "ja"
	draft.VoiceID = nil
	draft.ReferenceText = ""

	raw, err := json.Marshal(speakerGenerateRequest{
		RequestID: testSpeakerRequestID,
		ProfileID: strings.Repeat("a", 32),
		Draft:     draft,
	})
	if err != nil {
		t.Fatal(err)
	}

	var decoded speakerGenerateRequest
	if err = strictJSON(raw, &decoded); err != nil {
		t.Fatalf("speaker.generate DTO = %v", err)
	}
	if err = schema.Validate(decoded.Draft); err != nil {
		t.Fatalf("speaker.generate draft = %v", err)
	}
	payload, err := engine.BuildRequest(decoded.Draft, "bound.wav", "")
	if err != nil {
		t.Fatalf("Index TTS request = %v", err)
	}
	request := payload["request"].(map[string]any)
	options := request["options"].(map[string]any)
	if request["text"] != draft.Text || options["language"] != "ja" {
		t.Fatalf("Index TTS payload = %#v", request)
	}
}

// 音乐/编曲项目落盘时会保留 TTS 字段的 Go 零值。该 fixture 保留了原始
// 序列化源与页面实际发出的独立 TTS DTO，避免只以 DefaultDraft 自证。
func TestSpeakerGenerateRequestAcceptsSerializedScoreConversion(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "web", "e2e", "fixtures", "music-score-speaker-dto.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SerializedSource     json.RawMessage `json:"serializedSource"`
		SpeakerGenerateDraft schema.Draft    `json:"speakerGenerateDraft"`
	}
	if err = json.Unmarshal(contents, &fixture); err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err = json.Unmarshal(fixture.SerializedSource, &source); err != nil {
		t.Fatal(err)
	}
	if source["mode"] != "" || source["speed"] != float64(0) || source["emotions"] != nil {
		t.Fatalf("fixture must preserve serialized music/score zero values: %#v", source)
	}

	raw, err := json.Marshal(speakerGenerateRequest{
		RequestID: testSpeakerRequestID,
		ProfileID: strings.Repeat("a", 32),
		Draft:     fixture.SpeakerGenerateDraft,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded speakerGenerateRequest
	if err = strictJSON(raw, &decoded); err != nil {
		t.Fatalf("speaker.generate converted DTO = %v", err)
	}
	if err = schema.Validate(decoded.Draft); err != nil {
		t.Fatalf("speaker.generate converted draft = %v", err)
	}
	payload, err := engine.BuildRequest(decoded.Draft, "bound.wav", "")
	if err != nil {
		t.Fatalf("converted Index TTS request = %v", err)
	}
	request := payload["request"].(map[string]any)
	options := request["options"].(map[string]any)
	if request["text"] != fixture.SpeakerGenerateDraft.Text || options["language"] != "zh" || options["duration_factor"] != float64(1) || options["emotion_alpha"] != float64(0) {
		t.Fatalf("converted Index payload = %#v", request)
	}
}
