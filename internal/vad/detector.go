package vad

import (
	"encoding/binary"
	"math"
)

const (
	DefaultSampleRateHz      = 16000
	DefaultSilenceDurationMs = 1500
	DefaultRMSThreshold      = 100
)

// Detector is a minimal PCM 16-bit mono VAD for wiring the utterance boundary.
// It is intentionally simple and can be replaced without changing queue code.
type Detector struct {
	sampleRateHz     int
	silenceTargetMs  int
	rmsThreshold     float64
	hasSpeech        bool
	silenceElapsedMs int
}

func NewDetector() *Detector {
	return &Detector{
		sampleRateHz:    DefaultSampleRateHz,
		silenceTargetMs: DefaultSilenceDurationMs,
		rmsThreshold:    DefaultRMSThreshold,
	}
}

// Observe returns true when a speech segment has ended after enough silence.
func (d *Detector) Observe(chunk []byte) bool {
	if len(chunk) < 2 {
		return false
	}

	if rms16LE(chunk) >= d.rmsThreshold {
		d.hasSpeech = true
		d.silenceElapsedMs = 0
		return false
	}

	if !d.hasSpeech {
		return false
	}

	d.silenceElapsedMs += durationMs(chunk, d.sampleRateHz)
	if d.silenceElapsedMs < d.silenceTargetMs {
		return false
	}

	d.Reset()
	return true
}

func (d *Detector) Reset() {
	d.hasSpeech = false
	d.silenceElapsedMs = 0
}

func durationMs(chunk []byte, sampleRateHz int) int {
	samples := len(chunk) / 2
	if samples == 0 || sampleRateHz <= 0 {
		return 0
	}

	return samples * 1000 / sampleRateHz
}

// RMS returns the root mean square energy of a PCM 16-bit little-endian chunk.
func RMS(chunk []byte) float64 { return rms16LE(chunk) }

func rms16LE(chunk []byte) float64 {
	sampleCount := len(chunk) / 2
	if sampleCount == 0 {
		return 0
	}

	var sumSquares float64
	for i := 0; i+1 < len(chunk); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(chunk[i : i+2]))
		value := float64(sample)
		sumSquares += value * value
	}

	return math.Sqrt(sumSquares / float64(sampleCount))
}
