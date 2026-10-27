// Package tts defines the text-to-speech interface. A real provider (ElevenLabs,
// Google TTS, a local model) implements Synthesizer; the call engine's
// brain.Speaker interface wraps it to produce PCM frames at the engine's rate.
package tts

import "context"

// Synthesizer turns text into audio. The returned PCM is signed 16-bit
// little-endian mono at the provider's native sample rate; the caller resamples
// to 8 kHz if needed.
type Synthesizer interface {
	Synthesize(ctx context.Context, text string) (pcm []byte, sampleRate int, err error)
}
