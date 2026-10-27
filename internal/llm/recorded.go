package llm

import (
	"context"
	"fmt"
)

// Recorded replays completion responses from a fixture in order. Each call to
// Complete returns the next response. When the list is exhausted it returns an
// error, catching tests that produce more calls than expected.
type Recorded struct {
	Responses []Response
	pos       int
}

// Complete returns the next recorded response.
func (r *Recorded) Complete(_ context.Context, _ Request) (Response, error) {
	if r.pos >= len(r.Responses) {
		return Response{}, fmt.Errorf("llm: recorded fixture exhausted after %d responses", len(r.Responses))
	}
	resp := r.Responses[r.pos]
	r.pos++
	return resp, nil
}
