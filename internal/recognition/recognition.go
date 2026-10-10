// Package recognition 管理本地声纹档案、特征比较与受限的 Python 提取器协议。
package recognition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"yovoice/internal/platform"
)

const (
	Dimension        = 256
	RuntimeVersion   = "1.13.8"
	maxProfiles      = 1000
	maxDatabaseBytes = 8 << 20
	maxStdoutBytes   = 64 << 10
	maxStderrBytes   = 8 << 10
)

// Error 用于把内部失败映射为稳定且不会泄露路径的调用方错误码。
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

func fail(code string) error { return &Error{Code: code} }

func Code(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return "persistence_failed"
}

type Config struct {
	Python         string
	Model          string
	Runner         string
	ModelSHA256    string
	RunnerSHA256   string
	FeatureVersion string
	Threshold      float64
	Margin         float64
	Timeout        time.Duration
}

// LoadConfig 读取一次服务进程的本地、不可变识别配置。
func LoadConfig(getenv func(string) string) (Config, error) {
	config := Config{Threshold: .6, Margin: .05, Timeout: 120 * time.Second}
	for _, field := range []struct {
		name string
		out  *string
	}{
		{"YOVOICE_SPEAKER_PYTHON", &config.Python},
		{"YOVOICE_SPEAKER_MODEL", &config.Model},
		{"YOVOICE_SPEAKER_RUNNER", &config.Runner},
	} {
		value := strings.TrimSpace(getenv(field.name))
		if value == "" || !filepath.IsAbs(value) {
			return Config{}, fail("runtime_not_configured")
		}
		info, err := os.Stat(value)
		if err != nil || !info.Mode().IsRegular() {
			return Config{}, fail("runtime_not_configured")
		}
		*field.out = filepath.Clean(value)
	}
	var err error
	if config.Threshold, err = floatEnv(getenv, "YOVOICE_SPEAKER_THRESHOLD", config.Threshold, -1, 1, true); err != nil {
		return Config{}, err
	}
	if config.Margin, err = floatEnv(getenv, "YOVOICE_SPEAKER_MARGIN", config.Margin, 0, 2, false); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(getenv("YOVOICE_SPEAKER_TIMEOUT")); raw != "" {
		config.Timeout, err = time.ParseDuration(raw)
		if err != nil || config.Timeout <= 0 || config.Timeout > 10*time.Minute {
			return Config{}, fail("runtime_not_configured")
		}
	}
	if config.ModelSHA256, err = fileSHA256(config.Model); err != nil {
		return Config{}, fail("runtime_not_configured")
	}
	if config.RunnerSHA256, err = fileSHA256(config.Runner); err != nil {
		return Config{}, fail("runtime_not_configured")
	}
	versionInput := strings.Join([]string{config.ModelSHA256, config.RunnerSHA256, RuntimeVersion, "pcm16-mono16k-v1", "l2-cosine-v1", strconv.Itoa(Dimension)}, "\n")
	sum := sha256.Sum256([]byte(versionInput))
	config.FeatureVersion = hex.EncodeToString(sum[:])
	return config, nil
}

func floatEnv(getenv func(string) string, name string, fallback, min, max float64, inclusiveMin bool) (float64, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value > max || (inclusiveMin && value < min) || (!inclusiveMin && value <= min) {
		return 0, fail("runtime_not_configured")
	}
	return value, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type Profile struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"createdAt"`
	BindVoiceID    string    `json:"bindVoiceId,omitempty"`
	ModelSHA256    string    `json:"modelSha256"`
	FeatureVersion string    `json:"featureVersion"`
	Dimension      int       `json:"dimension"`
	Embedding      []float64 `json:"embedding"`
}

type PublicProfile struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"createdAt"`
	BindVoiceID    string    `json:"bindVoiceId,omitempty"`
	ModelSHA256    string    `json:"modelSha256"`
	FeatureVersion string    `json:"featureVersion"`
	Dimension      int       `json:"dimension"`
}

func (p Profile) Public() PublicProfile {
	return PublicProfile{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, BindVoiceID: p.BindVoiceID, ModelSHA256: p.ModelSHA256, FeatureVersion: p.FeatureVersion, Dimension: p.Dimension}
}

type database struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type Repository struct {
	root         string
	beforeAccept func()
}

func NewRepository(root string) Repository { return Repository{root: root} }

func (r Repository) Path() string { return filepath.Join(r.root, "speakers.json") }

func (r Repository) Load() ([]Profile, error) {
	path := r.Path()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return []Profile{}, nil
	}
	if err != nil || info.Size() > maxDatabaseBytes || !info.Mode().IsRegular() {
		return nil, fail("persistence_failed")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fail("persistence_failed")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxDatabaseBytes+1))
	decoder.DisallowUnknownFields()
	var data database
	if err = decoder.Decode(&data); err != nil || decoder.Decode(new(any)) != io.EOF || data.Version != 1 || len(data.Profiles) > maxProfiles {
		return nil, fail("persistence_failed")
	}
	seen := map[string]bool{}
	for _, profile := range data.Profiles {
		if seen[profile.ID] || !validProfile(profile) {
			return nil, fail("persistence_failed")
		}
		seen[profile.ID] = true
	}
	return data.Profiles, nil
}

