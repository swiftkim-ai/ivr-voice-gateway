package call_test

import (
	"context"
	"testing"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
)

// testBrain is the deterministic brain shared by these tests.
func testBrain() brain.Brain {
	return &brain.Scripted{Rules: []brain.Rule{
		{Match: "1", Say: []string{"Connecting you to sales."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "*", Say: []string{"Hello. Press 1 for sales."}},
	}}
}

func TestGreetingAndTurn(t *testing.T) {
	ctx := context.Background()
	sink := &call.MemorySink{}
	c := &call.Call{ID: "c1", Hint: "h", Brain: testBrain(), Speaker: brain.Silence{}, Sink: sink}

	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Respond(ctx); err != nil { // greeting
		t.Fatal(err)
	}
	if err := c.Digit(ctx, "1", false); err != nil {
		t.Fatal(err)
	}
	if err := c.End("hangup"); err != nil {
		t.Fatal(err)
	}

	var types []string
	var replies []string
	var actions []string
	for _, m := range sink.Messages() {
		types = append(types, m.Type)
		if m.Type == "reply" {
			replies = append(replies, m.Text)
		}
		if m.Type == "action" {
			actions = append(actions, m.Kind)
		}
	}

	wantTypes := []string{"call_started", "reply", "dtmf", "action", "reply", "call_ended"}
	if len(types) != len(wantTypes) {
		t.Fatalf("message types = %v, want %v", types, wantTypes)
	}
	for i, w := range wantTypes {
		if types[i] != w {
			t.Fatalf("message[%d] = %q, want %q (all: %v)", i, types[i], w, types)
		}
	}
	if len(replies) != 2 || replies[0] != "Hello. Press 1 for sales." || replies[1] != "Connecting you to sales." {
		t.Fatalf("replies = %v", replies)
	}
	if len(actions) != 1 || actions[0] != control.Transfer {
		t.Fatalf("actions = %v", actions)
	}
	if sink.AudioBytes() == 0 {
		t.Fatal("expected audio frames to be emitted")
	}
	if got := len(c.History()); got != 2 {
		t.Fatalf("history = %d turns, want 2", got)
	}
}

func TestSensitiveDigitIsRedactedAndNotAnswered(t *testing.T) {
	ctx := context.Background()
	sink := &call.MemorySink{}
	c := &call.Call{ID: "c2", Brain: testBrain(), Speaker: brain.Silence{}, Sink: sink}
	_ = c.Start()
	_ = c.Respond(ctx) // greeting
	sink.Reset()

	if err := c.Digit(ctx, "4", true); err != nil {
		t.Fatal(err)
	}
	// A sensitive digit is acknowledged on the wire but never answered, so no
	// reply follows it and it never enters history.
	for _, m := range sink.Messages() {
		if m.Type == "reply" {
			t.Fatal("sensitive digit must not trigger a reply")
		}
	}
	if got := len(c.History()); got != 1 {
		t.Fatalf("history = %d, want only the greeting turn", got)
	}
}
