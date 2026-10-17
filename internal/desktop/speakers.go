package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sync"

	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

const maxSpeakerInputBytes = 20 << 20

var speakerRequestIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type speakerCancelContextKey struct{}

// speakerOperationRegistry 只记录当前桌面请求自己的取消函数。它不参与 Workbench 的执行调度。
type speakerOperationRegistry struct {
	mu      sync.Mutex
	entries map[string]*speakerOperation
}

type speakerOperation struct {
	cancel context.CancelFunc
}

func (r *speakerOperationRegistry) register(requestID string, cancel context.CancelFunc) *speakerOperation {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]*speakerOperation)
	}
	// 识别、登记和档案生成共用 Workbench 的全局昂贵操作槽。这里同样只保留
	// 一个服务端取消句柄，避免不同 requestId 绕过该槽而把取消目标指向错误调用。
	if _, exists := r.entries[requestID]; exists || len(r.entries) != 0 {
		return nil
	}
	entry := &speakerOperation{cancel: cancel}
	r.entries[requestID] = entry
	return entry
}

func (r *speakerOperationRegistry) finish(requestID string, entry *speakerOperation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[requestID] != entry {
		return
	}
	delete(r.entries, requestID)
}

func (r *speakerOperationRegistry) cancel(requestID string) bool {
	r.mu.Lock()
	entry := r.entries[requestID]
	r.mu.Unlock()
	if entry == nil {
		return false
	}
	entry.cancel()
	return true
}

type speakerSource struct {
	Base64  *string `json:"base64"`
	VoiceID *string `json:"voiceId"`
}

type speakerEnrollRequest struct {
	RequestID   string        `json:"requestId"`
	Source      speakerSource `json:"source"`
	Name        *string       `json:"name"`
	BindVoiceID string        `json:"bindVoiceId"`
}

type speakerIdentifyRequest struct {
	RequestID string        `json:"requestId"`
	Source    speakerSource `json:"source"`
}

type speakerGenerateRequest struct {
	RequestID string       `json:"requestId"`
	ProfileID string       `json:"profileId"`
	Draft     schema.Draft `json:"draft"`
}

type speakerDeleteRequest struct {
	ID string `json:"id"`
}

type speakerCancelRequest struct {
	RequestID string `json:"requestId"`
}

func isSpeakerMethod(method string) bool {
	switch method {
	case "speaker.runtime", "speaker.list", "speaker.enroll", "speaker.identify", "speaker.delete", "speaker.generate", "speaker.cancel":
		return true
	default:
		return false
	}
}