func (r Repository) Save(ctx context.Context, profiles []Profile) error {
	if len(profiles) > maxProfiles {
		return fail("capacity")
	}
	seen := map[string]bool{}
	for _, profile := range profiles {
		if seen[profile.ID] || !validProfile(profile) {
			return fail("persistence_failed")
		}
		seen[profile.ID] = true
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(database{Version: 1, Profiles: profiles}, "", "  ")
	if err != nil || len(payload) > maxDatabaseBytes {
		if len(payload) > maxDatabaseBytes {
			return fail("capacity")
		}
		return fail("persistence_failed")
	}
	temp, err := os.CreateTemp(r.root, ".speakers-*")
	if err != nil {
		return fail("persistence_failed")
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(payload)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fail("persistence_failed")
	}
	if r.beforeAccept != nil {
		r.beforeAccept()
	}
	// 此检查是提交接受点；Rename 成功后即视为成功。
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(name, r.Path()); err != nil {
		return fail("persistence_failed")
	}
	return nil
}

func validProfile(profile Profile) bool {
	if len(profile.ID) != 32 || profile.Name == "" || len([]rune(profile.Name)) > 100 || profile.CreatedAt.IsZero() || len(profile.ModelSHA256) != 64 || profile.FeatureVersion == "" || profile.Dimension != Dimension || len(profile.Embedding) != Dimension {
		return false
	}
	for _, r := range profile.ID + profile.ModelSHA256 {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return normalized(profile.Embedding)
}

func normalized(values []float64) bool {
	sum := 0.0
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
		sum += value * value
	}
	return math.Abs(math.Sqrt(sum)-1) <= 1e-6
}

func Normalize(values []float64) ([]float64, error) {
	if len(values) != Dimension {
		return nil, fail("feature")
	}
	sum := 0.0
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fail("feature")
		}
		sum += value * value
	}
	if sum <= 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return nil, fail("feature")
	}
	scale := 1 / math.Sqrt(sum)
	result := make([]float64, Dimension)
	for i, value := range values {
		result[i] = value * scale
	}
	return result, nil
}

type Candidate struct {
	ProfileID string  `json:"profileId"`
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
}

type Identification struct {
	Status         string      `json:"status"`
	Reason         string      `json:"reason"`
	ProfileID      string      `json:"profileId,omitempty"`
	Score          *float64    `json:"score,omitempty"`
	Candidates     []Candidate `json:"candidates"`
	ModelSHA256    string      `json:"modelSha256"`
	FeatureVersion string      `json:"featureVersion"`
	Threshold      float64     `json:"threshold"`
	Margin         float64     `json:"margin"`
}

func Identify(config Config, profiles []Profile, query []float64) (Identification, error) {
	query, err := Normalize(query)
	if err != nil {
		return Identification{}, err
	}
	result := Identification{Status: "unknown", Reason: "empty_library", Candidates: []Candidate{}, ModelSHA256: config.ModelSHA256, FeatureVersion: config.FeatureVersion, Threshold: config.Threshold, Margin: config.Margin}
	if len(profiles) == 0 {
		return result, nil
	}
	candidates := make([]Candidate, 0, len(profiles))
	for _, profile := range profiles {
		if profile.ModelSHA256 != config.ModelSHA256 || profile.FeatureVersion != config.FeatureVersion || profile.Dimension != Dimension || !normalized(profile.Embedding) {
			return Identification{}, fail("feature_mismatch")
		}
		score := 0.0
		for index, value := range profile.Embedding {
			score += value * query[index]
		}
		candidates = append(candidates, Candidate{ProfileID: profile.ID, Name: profile.Name, Score: score})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ProfileID < candidates[j].ProfileID
		}
		return candidates[i].Score > candidates[j].Score
	})
	best := candidates[0].Score
	result.Score = &best
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	result.Candidates = candidates
	if best < config.Threshold-1e-12 {
		result.Reason = "below_threshold"
		return result, nil
	}
	if len(candidates) > 1 && (best == candidates[1].Score || best-candidates[1].Score < config.Margin-1e-12) {
		result.Status, result.Reason = "ambiguous", "close_candidates"
		return result, nil
	}
	result.Status, result.Reason, result.ProfileID = "matched", "matched", candidates[0].ProfileID
	return result, nil
}

type runnerRequest struct {
	ProtocolVersion int    `json:"protocolVersion"`
	ModelPath       string `json:"modelPath"`
	ModelSHA256     string `json:"modelSha256"`
	AudioPath       string `json:"audioPath"`
}

type runnerResponse struct {
	ProtocolVersion int       `json:"protocolVersion"`
	ModelSHA256     string    `json:"modelSha256"`
	RuntimeVersion  string    `json:"runtimeVersion"`
	Dimension       int       `json:"dimension"`
	Embedding       []float64 `json:"embedding"`
}

