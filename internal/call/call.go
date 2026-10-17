// Package call is the engine: one Call is one live conversation. It turns
// caller input into agent speech and actions, emitting a wire message and an
// event-log entry for everything that happens. The same engine serves the live
// path and replay, so behaviour cannot drift between them.
package call

import (
	"context"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// Sink is the channel back to the caller/IVR. Adapters implement it: the
// WebSocket adapter writes JSON text and binary audio frames; the in-memory sink
// records them for tests and replay.
type Sink interface {
	Message(wire.Message) error
	Audio(pcm []byte) error
}

// Call drives one conversation.
type Call struct {
	ID      string
	Hint    string
	Brain   brain.Brain
	Speaker brain.Speaker
	Sink    Sink

	// Log is optional; when set, every decision is appended to it.
	Log *eventlog.Writer

	seq     int
	pending string
	history []brain.Turn
}

// Start opens the call: writes the opening event and tells the client.
func (c *Call) Start() error {
	c.append("call_started", map[string]any{"hint": c.Hint})
	c.pending = ""
	return c.emit(wire.Message{Type: "call_started", CallID: c.ID})
}

// Input feeds recognised caller speech (from STT) into the call and produces the
// agent's response.
func (c *Call) Input(ctx context.Context, text string) error {
	c.append("transcript", map[string]any{"text": text, "final": true})
	final := true
	if err := c.emit(wire.Message{Type: "transcript", Text: text, Final: &final}); err != nil {
		return err
	}
	c.pending = text
	return c.Respond(ctx)
}

// Digit feeds a keypad digit pressed by the caller. When sensitive is true the
// digit is redacted in the log and never reaches the brain (ADR: payment/PIN
// isolation), which is why the engine, not the adapter, owns this rule.
func (c *Call) Digit(ctx context.Context, digit string, sensitive bool) error {
	data := map[string]any{"digit": digit}
	sensitiveDigit := digit
	if sensitive {
		data["digit"] = "<redacted>"
		data["sensitive"] = true
	}
	c.append("dtmf", data)
	if err := c.emit(wire.Message{Type: "dtmf", Digit: sensitiveDigit, Sensitive: sensitive}); err != nil {
		return err
	}
	if sensitive {
		return nil
	}
	c.pending = "dtmf:" + digit
	return c.Respond(ctx)
}

// Respond asks the brain for the next turn and carries it out.
func (c *Call) Respond(ctx context.Context) error {
	in := brain.Input{Hint: c.Hint, Caller: c.pending, History: c.history}
	reply, err := c.Brain.Respond(ctx, in)
	if err != nil {
		c.append("error", map[string]any{"code": "brain_failed", "message": err.Error()})
		_ = c.emit(wire.Message{Type: "error", Code: "brain_failed", ErrMessage: err.Error()})
		return err
	}

	var spoken []string
	for _, a := range reply.Actions {
		if err := c.act(a); err != nil {
			return err
		}
	}
	for _, s := range reply.Sentences {
		if err := c.say(s); err != nil {
			return err
		}
		spoken = append(spoken, s)
	}

	caller := c.pending
	c.history = append(c.history, brain.Turn{Caller: caller, Agent: spoken})
	c.pending = ""
	return nil
}

// act records and emits a control action.
func (c *Call) act(a control.Action) error {
	c.append("action", map[string]any{"kind": a.Kind, "args": a.Args})
	return c.emit(wire.Message{Type: "action", Kind: a.Kind, Args: a.Args})
}

// say records the reply, emits it, then streams its audio frames.
func (c *Call) say(text string) error {
	c.append("reply", map[string]any{"text": text})
	if err := c.emit(wire.Message{Type: "reply", Text: text}); err != nil {
		return err
	}
	for _, f := range c.Speaker.Frames(text) {
		if err := c.Sink.Audio(f); err != nil {
			return err
		}
	}
	return nil
}

// End closes the call and flushes the log.
func (c *Call) End(reason string) error {
	c.append("call_ended", map[string]any{"reason": reason})
	return c.emit(wire.Message{Type: "call_ended", Reason: reason})
}

// History returns the turns spoken so far (for tests and diagnostics).
func (c *Call) History() []brain.Turn { return c.history }

func (c *Call) emit(m wire.Message) error {
	c.seq++
	m.Seq = c.seq
	m.At = c.now()
	return c.Sink.Message(m)
}

func (c *Call) append(typ string, data map[string]any) {
	if c.Log != nil {
		_, _ = c.Log.Append(typ, data)
	}
}

func (c *Call) now() time.Time {
	if c.Log != nil && c.Log.Now != nil {
		return c.Log.Now()
	}
	return time.Now().UTC()
}
