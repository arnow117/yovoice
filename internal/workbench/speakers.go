package workbench

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"yovoice/internal/audio"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/recognition"
	"yovoice/internal/schema"
)

// SpeakerSource 是登记或识别的本地文件或已保存声音。远程调用者只能使用 VoiceID。
type SpeakerSource struct {
	FilePath string
	VoiceID  string
}

var speakerConverter = audio.Converter

func (w *Workbench) speakerConfigSnapshot() (recognition.Config, error) {
	w.speakerConfigOnce.Do(func() {
		w.speakerConfig, w.speakerConfigErr = recognition.LoadConfig(os.Getenv)
	})
	return w.speakerConfig, w.speakerConfigErr
}

func (w *Workbench) speakerRepository() recognition.Repository {
	return recognition.NewRepository(w.Store.Root)
}

func (w *Workbench) ListSpeakers() ([]recognition.PublicProfile, error) {
	profiles, err := w.speakerRepository().Load()
	if err != nil {
		return nil, err
	}
	result := make([]recognition.PublicProfile, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, profile.Public())
	}
	return result, nil
}

// DeleteSpeaker 是幂等删除；声纹从私有文件被完全重写，不通过 state 备份恢复。
func (w *Workbench) DeleteSpeaker(ctx context.Context, id string) error {
	if !schema.ValidID(id) {
		return recognitionError("invalid_input")
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	profiles, err := w.speakerRepository().Load()
	if err != nil {
		return err
	}
	profiles = slices.DeleteFunc(profiles, func(profile recognition.Profile) bool { return profile.ID == id })
	return w.speakerRepository().Save(ctx, profiles)
}

func (w *Workbench) EnrollSpeaker(ctx context.Context, source SpeakerSource, name, bindVoiceID string) (recognition.PublicProfile, error) {
	return w.enrollSpeaker(ctx, source, name, false, bindVoiceID)
}

// EnrollSpeakerNamed 让 CLI/HTTP 保留名称是否被显式提供；省略名称才会默认使用档案 ID。
func (w *Workbench) EnrollSpeakerNamed(ctx context.Context, source SpeakerSource, name string, nameProvided bool, bindVoiceID string) (recognition.PublicProfile, error) {
	return w.enrollSpeaker(ctx, source, name, nameProvided, bindVoiceID)
}

func (w *Workbench) enrollSpeaker(ctx context.Context, source SpeakerSource, name string, nameProvided bool, bindVoiceID string) (recognition.PublicProfile, error) {
	if err := ctx.Err(); err != nil {
		return recognition.PublicProfile{}, err
	}
	if nameProvided && strings.TrimSpace(name) == "" {
		return recognition.PublicProfile{}, recognitionError("invalid_input")
	}
	config, err := w.speakerConfigSnapshot()
	if err != nil {
		return recognition.PublicProfile{}, err
	}
	input, cleanup, err := w.copySpeakerInput(source, bindVoiceID)
	if err != nil {
		return recognition.PublicProfile{}, err
	}
	defer cleanup()
	var result recognition.PublicProfile
	op, err := w.startSpeakerOperation(ctx, config.Timeout, func(operationCtx context.Context) error {
		embedding, extractErr := w.extractSpeakerInline(operationCtx, config, input)
		if extractErr != nil {
			return extractErr
		}
		profile, profileErr := recognition.NewProfile(schema.NewID(), name, bindVoiceID, config, embedding)
		if profileErr != nil {
			return profileErr
		}
		w.edit.Lock()
		defer w.edit.Unlock()
		if bindVoiceID != "" && !w.hasVoice(bindVoiceID) {
			return recognitionError("voice_missing")
		}
		profiles, loadErr := w.speakerRepository().Load()
		if loadErr != nil {
			return loadErr
		}
		if compatibilityErr := compatibleSpeakerProfiles(profiles, profile, config); compatibilityErr != nil {
			return compatibilityErr
		}
		if w.beforeSpeakerCommit != nil {
			w.beforeSpeakerCommit()
		}
		if saveErr := w.speakerRepository().Save(operationCtx, append(profiles, profile)); saveErr != nil {
			return saveErr
		}
		result = profile.Public()
		return nil
	})
	if err != nil {
		return recognition.PublicProfile{}, err
	}
	if err = waitSpeakerOperation(ctx, op); err != nil {
		return recognition.PublicProfile{}, err
	}
	return result, nil
}

func compatibleSpeakerProfiles(existing []recognition.Profile, next recognition.Profile, config recognition.Config) error {
	for _, profile := range existing {
		if profile.ID == next.ID {
			return recognitionError("persistence_failed")
		}
		if profile.ModelSHA256 != config.ModelSHA256 || profile.FeatureVersion != config.FeatureVersion || profile.Dimension != recognition.Dimension {
			return recognitionError("feature_mismatch")
		}
	}
	return nil
}

func (w *Workbench) EnrollSpeakerFrom(ctx context.Context, reader io.Reader, name, bindVoiceID string) (recognition.PublicProfile, error) {
	path, err := w.receive(reader)
	if err != nil {
		return recognition.PublicProfile{}, err
	}
	defer os.Remove(path)
	return w.EnrollSpeaker(ctx, SpeakerSource{FilePath: path}, name, bindVoiceID)
}

func (w *Workbench) IdentifySpeaker(ctx context.Context, source SpeakerSource) (recognition.Identification, error) {
	if err := ctx.Err(); err != nil {
		return recognition.Identification{}, err
	}
	config, err := w.speakerConfigSnapshot()
	if err != nil {
		return recognition.Identification{}, err
	}
	input, cleanup, err := w.copySpeakerInput(source, "")
	if err != nil {
		return recognition.Identification{}, err
	}
	defer cleanup()
	profiles, err := w.speakerRepository().Load()
	if err != nil {
		return recognition.Identification{}, err
	}
	embedding, err := w.extractSpeaker(ctx, config, input)
	if err != nil {
		return recognition.Identification{}, err
	}
	return recognition.Identify(config, profiles, embedding)
}

func (w *Workbench) IdentifySpeakerFrom(ctx context.Context, reader io.Reader) (recognition.Identification, error) {
	path, err := w.receive(reader)
	if err != nil {
		return recognition.Identification{}, err
	}
	defer os.Remove(path)
	return w.IdentifySpeaker(ctx, SpeakerSource{FilePath: path})
}

// SynthesizeSpeaker 让 profile 只提供已绑定声音与其参考原文，调用方的正文与情绪仍由调用方控制。
func (w *Workbench) SynthesizeSpeaker(ctx context.Context, profileID string, d schema.Draft) (schema.Generation, error) {
	if err := ctx.Err(); err != nil {
		return schema.Generation{}, err
	}
	if !schema.ValidID(profileID) {
		return schema.Generation{}, recognitionError("invalid_input")
	}
	if d.VoiceID != nil || d.ReferenceText != "" {
		return schema.Generation{}, recognitionError("invalid_input")
	}
	w.edit.Lock()
	profiles, err := w.speakerRepository().Load()
	if err != nil {
		w.edit.Unlock()
		return schema.Generation{}, err
	}
	profile, found := recognition.Find(profiles, profileID)
	if !found {
		w.edit.Unlock()
		return schema.Generation{}, recognitionError("profile")
	}
	if profile.BindVoiceID == "" {
		w.edit.Unlock()
		return schema.Generation{}, recognitionError("unsupported_clone")
	}
	state := w.Store.Read()
	voiceIndex := slices.IndexFunc(state.Voices, func(voice schema.Voice) bool { return voice.ID == profile.BindVoiceID })
	if voiceIndex < 0 {
		w.edit.Unlock()
		return schema.Generation{}, recognitionError("voice_missing")
	}
	if err = applySpeakerBinding(&d, state.Voices[voiceIndex]); err != nil {
		w.edit.Unlock()
		return schema.Generation{}, err
	}
	var generation schema.Generation
	op, err := w.startGenerationLocked(d, "", "", "", false, true, &generation)
	w.edit.Unlock()
	if err != nil {
		return schema.Generation{}, err
	}
	select {
	case <-op.done:
	case <-ctx.Done():
		callerErr := ctx.Err()
		op.cancel()
		<-op.done
		if err = op.Err(); err != nil {
			if errors.Is(err, context.Canceled) && errors.Is(callerErr, context.DeadlineExceeded) {
				return schema.Generation{}, context.DeadlineExceeded
			}
			return schema.Generation{}, err
		}
	}
	if err = op.Err(); err != nil {
		return schema.Generation{}, err
	}
	if generation.ID == "" {
		return schema.Generation{}, recognitionError("persistence_failed")
	}
	if w.beforeSpeakerResultReturn != nil {
		w.beforeSpeakerResultReturn()
	}
	return generation, nil
}

func applySpeakerBinding(d *schema.Draft, voice schema.Voice) error {
	model, err := catalog.Lookup(d.ModelID)
	if err != nil {
		return recognitionError("invalid_input")
	}
	switch model.Family {
	case "index_tts2":
	case "voxcpm2":
		if d.VoxMode == "design" {
			return recognitionError("unsupported_clone")
		}
		if d.VoxMode == "" {
			if voice.ReferenceText == "" {
				d.VoxMode = "clone"
			} else {
				d.VoxMode = "continuation"
			}
		}
		if d.VoxMode == "clone" && voice.ReferenceText != "" {
			return recognitionError("unsupported_clone")
		}
	case "omnivoice":
		if d.VoiceMode == "design" {
			return recognitionError("unsupported_clone")
		}
		d.VoiceMode = "clone"
	case "qwen3_tts":
		if model.Variant != "" && model.Variant != "base" {
			return recognitionError("unsupported_clone")
		}
	default:
		return recognitionError("unsupported_clone")
	}
	d.VoiceID = &voice.ID
	d.ReferenceText = voice.ReferenceText
	if err = schema.Validate(*d); err != nil {
		return recognitionError("invalid_input")
	}
	return nil
}

func (w *Workbench) copySpeakerInput(source SpeakerSource, bindVoiceID string) (string, func(), error) {
	if (source.FilePath == "") == (source.VoiceID == "") {
		return "", nil, recognitionError("invalid_input")
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	if bindVoiceID != "" && !w.hasVoice(bindVoiceID) {
		return "", nil, recognitionError("voice_missing")
	}
	input := source.FilePath
	if source.VoiceID != "" {
		var err error
		input, err = w.MediaFile("voices", source.VoiceID)
		if err != nil {
			return "", nil, recognitionError("voice_missing")
		}
	}
	info, err := os.Stat(input)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, recognitionError("audio")
	}
	if info.Size() > maxUpload {
		return "", nil, recognitionError("input_too_large")
	}
	directory, err := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "recognition-")
	if err != nil {
		return "", nil, recognitionError("persistence_failed")
	}
	output := filepath.Join(directory, "source"+filepath.Ext(input))
	if err = copySpeakerFile(input, output); err != nil {
		_ = os.RemoveAll(directory)
		var callError *msg.CallError
		if errors.As(err, &callError) && callError.Code == msg.ErrAudioTooLarge {
			return "", nil, recognitionError("input_too_large")
		}
		return "", nil, recognitionError("audio")
	}
	return output, func() { _ = os.RemoveAll(directory) }, nil
}

func copySpeakerFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	written, err := io.Copy(output, io.LimitReader(input, maxUpload+1))
	if err == nil && written > maxUpload {
		err = msg.Err(msg.ErrAudioTooLarge, nil)
	}
	if chmodErr := output.Chmod(0600); err == nil {
		err = chmodErr
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(destination)
	}
	return err
}

func (w *Workbench) extractSpeaker(ctx context.Context, config recognition.Config, input string) ([]float64, error) {
	var embedding []float64
	op, err := w.startSpeakerOperation(ctx, config.Timeout, func(operationCtx context.Context) error {
		var extractErr error
		embedding, extractErr = w.extractSpeakerInline(operationCtx, config, input)
		return extractErr
	})
	if err != nil {
		return nil, err
	}
	if err = waitSpeakerOperation(ctx, op); err != nil {
		return nil, err
	}
	return embedding, nil
}

func (w *Workbench) extractSpeakerInline(ctx context.Context, config recognition.Config, input string) ([]float64, error) {
	executable, err := speakerConverter()
	if err != nil {
		return nil, err
	}
	output := filepath.Join(filepath.Dir(input), "recognition.wav")
	if err = audio.ConvertRecognition(ctx, executable, input, output); err != nil {
		return nil, err
	}
	if _, _, err = audio.RecognitionWAV(output); err != nil {
		return nil, err
	}
	return recognition.Extract(ctx, config, output)
}

