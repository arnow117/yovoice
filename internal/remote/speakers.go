package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

const speakerJSONLimit = 64 << 10

func (a *API) speakersHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/speakers")
	if (r.Method == http.MethodGet || r.Method == http.MethodDelete || strings.HasSuffix(path, "/generate")) && !validSpeakerQuery(r) {
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "":
		a.speakerList(w)
	case r.Method == http.MethodPost && path == "":
		a.withSpeakerBusy(w, r, a.speakerEnroll)
	case r.Method == http.MethodPost && path == "/identify":
		a.withSpeakerBusy(w, r, a.speakerIdentify)
	case r.Method == http.MethodDelete && validSpeakerPath(path):
		a.speakerDelete(w, r, strings.TrimPrefix(path, "/"))
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/generate") && validSpeakerPath(strings.TrimSuffix(path, "/generate")):
		a.withSpeakerBusy(w, r, a.speakerGenerate)
	default:
		http.NotFound(w, r)
	}
}

func validSpeakerPath(path string) bool {
	return strings.Count(path, "/") == 1 && schema.ValidID(strings.TrimPrefix(path, "/"))
}

func (a *API) withSpeakerBusy(w http.ResponseWriter, r *http.Request, action func(http.ResponseWriter, *http.Request)) {
	if !a.busy.TryLock() {
		speakerError(w, http.StatusConflict, "busy")
		return
	}
	defer a.busy.Unlock()
	action(w, r)
}

func (a *API) speakerList(w http.ResponseWriter) {
	profiles, err := a.Workbench.ListSpeakers()
	if err != nil {
		speakerWriteError(w, err)
		return
	}
	apiJSON(w, profiles)
}

func (a *API) speakerDelete(w http.ResponseWriter, r *http.Request, id string) {
	if err := a.Workbench.DeleteSpeaker(r.Context(), id); err != nil {
		speakerWriteError(w, err)
		return
	}
	apiJSON(w, map[string]any{"id": id, "deleted": true})
}

