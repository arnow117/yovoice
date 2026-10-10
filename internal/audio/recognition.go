package audio

import (
	"encoding/binary"
	"io"
	"math"
	"os"

	"yovoice/internal/msg"
)

// RecognitionWAV 检查提取器的输入格式、时长和归一化 RMS。
// 返回值不尝试判断语言、人数或录音真实性。
func RecognitionWAV(path string) (duration, rms float64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	header := make([]byte, 12)
	if _, err = io.ReadFull(file, header); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
	}
	var formatOK bool
	var samples uint64
	var energy float64
	for {
		chunk := make([]byte, 8)
		if _, readErr := io.ReadFull(file, chunk); readErr == io.EOF {
			break
		} else if readErr != nil {
			return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		if size > 20<<20 || size%2 != 0 {
			return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
		}
		data := make([]byte, size)
		if _, err = io.ReadFull(file, data); err != nil {
			return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
		}
		switch string(chunk[:4]) {
		case "fmt ":
			if len(data) < 16 || binary.LittleEndian.Uint16(data) != 1 || binary.LittleEndian.Uint16(data[2:]) != 1 || binary.LittleEndian.Uint32(data[4:]) != 16000 || binary.LittleEndian.Uint16(data[14:]) != 16 {
				return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
			}
			formatOK = true
		case "data":
			for offset := 0; offset < len(data); offset += 2 {
				value := float64(int16(binary.LittleEndian.Uint16(data[offset:]))) / 32768
				energy += value * value
				samples++
			}
		}
	}
	if !formatOK || samples == 0 {
		return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
	}
	duration = float64(samples) / 16000
	rms = math.Sqrt(energy / float64(samples))
	if duration < 1 || duration > 60 {
		return 0, 0, msg.Err(msg.ErrAudioDuration, nil)
	}
	if rms < 1e-4 || math.IsNaN(rms) || math.IsInf(rms, 0) {
		return 0, 0, msg.Err(msg.ErrAudioFormat, nil)
	}
	return duration, rms, nil
}