func waitSpeakerOperation(ctx context.Context, op *operation) error {
	select {
	case <-op.done:
	case <-ctx.Done():
		op.cancel()
		<-op.done
		if err := op.Err(); err != nil {
			return err
		}
		return nil
	}
	return op.Err()
}

func (w *Workbench) startSpeakerOperation(parent context.Context, timeout time.Duration, action func(context.Context) error) (*operation, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.cancel != nil {
		return nil, msg.Err(msg.ErrBusy, nil)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	op := &operation{cancel: cancel, done: make(chan struct{})}
	w.cancel, w.done, w.active = cancel, op.done, op
	go func() {
		defer close(op.done)
		defer cancel()
		err := action(ctx)
		op.finish(err)
		w.mu.Lock()
		if w.active == op {
			w.cancel = nil
			w.active = nil
		}
		w.mu.Unlock()
	}()
	return op, nil
}

func (w *Workbench) hasVoice(id string) bool {
	return slices.ContainsFunc(w.Store.Read().Voices, func(voice schema.Voice) bool { return voice.ID == id })
}

func recognitionError(code string) error {
	return &recognition.Error{Code: code}
}

func SpeakerErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var callError *msg.CallError
	if errors.As(err, &callError) {
		switch callError.Code {
		case msg.ErrBusy:
			return "busy"
		case msg.ErrAudioTooLarge:
			return "input_too_large"
		case msg.ErrAudioDecode, msg.ErrAudioDuration, msg.ErrAudioFormat, msg.ErrAudioConverterMissing:
			return "audio"
		case msg.ErrGenerateFailed:
			return "runtime_failed"
		case msg.ErrStateSaveFailed:
			return "persistence_failed"
		}
	}
	return recognition.Code(err)
}

func SpeakerErrorMessage(err error) string {
	code := SpeakerErrorCode(err)
	if code == "" {
		return ""
	}
	return strings.ReplaceAll(code, "_", " ")
}
