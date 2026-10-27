package tts

import (
	"context"
	"fmt"
)

// Recorded replays synthesized audio from a fixture in order. Each call to
// Synthesize returns the next chunk. When the list is exhausted it returns an
// error, catching tests that produce more calls than expected.
type Recorded struct {
	Chunks     [][]byte
	SampleRate int
	pos        int
}

// Synthesize returns the next recorded audio chunk.
func (r *Recorded) Synthesize(_ context.Context, _ string) ([]byte, int, error) {
	if r.pos >= len(r.Chunks) {
		return nil, 0, fmt.Errorf("tts: recorded fixture exhausted after %d chunks", len(r.Chunks))
	}
	pcm := r.Chunks[r.pos]
	r.pos++
	rate := r.SampleRate
	if rate == 0 {
		rate = 8000
	}
	return pcm, rate, nil
}
