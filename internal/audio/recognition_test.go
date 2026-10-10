package audio

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"yovoice/internal/msg"
)

func TestRecognitionWAVValidatesRecognitionPCM16Bounds(t *testing.T) {
	const sampleValue = int16(1024)
	cases := []struct {
		name         string
		data         []byte
		wantCode     msg.Code
		wantDuration float64
		wantRMS      float64
	}{
		{name: "empty", data: nil, wantCode: msg.ErrAudioFormat},
		{name: "corrupt", data: []byte("not a WAV"), wantCode: msg.ErrAudioFormat},
		{name: "truncated", data: recognitionPCM16WAV(16000, sampleValue)[:20], wantCode: msg.ErrAudioFormat},
		{name: "near silence", data: recognitionPCM16WAV(16000, 1), wantCode: msg.ErrAudioFormat},
		{name: "shorter than one second", data: recognitionPCM16WAV(15999, sampleValue), wantCode: msg.ErrAudioDuration},
		{name: "exactly one second", data: recognitionPCM16WAV(16000, sampleValue), wantDuration: 1, wantRMS: float64(sampleValue) / 32768},
		{name: "exactly sixty seconds", data: recognitionPCM16WAV(16000*60, sampleValue), wantDuration: 60, wantRMS: float64(sampleValue) / 32768},
		{name: "longer than sixty seconds", data: recognitionPCM16WAV(16000*60+1, sampleValue), wantCode: msg.ErrAudioDuration},
	}

	directory := t.TempDir()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if len(test.data) > 2<<20 {
				t.Fatalf("test WAV unexpectedly exceeds local fixture limit: %d", len(test.data))
			}
			path := filepath.Join(directory, test.name+".wav")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}

			duration, rms, err := RecognitionWAV(path)
			if test.wantCode != "" {
				if code := recognitionWAVErrorCode(err); code != test.wantCode {
					t.Fatalf("RecognitionWAV error code = %q, want %q (error %v)", code, test.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(duration-test.wantDuration) > 1e-12 {
				t.Fatalf("duration = %v, want %v", duration, test.wantDuration)
			}
			if math.Abs(rms-test.wantRMS) > 1e-12 {
				t.Fatalf("RMS = %v, want %v", rms, test.wantRMS)
			}
		})
	}
}

func recognitionWAVErrorCode(err error) msg.Code {
	var callError *msg.CallError
	if !errors.As(err, &callError) {
		return ""
	}
	return callError.Code
}

func recognitionPCM16WAV(samples int, value int16) []byte {
	dataLength := samples * 2
	result := make([]byte, 44+dataLength)
	copy(result, "RIFF")
	binary.LittleEndian.PutUint32(result[4:], uint32(len(result)-8))
	copy(result[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(result[16:], 16)
	binary.LittleEndian.PutUint16(result[20:], 1)
	binary.LittleEndian.PutUint16(result[22:], 1)
	binary.LittleEndian.PutUint32(result[24:], 16000)
	binary.LittleEndian.PutUint32(result[28:], 32000)
	binary.LittleEndian.PutUint16(result[32:], 2)
	binary.LittleEndian.PutUint16(result[34:], 16)
	copy(result[36:], "data")
	binary.LittleEndian.PutUint32(result[40:], uint32(dataLength))
	for sample := 0; sample < samples; sample++ {
		binary.LittleEndian.PutUint16(result[44+sample*2:], uint16(value))
	}
	return result
}
