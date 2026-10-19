package replay_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/replay"
)

func testBrain() brain.Brain {
	return &brain.Scripted{Rules: []brain.Rule{
		{Match: "1", Say: []string{"Connecting you to sales."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "*", Say: []string{"Hello. Press 1 for sales."}},
	}}
}

// record runs one scripted call through the real engine and returns its events.
func record(t *testing.T) []eventlog.Event {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c1.jsonl")
	lg, err := eventlog.Create(path, "c1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c := &call.Call{ID: "c1", Hint: "h", Brain: testBrain(), Speaker: brain.Silence{}, Sink: &call.MemorySink{}, Log: lg}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Respond(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Digit(ctx, "1", false); err != nil {
		t.Fatal(err)
	}
	if err := c.Input(ctx, "about my invoice"); err != nil {
		t.Fatal(err)
	}
	if err := c.End("hangup"); err != nil {
		t.Fatal(err)
	}
	if err := lg.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := eventlog.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestReplayIsDeterministic(t *testing.T) {
	events := record(t)
	rep, err := replay.Run(context.Background(), events, testBrain(), brain.Silence{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("expected a clean replay, got divergences: %+v", rep.Divergences)
	}
	if rep.Turns != 3 { // greeting + dtmf + one utterance
		t.Fatalf("turns = %d, want 3", rep.Turns)
	}
}

func TestReplayReportsDivergence(t *testing.T) {
	events := record(t)
	// Corrupt a recorded reply; replay must notice the engine no longer matches.
	for i := range events {
		if events[i].Type == "reply" {
			events[i].Data["text"] = "totally different words"
			break
		}
	}
	rep, err := replay.Run(context.Background(), events, testBrain(), brain.Silence{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("expected a divergence after altering the recording")
	}
}

// digitsBrain asks for a digit group, then reads back whatever came of it.
func digitsBrain(args map[string]any) brain.Brain {
	return &brain.Scripted{Rules: []brain.Rule{
		// First: a collected group contains digits that match the rules below.
		{Match: "digits:", Say: []string{"I have {caller}. Connecting you."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "2", Say: []string{"Enter your number, then press hash."},
			Actions: []control.Action{{Kind: control.ReadDigits, Args: args}}},
		{Match: "*", Say: []string{"Hello. Press 2."}},
	}}
}

// recordDigits runs a call that collects a digit group and returns its events.
func recordDigits(t *testing.T, args map[string]any, entry string) []eventlog.Event {
	t.Helper()
	path := filepath.Join(t.TempDir(), "d1.jsonl")
	lg, err := eventlog.Create(path, "d1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c := &call.Call{
		ID: "d1", Brain: digitsBrain(args), Speaker: brain.Silence{},
		Sink: &call.MemorySink{}, Log: lg, Redactor: call.NewRedactor("test-salt"),
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Respond(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Digit(ctx, "2", false); err != nil { // arms the group
		t.Fatal(err)
	}
	for _, d := range entry {
		if err := c.Digit(ctx, string(d), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.End("hangup"); err != nil {
		t.Fatal(err)
	}
	if err := lg.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := eventlog.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestReplayReproducesADigitGroup(t *testing.T) {
	events := recordDigits(t, map[string]any{"label": "account", "terminator": "#"}, "48217#")

	rep, err := replay.Run(context.Background(), events,
		digitsBrain(map[string]any{"label": "account", "terminator": "#"}), brain.Silence{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("a call with a digit group did not replay: %+v", rep.Divergences)
	}
	// greeting + the digit that armed the group + the group itself
	if rep.Turns != 3 {
		t.Fatalf("turns = %d, want 3", rep.Turns)
	}
}

// A redacted group keeps no digits, so replay has to drive the engine from the
// count alone. If it could not, every call that touches a PIN would be
// unreplayable — and those are the calls most worth replaying.
func TestReplayReproducesARedactedGroup(t *testing.T) {
	args := map[string]any{"label": "pin", "count": 4, "sensitive": true}
	events := recordDigits(t, args, "4821")

	for _, ev := range events {
		if ev.Type == "digits" {
			if _, leaked := ev.Data["digits"]; leaked {
				t.Fatal("the recording kept the digits of a redacted group")
			}
		}
	}

	rep, err := replay.Run(context.Background(), events, digitsBrain(args), brain.Silence{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("a redacted group did not replay: %+v", rep.Divergences)
	}
}

func TestReplayNoticesADigitGroupChangingTheAnswer(t *testing.T) {
	args := map[string]any{"label": "account", "terminator": "#"}
	events := recordDigits(t, args, "48217#")

	// The brain now says something different about the same group.
	changed := &brain.Scripted{Rules: []brain.Rule{
		{Match: "digits:", Say: []string{"That account is closed."}},
		{Match: "2", Say: []string{"Enter your number, then press hash."},
			Actions: []control.Action{{Kind: control.ReadDigits, Args: args}}},
		{Match: "*", Say: []string{"Hello. Press 2."}},
	}}

	rep, err := replay.Run(context.Background(), events, changed, brain.Silence{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("expected a divergence after changing what the brain says about the group")
	}
}
