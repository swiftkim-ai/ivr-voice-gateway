package control

import (
	"context"
	"errors"
	"sync"
)

// ErrUnsupported is returned when an action is valid in the contract but the
// connected switch adapter cannot carry it out. It is an outcome, not a bug:
// the engine records it like any other result (ADR-0007).
var ErrUnsupported = errors.New("control: action not supported by this adapter")

// Controller carries out actions on the switch hosting the call. Media adapters
// carry audio; actions travel here, on a channel of their own (ADR-0004).
type Controller interface {
	// Do performs a on one switch channel. The channel string is
	// adapter-specific — an Asterisk channel name for AMI.
	Do(ctx context.Context, channel string, a Action) error
}

// Performed is one action a Loopback was asked to carry out.
type Performed struct {
	Channel string
	Action  Action
}

// Loopback records actions instead of performing them. It stands in for a
// switch in tests and in the WebSocket adapter, where the client is expected to
// act on the recorded action itself.
type Loopback struct {
	mu   sync.Mutex
	done []Performed
}

// Do records the action and always succeeds.
func (l *Loopback) Do(_ context.Context, channel string, a Action) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.done = append(l.done, Performed{Channel: channel, Action: a})
	return nil
}

// Performed returns a copy of everything recorded so far.
func (l *Loopback) Performed() []Performed {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Performed, len(l.done))
	copy(out, l.done)
	return out
}
