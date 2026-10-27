package stt

import (
	"context"
	"fmt"
)

// Recorded replays transcription results from a fixture in order. Each call to
// Transcribe returns the next result from the list. When the list is exhausted
// it returns an error, catching tests that produce more calls than expected.
type Recorded struct {
	Results []Result
	pos     int
}

// Transcribe returns the next recorded result.
func (r *Recorded) Transcribe(_ context.Context, _ []byte) (Result, error) {
	if r.pos >= len(r.Results) {
		return Result{}, fmt.Errorf("stt: recorded fixture exhausted after %d results", len(r.Results))
	}
	res := r.Results[r.pos]
	r.pos++
	return res, nil
}