func Extract(ctx context.Context, config Config, audioPath string) ([]float64, error) {
	if after, err := fileSHA256(config.Model); err != nil || after != config.ModelSHA256 {
		return nil, fail("runtime_failed")
	}
	if after, err := fileSHA256(config.Runner); err != nil || after != config.RunnerSHA256 {
		return nil, fail("runtime_failed")
	}
	deadline, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	request, err := json.Marshal(runnerRequest{ProtocolVersion: 1, ModelPath: config.Model, ModelSHA256: config.ModelSHA256, AudioPath: audioPath})
	if err != nil {
		return nil, fail("runtime_failed")
	}
	command := execCommand(deadline, config.Python, "-I", config.Runner)
	command.Env = cleanEnvironment()
	platform.ConfigureProcess(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fail("runtime_failed")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fail("runtime_failed")
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fail("runtime_failed")
	}
	if err = command.Start(); err != nil {
		return nil, fail("runtime_failed")
	}
	stopKill := make(chan struct{})
	defer close(stopKill)
	go func() {
		select {
		case <-deadline.Done():
			platform.KillProcess(command)
		case <-stopKill:
		}
	}()
	if _, err = stdin.Write(append(request, '\n')); err == nil {
		err = stdin.Close()
	}
	if err != nil {
		platform.KillProcess(command)
		_ = command.Wait()
		return nil, fail("runtime_failed")
	}
	type readResult struct {
		bytes []byte
		err   error
	}
	read := func(reader io.Reader, limit int64) <-chan readResult {
		result := make(chan readResult, 1)
		go func() {
			bytes, readErr := readBounded(reader, limit)
			if readErr != nil {
				platform.KillProcess(command)
			}
			result <- readResult{bytes: bytes, err: readErr}
		}()
		return result
	}
	stdoutResult := read(stdout, maxStdoutBytes)
	stderrResult := read(stderr, maxStderrBytes)
	// StdoutPipe/StderrPipe 的读取必须在 Wait 前完成。Wait 会关闭这些管道；
	// 先 Wait 会使快速退出的合法子进程偶发丢失尾部 JSON。
	stdoutValue := <-stdoutResult
	stderrValue := <-stderrResult
	stdoutBytes, stdoutErr := stdoutValue.bytes, stdoutValue.err
	stderrErr := stderrValue.err
	if stdoutErr != nil || stderrErr != nil {
		platform.KillProcess(command)
	}
	waitErr := command.Wait()
	if deadline.Err() != nil {
		platform.KillProcess(command)
		return nil, deadline.Err()
	}
	if stdoutErr != nil || stderrErr != nil {
		return nil, fail("runtime_failed")
	}
	if waitErr != nil {
		return nil, runnerFailure(stdoutBytes)
	}
	var response runnerResponse
	decoder := json.NewDecoder(strings.NewReader(string(stdoutBytes)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&response); err != nil || decoder.Decode(new(any)) != io.EOF || response.ProtocolVersion != 1 || response.ModelSHA256 != config.ModelSHA256 || response.RuntimeVersion != RuntimeVersion || response.Dimension != Dimension {
		return nil, fail("runtime_failed")
	}
	embedding, err := Normalize(response.Embedding)
	if err != nil {
		return nil, err
	}
	if after, err := fileSHA256(config.Model); err != nil || after != config.ModelSHA256 {
		return nil, fail("runtime_failed")
	}
	if after, err := fileSHA256(config.Runner); err != nil || after != config.RunnerSHA256 {
		return nil, fail("runtime_failed")
	}
	return embedding, nil
}

func runnerFailure(stdout []byte) error {
	var response struct {
		ErrorCode string `json:"errorCode"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil || decoder.Decode(new(any)) != io.EOF {
		return fail("runtime_failed")
	}
	switch response.ErrorCode {
	case "invalid_audio", "feature_not_ready":
		return fail("audio")
	case "invalid_feature":
		return fail("feature")
	case "invalid_request", "model_mismatch", "runtime_failed":
		return fail("runtime_failed")
	default:
		return fail("runtime_failed")
	}
}

var execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, arg...)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(value)) > limit {
		return nil, fail("runtime_failed")
	}
	return value, nil
}

func cleanEnvironment() []string {
	path := os.Getenv("PATH")
	if path == "" {
		return []string{}
	}
	return []string{"PATH=" + path}
}

func NewProfile(id, name, bindVoiceID string, config Config, embedding []float64) (Profile, error) {
	embedding, err := Normalize(embedding)
	if err != nil {
		return Profile{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	if len([]rune(name)) > 100 || strings.TrimSpace(id) == "" {
		return Profile{}, fail("invalid_input")
	}
	return Profile{ID: id, Name: name, CreatedAt: time.Now().UTC(), BindVoiceID: bindVoiceID, ModelSHA256: config.ModelSHA256, FeatureVersion: config.FeatureVersion, Dimension: Dimension, Embedding: embedding}, nil
}

func Find(profiles []Profile, id string) (Profile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

func Message(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("speaker recognition failed: %s", Code(err))
}