func speakerRequestID(method string, data json.RawMessage) (string, bool) {
	if method != "speaker.enroll" && method != "speaker.identify" && method != "speaker.generate" {
		return "", false
	}
	var request struct {
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(data, &request) != nil || !speakerRequestIDPattern.MatchString(request.RequestID) {
		return "", false
	}
	return request.RequestID, true
}

func callSpeaker(ctx context.Context, w *workbench.Workbench, method string, data json.RawMessage) (any, error) {
	switch method {
	case "speaker.runtime":
		if err := strictJSON(data, &struct{}{}); err != nil {
			return nil, err
		}
		return w.SpeakerRuntime(), nil
	case "speaker.list":
		if err := strictJSON(data, &struct{}{}); err != nil {
			return nil, err
		}
		return w.ListSpeakers()
	case "speaker.enroll":
		var request speakerEnrollRequest
		if err := strictJSON(data, &request); err != nil {
			return nil, err
		}
		if !speakerRequestIDPattern.MatchString(request.RequestID) {
			return nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
		}
		source, reader, err := decodeSpeakerSource(request.Source)
		if err != nil {
			return nil, err
		}
		if source.VoiceID != "" {
			return w.EnrollSpeakerNamed(ctx, source, valueOf(request.Name), request.Name != nil, request.BindVoiceID)
		}
		return w.EnrollSpeakerFromNamed(ctx, reader, valueOf(request.Name), request.Name != nil, request.BindVoiceID)
	case "speaker.identify":
		var request speakerIdentifyRequest
		if err := strictJSON(data, &request); err != nil {
			return nil, err
		}
		if !speakerRequestIDPattern.MatchString(request.RequestID) {
			return nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
		}
		source, reader, err := decodeSpeakerSource(request.Source)
		if err != nil {
			return nil, err
		}
		if source.VoiceID != "" {
			return w.IdentifySpeaker(ctx, source)
		}
		return w.IdentifySpeakerFrom(ctx, reader)
	case "speaker.delete":
		var request speakerDeleteRequest
		if err := strictJSON(data, &request); err != nil {
			return nil, err
		}
		if err := w.DeleteSpeaker(ctx, request.ID); err != nil {
			return nil, err
		}
		return map[string]any{"id": request.ID, "deleted": true}, nil
	case "speaker.generate":
		var request speakerGenerateRequest
		if err := strictJSON(data, &request); err != nil {
			return nil, err
		}
		if !speakerRequestIDPattern.MatchString(request.RequestID) || request.Draft.VoiceID != nil || request.Draft.ReferenceText != "" {
			return nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
		}
		return w.SynthesizeSpeaker(ctx, request.ProfileID, request.Draft)
	case "speaker.cancel":
		var request speakerCancelRequest
		if err := strictJSON(data, &request); err != nil {
			return nil, err
		}
		if !speakerRequestIDPattern.MatchString(request.RequestID) {
			return nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
		}
		cancel, _ := ctx.Value(speakerCancelContextKey{}).(func(string) bool)
		if cancel == nil {
			return map[string]bool{"cancelRequested": false}, nil
		}
		return map[string]bool{"cancelRequested": cancel(request.RequestID)}, nil
	default:
		return nil, msg.Err(msg.ErrMethodUnsupported, nil)
	}
}

func strictJSON(data json.RawMessage, target any) error {
	if len(data) == 0 || string(data) == "null" {
		return msg.Err(msg.ErrSpeakerRequestInvalid, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || decoder.Decode(new(any)) != io.EOF {
		return msg.Err(msg.ErrSpeakerRequestInvalid, nil)
	}
	return nil
}

func decodeSpeakerSource(source speakerSource) (workbench.SpeakerSource, io.Reader, error) {
	if (source.Base64 == nil) == (source.VoiceID == nil) {
		return workbench.SpeakerSource{}, nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
	}
	if source.VoiceID != nil {
		if !schema.ValidID(*source.VoiceID) {
			return workbench.SpeakerSource{}, nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
		}
		return workbench.SpeakerSource{VoiceID: *source.VoiceID}, nil, nil
	}
	encodedLimit := base64.StdEncoding.EncodedLen(maxSpeakerInputBytes)
	if len(*source.Base64) > encodedLimit {
		return workbench.SpeakerSource{}, nil, msg.Err(msg.ErrSpeakerInputTooLarge, nil)
	}
	decoded, err := base64.StdEncoding.DecodeString(*source.Base64)
	if err != nil {
		return workbench.SpeakerSource{}, nil, msg.Err(msg.ErrSpeakerRequestInvalid, nil)
	}
	if len(decoded) > maxSpeakerInputBytes {
		return workbench.SpeakerSource{}, nil, msg.Err(msg.ErrSpeakerInputTooLarge, nil)
	}
	return workbench.SpeakerSource{}, bytes.NewReader(decoded), nil
}

func speakerCallError(err error) error {
	var callError *msg.CallError
	if errors.As(err, &callError) {
		if isSpeakerErrorCode(callError.Code) {
			return callError
		}
		return msg.Err(speakerCodeFor(callError.Code), nil)
	}
	return msg.Err(speakerCodeForRecognition(workbench.SpeakerErrorCode(err)), nil)
}

func isSpeakerErrorCode(code msg.Code) bool {
	switch code {
	case msg.ErrSpeakerRequestInvalid, msg.ErrSpeakerRuntime, msg.ErrSpeakerAudio, msg.ErrSpeakerInputTooLarge,
		msg.ErrSpeakerBusy, msg.ErrSpeakerCancelled, msg.ErrSpeakerTimeout, msg.ErrSpeakerProfile,
		msg.ErrSpeakerVoice, msg.ErrSpeakerClone, msg.ErrSpeakerStorage, msg.ErrSpeakerFeature:
		return true
	default:
		return false
	}
}

func speakerCodeFor(code msg.Code) msg.Code {
	switch code {
	case msg.ErrModelRequired, msg.ErrModelUnsupported:
		// 这两个生成前置条件已经是无参数、可恢复的公开消息码；保留其语义，
		// 让页面提示安装模型或选择兼容模型，而不是伪装成请求格式错误。
		return code
	case msg.ErrBusy:
		return msg.ErrSpeakerBusy
	case msg.ErrAudioTooLarge:
		return msg.ErrSpeakerInputTooLarge
	case msg.ErrAudioDecode, msg.ErrAudioDuration, msg.ErrAudioFormat, msg.ErrAudioConverterMissing:
		return msg.ErrSpeakerAudio
	case msg.ErrGenerateFailed, msg.ErrRuntimeMissing, msg.ErrRuntimeRequired, msg.ErrEngineStartFailed, msg.ErrEngineStartTimeout:
		return msg.ErrSpeakerRuntime
	case msg.ErrStateSaveFailed:
		return msg.ErrSpeakerStorage
	default:
		return msg.ErrSpeakerRequestInvalid
	}
}

func speakerCodeForRecognition(code string) msg.Code {
	switch code {
	case "cancelled":
		return msg.ErrSpeakerCancelled
	case "deadline":
		return msg.ErrSpeakerTimeout
	case "runtime_not_configured", "runtime_failed":
		return msg.ErrSpeakerRuntime
	case "audio":
		return msg.ErrSpeakerAudio
	case "input_too_large":
		return msg.ErrSpeakerInputTooLarge
	case "busy":
		return msg.ErrSpeakerBusy
	case "profile":
		return msg.ErrSpeakerProfile
	case "voice_missing":
		return msg.ErrSpeakerVoice
	case "unsupported_clone":
		return msg.ErrSpeakerClone
	case "feature_mismatch":
		return msg.ErrSpeakerFeature
	case "persistence_failed":
		return msg.ErrSpeakerStorage
	default:
		return msg.ErrSpeakerRequestInvalid
	}
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
