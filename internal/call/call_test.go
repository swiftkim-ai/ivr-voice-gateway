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

func TestEscalateProducesHandoffMessage(t *testing.T) {
	escalateBrain := &brain.Scripted{Rules: []brain.Rule{
		{Match: "3", Say: []string{"Connecting you to a specialist."},
			Actions: []control.Action{{Kind: control.Escalate, Args: map[string]any{
				"dest":            "300",
				"identity":        "+254722123456",
				"intent":          "billing inquiry",
				"sentiment":       "frustrated",
				"escalation_code": "BILLING",
				"summary":         "Caller wants help with billing.",
				"entities":        map[string]any{"account": "48217"},
				"actions_taken":   []any{"Greeted caller"},
			}}}},
		{Match: "*", Say: []string{"Press 3 for billing."}},
	}}

	ctx := context.Background()
	sink := &call.MemorySink{}
	c := &call.Call{ID: "c-esc", Brain: escalateBrain, Speaker: brain.Silence{}, Sink: sink}
	_ = c.Start()
	_ = c.Respond(ctx)
	if err := c.Digit(ctx, "3", false); err != nil {
		t.Fatal(err)
	}
	_ = c.End("hangup")

	var types []string
	for _, m := range sink.Messages() {
		types = append(types, m.Type)
	}

	// The escalate action emits: action (escalate) + escalation + reply + call_ended
	hasAction := false
	hasEscalation := false
	for _, m := range sink.Messages() {
		if m.Type == "action" && m.Kind == control.Escalate {
			hasAction = true
		}
		if m.Type == "escalation" {
			hasEscalation = true
			if m.Handoff == nil {
				t.Fatal("escalation message has nil handoff")
			}
			if m.Handoff.Identity != "+254722123456" {
				t.Errorf("identity = %q, want +254722123456", m.Handoff.Identity)
			}
			if m.Handoff.Intent != "billing inquiry" {
				t.Errorf("intent = %q, want billing inquiry", m.Handoff.Intent)
			}
			if m.Handoff.Sentiment != "frustrated" {
				t.Errorf("sentiment = %q, want frustrated", m.Handoff.Sentiment)
			}
			if m.Handoff.EscalationCode != "BILLING" {
				t.Errorf("escalation_code = %q, want BILLING", m.Handoff.EscalationCode)
			}
			if m.Handoff.Entities["account"] != "48217" {
				t.Errorf("entities[account] = %q, want 48217", m.Handoff.Entities["account"])
			}
			if len(m.Handoff.ActionsTaken) != 1 || m.Handoff.ActionsTaken[0] != "Greeted caller" {
				t.Errorf("actions_taken = %v, want [Greeted caller]", m.Handoff.ActionsTaken)
			}
		}
	}
	if !hasAction {
		t.Error("expected an escalate action message")
	}
	if !hasEscalation {
		t.Error("expected an escalation message with handoff")
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
