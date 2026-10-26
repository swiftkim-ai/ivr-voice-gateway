package media

import (
	"encoding/binary"
	"math"
)

// DTMF frequency pairs per ITU-T Q.23.
var dtmfFreqs = map[byte][2]float64{
	'1': {697, 1209}, '2': {697, 1336}, '3': {697, 1477}, 'A': {697, 1633},
	'4': {770, 1209}, '5': {770, 1336}, '6': {770, 1477}, 'B': {770, 1633},
	'7': {852, 1209}, '8': {852, 1336}, '9': {852, 1477}, 'C': {852, 1633},
	'*': {941, 1209}, '0': {941, 1336}, '#': {941, 1477}, 'D': {941, 1633},
}

const (
	dtmfToneMS = 100
	dtmfGapMS  = 100
	dtmfAmp    = 8192
)

// DTMFTone generates PCM16 LE frames for a single DTMF digit at 8 kHz:
// dtmfToneMS of dual-tone followed by dtmfGapMS of silence. Returns nil
// for an unrecognised digit. Each returned slice is one 20 ms frame.
func DTMFTone(digit byte) [][]byte {
	freqs, ok := dtmfFreqs[digit]
	if !ok {
		return nil
	}

	toneSamples := SampleRate * dtmfToneMS / 1000
	gapSamples := SampleRate * dtmfGapMS / 1000
	totalSamples := toneSamples + gapSamples

	pcm := make([]int16, totalSamples)
	for i := 0; i < toneSamples; i++ {
		t := float64(i) / float64(SampleRate)
		lo := math.Sin(2 * math.Pi * freqs[0] * t)
		hi := math.Sin(2 * math.Pi * freqs[1] * t)
		pcm[i] = int16((lo + hi) * dtmfAmp)
	}

	raw := make([]byte, totalSamples*2)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(s))
	}

	var frames [][]byte
	for off := 0; off+FrameBytes <= len(raw); off += FrameBytes {
		frame := make([]byte, FrameBytes)
		copy(frame, raw[off:off+FrameBytes])
		frames = append(frames, frame)
	}
	return frames
}
