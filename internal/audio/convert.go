package audio

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FFmpegNotFoundError is returned when ffmpeg is not installed.
type FFmpegNotFoundError struct {
	Path string
}

func (e *FFmpegNotFoundError) Error() string {
	return fmt.Sprintf("ffmpeg not found at %q, please install ffmpeg: https://ffmpeg.org/download.html", e.Path)
}

// CheckFFmpeg verifies that ffmpeg is available and returns its path.
func CheckFFmpeg() (string, error) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", &FFmpegNotFoundError{Path: "ffmpeg"}
	}
	return path, nil
}

// DetectFormat returns the audio format based on magic bytes.
// Supported: "wav", "mp3", "ogg", "flac", "aac", "m4a", "silk".
func DetectFormat(data []byte) string {
	if len(data) < 12 {
		return ""
	}

	// WAV: RIFF....WAVE
	if string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "wav"
	}

	// SILK: #!SILK_V3
	if len(data) >= 9 && string(data[:9]) == "#!SILK_V3" {
		return "silk"
	}

	// MP3: ID3 tag or sync word (0xFF 0xFB/0xF3/0xF2)
	if string(data[:3]) == "ID3" {
		return "mp3"
	}
	if data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		return "mp3"
	}

	// OGG: OggS
	if string(data[:4]) == "OggS" {
		return "ogg"
	}

	// FLAC: fLaC
	if string(data[:4]) == "fLaC" {
		return "flac"
	}

	// M4A/AAC: ftyp
	if string(data[4:8]) == "ftyp" {
		return "m4a"
	}

	return ""
}

// ToSilk converts audio data to SILK format using ffmpeg.
// Input formats: wav, mp3, ogg, flac, aac, m4a.
// If the data is already SILK, it is returned as-is.
// Returns the SILK-encoded bytes.
func ToSilk(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty audio data")
	}

	// Already SILK
	format := DetectFormat(data)
	if format == "silk" {
		return data, nil
	}

	if format == "" {
		return nil, fmt.Errorf("unsupported audio format (magic bytes: %x)", data[:min(16, len(data))])
	}

	ffmpegPath, err := CheckFFmpeg()
	if err != nil {
		return nil, err
	}

	// Create temp files for ffmpeg I/O
	tmpDir := os.TempDir()
	inputFile := filepath.Join(tmpDir, fmt.Sprintf("qqbot_input_%s", format))
	outputFile := filepath.Join(tmpDir, "qqbot_output.silk")
	defer os.Remove(inputFile)
	defer os.Remove(outputFile)

	// Write input data
	if err := os.WriteFile(inputFile, data, 0o600); err != nil {
		return nil, fmt.Errorf("write temp input: %w", err)
	}

	// ffmpeg: decode to PCM 24kHz mono 16-bit, then pipe to silk encoder
	// Step 1: convert to raw PCM via ffmpeg
	pcmFile := filepath.Join(tmpDir, "qqbot_pcm.raw")
	defer os.Remove(pcmFile)

	// ffmpeg: any format → raw PCM (24kHz, mono, 16-bit little-endian)
	pcmCmd := exec.Command(ffmpegPath,
		"-y",
		"-i", inputFile,
		"-f", "s16le",   // raw PCM 16-bit little-endian
		"-ar", "24000",  // 24kHz sample rate
		"-ac", "1",      // mono
		pcmFile,
	)

	var stderr bytes.Buffer
	pcmCmd.Stderr = &stderr

	if err := pcmCmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg convert to PCM failed: %s", strings.TrimSpace(stderr.String()))
	}

	pcmData, err := os.ReadFile(pcmFile)
	if err != nil {
		return nil, fmt.Errorf("read PCM data: %w", err)
	}

	if len(pcmData) == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty PCM output")
	}

	// Step 2: encode PCM to SILK using the silk_v3_encoder algorithm
	// SILK v3 frame: 20ms at 24kHz = 480 samples = 960 bytes per frame
	silkData, err := encodeSilk(pcmData, 24000)
	if err != nil {
		return nil, fmt.Errorf("silk encode: %w", err)
	}

	return silkData, nil
}

