// Package media holds the audio primitives every adapter shares.
//
// At the engine boundary audio is always 8 kHz mono 16-bit little-endian PCM in
// 20 ms frames. Adapters resample and transcode at the edge; the engine never
// sees another format.
package media

const (
	// SampleRate is the phone-path rate used everywhere inside the engine.
	SampleRate = 8000
	// FrameMS is the frame duration the contract commits to.
	FrameMS = 20
	// SamplesPerFrame is 8000 Hz * 20 ms.
	SamplesPerFrame = SampleRate * FrameMS / 1000
	// FrameBytes is SamplesPerFrame int16 samples.
	FrameBytes = SamplesPerFrame * 2
)

// Silence returns n bytes of PCM16 zero samples (n should be a multiple of
// FrameBytes for a whole number of frames).
func Silence(n int) []byte { return make([]byte, n) }

// SilenceFrames returns n frames of silence, each FrameBytes long.
func SilenceFrames(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = make([]byte, FrameBytes)
	}
	return out
}

// MulawEncode converts PCM16 samples to G.711 mu-law bytes, the codec Asterisk
// speaks on the wire for the phone path.
func MulawEncode(pcm []int16) []byte {
	out := make([]byte, len(pcm))
	for i, s := range pcm {
		out[i] = encodeMulaw(s)
	}
	return out
}

// MulawDecode converts G.711 mu-law bytes back to PCM16 samples.
func MulawDecode(ulaw []byte) []int16 {
	out := make([]int16, len(ulaw))
	for i, b := range ulaw {
		out[i] = decodeMulaw(b)
	}
	return out
}

const mulawBias = 0x84

// encodeMulaw is the standard G.711 mu-law compressor.
func encodeMulaw(sample int16) byte {
	sign := byte(0)
	s := int(sample)
	ss := s
	if ss < 0 {
		sign = 0x80
		ss = -ss
	}
	if ss > 32635 {
		ss = 32635
	}
	ss += mulawBias
	exponent := 0
	switch {
	case ss < 0x200:
		exponent = 0
	case ss < 0x400:
		exponent = 1
	case ss < 0x800:
		exponent = 2
	case ss < 0x1000:
		exponent = 3
	case ss < 0x2000:
		exponent = 4
	case ss < 0x4000:
		exponent = 5
	case ss < 0x8000:
		exponent = 6
	default:
		exponent = 7
	}
	mantissa := (ss >> uint(exponent+3)) & 0x0f
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}

// decodeMulaw is the standard G.711 mu-law expander.
func decodeMulaw(u byte) int16 {
	u = ^u
	sign := u & 0x80
	exponent := (u >> 4) & 0x07
	mantissa := u & 0x0f
	sample := int(((int(mantissa) << 3) + mulawBias) << exponent)
	sample -= mulawBias
	if sign != 0 {
		sample = -sample
	}
	if sample > 32767 {
		sample = 32767
	}
	if sample < -32768 {
		sample = -32768
	}
	return int16(sample)
}
