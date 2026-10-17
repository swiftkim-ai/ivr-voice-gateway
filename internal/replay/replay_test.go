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
