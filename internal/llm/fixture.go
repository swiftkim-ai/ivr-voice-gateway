package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Exchange is one request-response pair, saved as a fixture for replay.
type Exchange struct {
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// Fixture is a recorded sequence of LLM exchanges for one call.
type Fixture struct {
	Exchanges []Exchange `json:"exchanges"`
}

// SaveFixture writes a fixture to path as pretty-printed JSON.
func SaveFixture(path string, f Fixture) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("llm: marshal fixture: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadFixture reads a fixture from path.
func LoadFixture(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("llm: read fixture: %w", err)
	}
	var f Fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return Fixture{}, fmt.Errorf("llm: parse fixture: %w", err)
	}
	return f, nil
}

// Recording wraps a Completer and records every exchange. After the call,
// Fixture() returns the recorded sequence.
type Recording struct {
	Inner     Completer
	exchanges []Exchange
}

// Complete delegates to Inner and records the exchange.
func (r *Recording) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := r.Inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	r.exchanges = append(r.exchanges, Exchange{Request: req, Response: resp})
	return resp, nil
}

// Fixture returns the recorded exchanges.
func (r *Recording) Fixture() Fixture {
	return Fixture{Exchanges: r.exchanges}
}

// RecordedFromFixture returns a Recorded completer from a fixture.
func RecordedFromFixture(f Fixture) *Recorded {
	responses := make([]Response, len(f.Exchanges))
	for i, ex := range f.Exchanges {
		responses[i] = ex.Response
	}
	return &Recorded{Responses: responses}
}