func (a *API) speakerEnroll(w http.ResponseWriter, r *http.Request) {
	mediaType, err := requestMediaType(r)
	if err != nil {
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	var profile any
	switch mediaType {
	case "application/json":
		input := struct {
			VoiceID     string  `json:"voiceId"`
			Name        *string `json:"name,omitempty"`
			BindVoiceID string  `json:"bindVoiceId,omitempty"`
		}{}
		if !validSpeakerQuery(r) {
			speakerError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if err := decodeStrictJSON(http.MaxBytesReader(w, r.Body, speakerJSONLimit), &input); err != nil || input.VoiceID == "" {
			if err != nil {
				speakerInputError(w, err)
			} else {
				speakerError(w, http.StatusBadRequest, "invalid_input")
			}
			return
		}
		name := ""
		if input.Name != nil {
			name = *input.Name
		}
		profile, err = a.Workbench.EnrollSpeakerNamed(r.Context(), workbench.SpeakerSource{VoiceID: input.VoiceID}, name, input.Name != nil, input.BindVoiceID)
	case "application/octet-stream":
		if !validSpeakerQuery(r, "name", "bindVoiceId") {
			speakerError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if names, ok := r.URL.Query()["name"]; ok && strings.TrimSpace(names[0]) == "" {
			speakerError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		body := http.MaxBytesReader(w, r.Body, 20<<20)
		profile, err = a.Workbench.EnrollSpeakerFrom(r.Context(), body, r.URL.Query().Get("name"), r.URL.Query().Get("bindVoiceId"))
	default:
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if err != nil {
		speakerWriteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	apiJSON(w, profile)
}

func (a *API) speakerIdentify(w http.ResponseWriter, r *http.Request) {
	mediaType, err := requestMediaType(r)
	if err != nil {
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	var result any
	switch mediaType {
	case "application/json":
		input := struct {
			VoiceID string `json:"voiceId"`
		}{}
		if !validSpeakerQuery(r) {
			speakerError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if err := decodeStrictJSON(http.MaxBytesReader(w, r.Body, speakerJSONLimit), &input); err != nil || input.VoiceID == "" {
			if err != nil {
				speakerInputError(w, err)
			} else {
				speakerError(w, http.StatusBadRequest, "invalid_input")
			}
			return
		}
		result, err = a.Workbench.IdentifySpeaker(r.Context(), workbench.SpeakerSource{VoiceID: input.VoiceID})
	case "application/octet-stream":
		if !validSpeakerQuery(r) {
			speakerError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		result, err = a.Workbench.IdentifySpeakerFrom(r.Context(), http.MaxBytesReader(w, r.Body, 20<<20))
	default:
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if err != nil {
		speakerWriteError(w, err)
		return
	}
	apiJSON(w, result)
}

func (a *API) speakerGenerate(w http.ResponseWriter, r *http.Request) {
	if !validSpeakerQuery(r) {
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if mediaType, err := requestMediaType(r); err != nil || mediaType != "application/json" {
		speakerError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/speakers/"), "/generate")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	draft, voiceProvided, referenceTextProvided, err := decodeProfileGeneration(r.Body)
	if err != nil || voiceProvided || referenceTextProvided {
		if err != nil {
			speakerInputError(w, err)
		} else {
			speakerError(w, http.StatusBadRequest, "invalid_input")
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.generationTimeout())
	defer cancel()
	generation, err := a.Workbench.SynthesizeSpeaker(ctx, id, draft)
	if err != nil {
		speakerWriteError(w, err)
		return
	}
	w.Header().Set("X-Yovoice-Speaker-Profile", id)
	a.audio(w, r, generation.ID)
}

func requestMediaType(r *http.Request) (string, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return "", fmt.Errorf("invalid content type")
	}
	return mediaType, nil
}

func validSpeakerQuery(r *http.Request, allowed ...string) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for key, values := range query {
		if !allowedSet[key] || len(values) != 1 {
			return false
		}
	}
	return true
}

func decodeStrictJSON(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return fmt.Errorf("only one JSON object is allowed")
	}
	return nil
}

func decodeProfileGeneration(body io.Reader) (schema.Draft, bool, bool, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return schema.Draft{}, false, false, err
	}
	var fields map[string]json.RawMessage
	if err = decodeStrictJSON(strings.NewReader(string(raw)), &fields); err != nil {
		return schema.Draft{}, false, false, err
	}
	draft, err := decodeGenerationUnchecked(strings.NewReader(string(raw)))
	if err != nil {
		return draft, false, false, err
	}
	_, voiceProvided := fields["voiceId"]
	_, referenceTextProvided := fields["referenceText"]
	return draft, voiceProvided, referenceTextProvided, nil
}

func decodeGenerationUnchecked(body io.Reader) (schema.Draft, error) {
	draft := schema.DefaultDraft()
	draft.Title, draft.Text, draft.Mode, draft.EmotionText = "API 语音", "", "speaker", ""
	input := struct {
		Text string `json:"text"`
		schema.SynthesisSettings
	}{SynthesisSettings: draft.SynthesisSettings}
	if err := decodeStrictJSON(body, &input); err != nil {
		return draft, err
	}
	draft.Text, draft.SynthesisSettings = input.Text, input.SynthesisSettings
	if strings.TrimSpace(draft.Text) == "" {
		return draft, fmt.Errorf("text is required")
	}
	return draft, nil
}

func speakerWriteError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		speakerError(w, http.StatusRequestEntityTooLarge, "input_too_large")
		return
	}
	code := workbench.SpeakerErrorCode(err)
	status := map[string]int{
		"invalid_input": http.StatusBadRequest, "audio": http.StatusBadRequest, "feature": http.StatusBadRequest,
		"unsupported_clone": http.StatusBadRequest, "profile": http.StatusNotFound, "voice_missing": http.StatusNotFound,
		"busy": http.StatusConflict, "capacity": http.StatusConflict, "feature_mismatch": http.StatusConflict,
		"input_too_large": http.StatusRequestEntityTooLarge, "runtime_not_configured": http.StatusServiceUnavailable,
		"runtime_failed": http.StatusBadGateway, "persistence_failed": http.StatusInternalServerError,
		"deadline": http.StatusGatewayTimeout, "cancelled": http.StatusRequestTimeout,
	}[code]
	if status == 0 {
		status = http.StatusInternalServerError
		code = "persistence_failed"
	}
	speakerError(w, status, code)
}

func speakerInputError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		speakerError(w, http.StatusRequestEntityTooLarge, "input_too_large")
		return
	}
	speakerError(w, http.StatusBadRequest, "invalid_input")
}

func speakerError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": strings.ReplaceAll(code, "_", " ")}})
}
