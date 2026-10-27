package llm

import (
	"context"
	"testing"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
)

func TestBrainGreeting(t *testing.T) {
	comp := &Recorded{Responses: []Response{
		{Text: "Hello! How can I help you today?"},
	}}
	b := NewBrain(BrainConfig{
		SystemPrompt: "You are a helpful IVR agent.",
		Completer:    comp,
	})

	reply, err := b.Respond(context.Background(), brain.Input{Hint: "+254722000000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Sentences) != 1 || reply.Sentences[0] != "Hello! How can I help you today?" {
		t.Fatalf("unexpected sentences: %v", reply.Sentences)
	}
}

func TestBrainToolCalls(t *testing.T) {
	comp := &Recorded{Responses: []Response{
		{Text: "Hello!"},
		{
			Text:      "Transferring you now.",
			ToolCalls: []ToolCall{{Name: "transfer", Args: map[string]any{"dest": "200"}}},
		},
	}}
	b := NewBrain(BrainConfig{
		SystemPrompt: "You are a helpful IVR agent.",
		Completer:    comp,
		Tools: []ToolDef{
			{Name: "transfer", Description: "Transfer the call"},
		},
	})

	// Greeting
	reply, err := b.Respond(context.Background(), brain.Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Sentences) != 1 {
		t.Fatalf("expected 1 greeting sentence, got %d", len(reply.Sentences))
	}

	// Caller input -> tool call
	reply, err = b.Respond(context.Background(), brain.Input{
		Caller:  "Transfer me to sales",
		History: []brain.Turn{{Agent: reply.Sentences}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Actions) != 1 || reply.Actions[0].Kind != "transfer" {
		t.Fatalf("expected transfer action, got %v", reply.Actions)
	}
}

func TestBrainInvalidToolCallsFiltered(t *testing.T) {
	comp := &Recorded{Responses: []Response{
		{ToolCalls: []ToolCall{
			{Name: "transfer", Args: map[string]any{"dest": "200"}},
			{Name: "nonexistent_tool", Args: map[string]any{}},
		}},
	}}
	b := NewBrain(BrainConfig{Completer: comp})

	reply, err := b.Respond(context.Background(), brain.Input{Caller: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Actions) != 1 {
		t.Fatalf("expected 1 valid action, got %d: %v", len(reply.Actions), reply.Actions)
	}
}

func TestRecordedExhausted(t *testing.T) {
	comp := &Recorded{Responses: []Response{{Text: "one"}}}
	if _, err := comp.Complete(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := comp.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected error when fixture exhausted")
	}
}
