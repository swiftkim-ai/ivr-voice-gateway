package call_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// digitsCall builds a call whose brain asks for a digit group on the first
// turn, then echoes whatever the group produced.
func digitsCall(t *testing.T, args map[string]any) (*call.Call, *call.MemorySink, *eventlog.Writer) {
	t.Helper()
	lg, err := eventlog.Create(t.TempDir()+"/call.jsonl", "digits-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lg.Close() })

	sink := &call.MemorySink{}
	c := &call.Call{
		ID: "digits-1",
		Brain: &brain.Scripted{Rules: []brain.Rule{
			{Match: "digits:", Say: []string{"Thank you."}},
			{Match: "*", Say: []string{"Please enter your number."},
				Actions: []control.Action{{Kind: control.ReadDigits, Args: args}}},
		}},
		Speaker:  brain.Silence{FramesPerSentence: 1},
		Sink:     sink,
		Log:      lg,
		Redactor: call.NewRedactor("test-salt"),
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Respond(context.Background()); err != nil { // greeting arms the group
		t.Fatal(err)
	}
	return c, sink, lg
}

func press(t *testing.T, c *call.Call, digits string) {
	t.Helper()
	for _, d := range digits {
		if err := c.Digit(context.Background(), string(d), false); err != nil {
			t.Fatalf("digit %q: %v", d, err)
		}
	}
}

// lastOfType returns the last message of the given type.
func lastOfType(msgs []wire.Message, typ string) (wire.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Type == typ {
			return msgs[i], true
		}
	}
	return wire.Message{}, false
}

func eventsOfType(t *testing.T, lg *eventlog.Writer, typ string) []eventlog.Event {
	t.Helper()
	all, err := eventlog.Read(lg.Path())
	if err != nil {
		t.Fatal(err)
	}
	var out []eventlog.Event
	for _, e := range all {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestReadDigitsCollectsByCount(t *testing.T) {
	c, sink, lg := digitsCall(t, map[string]any{"label": "account", "count": 5})

	press(t, c, "1234")
	if !c.Collecting() {
		t.Fatal("group closed before the requested count was reached")
	}
	if _, ok := lastOfType(sink.Messages(), "digits"); ok {
		t.Fatal("a digits message was sent before the group was complete")
	}
	press(t, c, "5")

	if c.Collecting() {
		t.Fatal("group still open after the requested count")
	}
	msg, ok := lastOfType(sink.Messages(), "digits")
	if !ok {
		t.Fatal("no digits message after the group closed")
	}
	if msg.Digits != "12345" || msg.Count != 5 || msg.Label != "account" {
		t.Fatalf("digits message = %+v", msg)
	}

	// The group is one event, not five.
	if dtmf := eventsOfType(t, lg, "dtmf"); len(dtmf) != 0 {
		t.Fatalf("logged %d per-digit events; a group is reported once", len(dtmf))
	}
	ev := eventsOfType(t, lg, "digits")
	if len(ev) != 1 {
		t.Fatalf("logged %d digits events, want 1", len(ev))
	}
	if ev[0].Data["digits"] != "12345" {
		t.Fatalf("logged digits = %v", ev[0].Data["digits"])
	}
}

func TestReadDigitsEndsOnTerminator(t *testing.T) {
	c, sink, _ := digitsCall(t, map[string]any{"terminator": "#"})

	press(t, c, "987#")
	if c.Collecting() {
		t.Fatal("terminator did not close the group")
	}
	msg, _ := lastOfType(sink.Messages(), "digits")
	if msg.Digits != "987" {
		t.Fatalf("digits = %q, want %q (the terminator is not part of the group)", msg.Digits, "987")
	}
	if msg.Count != 3 {
		t.Fatalf("count = %d, want 3", msg.Count)
	}
}

func TestReadDigitsDefaultsToHashTerminator(t *testing.T) {
	// Neither a count nor a terminator was given; without a default the call
	// would hang on the caller's first digit.
	c, sink, _ := digitsCall(t, map[string]any{"label": "account"})

	press(t, c, "42#")
	if c.Collecting() {
		t.Fatal("group never closed; no usable default terminator")
	}
	msg, _ := lastOfType(sink.Messages(), "digits")
	if msg.Digits != "42" {
		t.Fatalf("digits = %q", msg.Digits)
	}
}

func TestSensitiveDigitsNeverReachTheLogOrTheBrain(t *testing.T) {
	const pan = "4111111111111111"
	c, sink, lg := digitsCall(t, map[string]any{
		"label": "card", "count": len(pan), "sensitive": true,
	})

	press(t, c, pan)

	msg, ok := lastOfType(sink.Messages(), "digits")
	if !ok {
		t.Fatal("no digits message")
	}
	if !msg.Redacted {
		t.Fatal("sensitive group was not marked redacted")
	}
	if msg.Digits != "" {
		t.Fatalf("sensitive group sent digits %q on the wire", msg.Digits)
	}
	if msg.Count != len(pan) {
		t.Fatalf("count = %d, want %d", msg.Count, len(pan))
	}
	if msg.Hash == "" {
		t.Fatal("no correlation token for the redacted group")
	}

	// Nothing anywhere in the log may contain the digits.
	raw, err := readFile(lg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, pan) {
		t.Fatalf("the card number was written to the append-only log:\n%s", raw)
	}
	ev := eventsOfType(t, lg, "digits")
	if len(ev) != 1 {
		t.Fatalf("logged %d digits events, want 1", len(ev))
	}
	if _, present := ev[0].Data["digits"]; present {
		t.Fatal("the log carries a digits field for a redacted group")
	}
	if ev[0].Data["redacted"] != true || ev[0].Data["hash"] == "" {
		t.Fatalf("logged event = %v", ev[0].Data)
	}

	// The brain saw the group's shape, not its contents.
	turns := c.History()
	last := turns[len(turns)-1]
	if strings.Contains(last.Caller, pan) {
		t.Fatalf("the brain was given the card number: %q", last.Caller)
	}
	if !strings.Contains(last.Caller, "redacted") {
		t.Fatalf("brain input = %q, want a redacted surrogate", last.Caller)
	}
}

func TestSensitiveDigitsRefusedWithoutARedactor(t *testing.T) {
	c, sink, lg := digitsCall(t, map[string]any{"count": 4, "sensitive": true})
	c.Redactor = nil // as if the deployment never configured a key

	press(t, c, "1234")

	msg, ok := lastOfType(sink.Messages(), "error")
	if !ok {
		t.Fatal("expected an error when sensitive digits are collected with no key")
	}
	if msg.Code != "no_redactor" {
		t.Fatalf("error code = %q", msg.Code)
	}
	if _, ok := lastOfType(sink.Messages(), "digits"); ok {
		t.Fatal("a digits message was sent despite having no key")
	}
	if raw, _ := readFile(lg.Path()); strings.Contains(raw, "1234") {
		t.Fatal("digits reached the log despite having no key")
	}
}

func TestRedactorTokenIsStableKeyedAndShort(t *testing.T) {
	a := call.NewRedactor("salt-one")
	b := call.NewRedactor("salt-two")

	tok := a.Token("4111111111111111")
	if tok != a.Token("4111111111111111") {
		t.Fatal("token is not stable, so repeat entries cannot be correlated")
	}
	if len(tok) != 16 {
		t.Fatalf("token length = %d, want 16", len(tok))
	}
	if tok == b.Token("4111111111111111") {
		t.Fatal("token does not depend on the key, so the log is brute-forceable")
	}
	if tok == a.Token("4111111111111112") {
		t.Fatal("different digits produced the same token")
	}
}

// readFile returns the raw log text, so a test can assert that a digit string
// appears nowhere in it at all — not in any field, under any key.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
