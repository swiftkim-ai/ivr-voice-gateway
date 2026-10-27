package tts

import (
	"context"
	"log"

	"github.com/swiftkimani/ivr-voice-gateway/internal/media"
)

// Speaker wraps a Synthesizer into the brain.Speaker interface the call engine
// expects. It synthesizes text, resamples to 8 kHz if needed, and chops the
// result into 20 ms frames.
type Speaker struct {
	Synth Synthesizer
}

// Frames implements brain.Speaker.
func (s Speaker) Frames(text string) [][]byte {
	pcm, rate, err := s.Synth.Synthesize(context.Background(), text)
	if err != nil {
		log.Printf("tts: synthesize: %v", err)
		return silenceFrames(5)
	}
	if len(pcm) == 0 {
		return silenceFrames(5)
	}

	if rate != 8000 {
		pcm = resample(pcm, rate, 8000)
	}

	return chop(pcm, media.FrameBytes)
}

func chop(pcm []byte, frameBytes int) [][]byte {
	var frames [][]byte
	for i := 0; i+frameBytes <= len(pcm); i += frameBytes {
		frame := make([]byte, frameBytes)
		copy(frame, pcm[i:i+frameBytes])
		frames = append(frames, frame)
	}
	if tail := len(pcm) % frameBytes; tail > 0 {
		frame := make([]byte, frameBytes)
		copy(frame, pcm[len(pcm)-tail:])
		frames = append(frames, frame)
	}
	return frames
}

func silenceFrames(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = make([]byte, media.FrameBytes)
	}
	return out
}

// resample performs linear interpolation from srcRate to dstRate. Good enough
// for phone-quality audio; a production path would use a proper resampler.
func resample(pcm []byte, srcRate, dstRate int) []byte {
	if srcRate == dstRate {
		return pcm
	}
	srcSamples := len(pcm) / 2
	dstSamples := srcSamples * dstRate / srcRate
	out := make([]byte, dstSamples*2)

	for i := 0; i < dstSamples; i++ {
		srcPos := float64(i) * float64(srcRate) / float64(dstRate)
		idx := int(srcPos)
		frac := srcPos - float64(idx)

		s0 := readSample(pcm, idx)
		s1 := readSample(pcm, idx+1)
		sample := int16(float64(s0)*(1-frac) + float64(s1)*frac)

		out[i*2] = byte(sample)
		out[i*2+1] = byte(sample >> 8)
	}
	return out
}

func readSample(pcm []byte, idx int) int16 {
	if idx < 0 || idx*2+1 >= len(pcm) {
		return 0
	}
	return int16(pcm[idx*2]) | int16(pcm[idx*2+1])<<8
}
