package media

import (
	"encoding/binary"
	"math"
)

// VAD is a simple energy-based voice activity detector. It accumulates audio
// frames and reports when a complete utterance (speech followed by silence) is
// ready to be transcribed.
//
// The caller feeds 20 ms PCM16 frames via Feed(). When speech ends (energy
// drops below the threshold for SilenceFrames consecutive frames), Utterance()
// returns the accumulated audio and resets.
type VAD struct {
	// Threshold is the RMS energy (0–32768) below which a frame is considered
	// silence. Defaults to 500, which works for typical phone audio.
	Threshold float64
	// SilenceFrames is how many consecutive silent frames end an utterance.
	// Defaults to 15 (300 ms at 20 ms/frame).
	SilenceFrames int

	speaking bool
	silent   int
	buf      []byte
}

func (v *VAD) threshold() float64 {
	if v.Threshold <= 0 {
		return 500
	}
	return v.Threshold
}

func (v *VAD) silenceFrames() int {
	if v.SilenceFrames <= 0 {
		return 15
	}
	return v.SilenceFrames
}

// Feed accepts one 20 ms PCM16 LE frame and returns true when a complete
// utterance is ready (call Utterance to retrieve it).
func (v *VAD) Feed(frame []byte) bool {
	energy := rms(frame)
	loud := energy >= v.threshold()

	if loud {
		v.speaking = true
		v.silent = 0
		v.buf = append(v.buf, frame...)
		return false
	}

	if !v.speaking {
		return false
	}

	// Still in speech, but this frame is silent.
	v.silent++
	v.buf = append(v.buf, frame...)

	if v.silent >= v.silenceFrames() {
		return true
	}
	return false
}

// Utterance returns the accumulated audio and resets the detector.
// Call only after Feed returned true.
func (v *VAD) Utterance() []byte {
	out := v.buf
	v.buf = nil
	v.speaking = false
	v.silent = 0
	return out
}

// Speaking reports whether the detector is in a speech segment.
func (v *VAD) Speaking() bool { return v.speaking }

// Reset clears any buffered audio and returns to the idle state.
func (v *VAD) Reset() {
	v.buf = nil
	v.speaking = false
	v.silent = 0
}

// rms computes the root mean square of PCM16 LE samples.
func rms(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(n))
}