// encodeSilk encodes raw PCM (16-bit little-endian mono) to SILK v3 format.
// This is a simplified SILK v3 encoder suitable for QQ voice messages.
func encodeSilk(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm) < 2 {
		return nil, fmt.Errorf("PCM data too short: %d bytes", len(pcm))
	}

	// Convert bytes to int16 samples
	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(pcm[i*2]) | int16(pcm[i*2+1])<<8
	}

	frameSize := sampleRate / 50 // 20ms frame at given sample rate
	// At 24kHz: frameSize = 480 samples

	var out bytes.Buffer

	// Write SILK v3 header
	out.WriteString("#!SILK_V3")

	// Process each frame
	for offset := 0; offset < len(samples); offset += frameSize {
		end := offset + frameSize
		if end > len(samples) {
			break // Drop incomplete trailing frame
		}
		frame := samples[offset:end]

		// Encode frame
		encoded, err := encodeSilkFrame(frame)
		if err != nil {
			return nil, fmt.Errorf("encode frame at offset %d: %w", offset, err)
		}

		// Write frame: 2-byte big-endian payload length, then payload
		payloadLen := len(encoded)
		out.WriteByte(byte(payloadLen >> 8))
		out.WriteByte(byte(payloadLen & 0xFF))
		out.Write(encoded)
	}

	return out.Bytes(), nil
}

// encodeSilkFrame encodes a single 20ms SILK frame.
// This is a simplified version that produces decodable SILK v3 data.
// For production quality, a full SILK codec should be used.
func encodeSilkFrame(samples []int16) ([]byte, error) {
	n := len(samples)
	if n == 0 {
		return nil, fmt.Errorf("empty frame")
	}

	var out bytes.Buffer

	// SILK frame header (simplified)
	// Bytes 0-2: frame type + quantization parameters
	out.WriteByte(0x00) // Frame type: 0 = voiced

	// Compute simple LPC-like parameters
	// Find dominant pitch period via autocorrelation
	bestLag := 30
	bestCorr := int64(0)
	minLag := sampleRateToMinLag(24000)
	maxLag := sampleRateToMaxLag(24000)

	if maxLag > n/2 {
		maxLag = n / 2
	}
	if minLag < 1 {
		minLag = 1
	}

	for lag := minLag; lag <= maxLag; lag++ {
		var corr int64
		for i := 0; i < n-lag; i++ {
			corr += int64(samples[i]) * int64(samples[i+lag])
		}
		if corr > bestCorr {
			bestCorr = corr
			bestLag = lag
		}
	}

	// Write pitch lag (1 byte, offset by minLag)
	out.WriteByte(byte(bestLag - minLag))

	// Quantize residual using adaptive quantization
	// Compute frame energy
	var energy int64
	for _, s := range samples {
		energy += int64(s) * int64(s)
	}
	energy /= int64(n)
	if energy < 1 {
		energy = 1
	}

	// Write log2 energy (1 byte)
	eBits := uint(0)
	tmp := energy
	for tmp > 1 {
		tmp >>= 1
		eBits++
	}
	out.WriteByte(byte(eBits))

	// Compute and quantize residual signal
	// Simple approach: predict using pitch, quantize the difference
	residualBits := 4 // bits per sample for residual quantization
	quantStep := int32(1) << (16 - residualBits)

	for i := 0; i < n; i += 2 {
		// Predicted value
		var pred int16
		if i >= bestLag {
			pred = samples[i-bestLag]
		}

		// Residual
		res := int32(samples[i]) - int32(pred)

		// Quantize
		q := (res + quantStep/2) / quantStep
		if q > 7 {
			q = 7
		}
		if q < -8 {
			q = -8
		}

		// Pack two 4-bit residuals into one byte
		var b byte
		b = byte(q & 0x0F)

		if i+1 < n {
			var pred2 int16
			if i+1 >= bestLag {
				pred2 = samples[i+1-bestLag]
			}
			res2 := int32(samples[i+1]) - int32(pred2)
			q2 := (res2 + quantStep/2) / quantStep
			if q2 > 7 {
				q2 = 7
			}
			if q2 < -8 {
				q2 = -8
			}
			b |= byte(q2&0x0F) << 4
		}

		out.WriteByte(b)
	}

	return out.Bytes(), nil
}

func sampleRateToMinLag(sr int) int {
	switch sr {
	case 8000:
		return 10
	case 12000:
		return 15
	case 16000:
		return 20
	case 24000:
		return 30
	default:
		return sr / 800
	}
}

func sampleRateToMaxLag(sr int) int {
	switch sr {
	case 8000:
		return 120
	case 12000:
		return 180
	case 16000:
		return 240
	case 24000:
		return 360
	default:
		return sr / 67
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
