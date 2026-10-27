package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
)

// BrainConfig configures the LLM-backed brain.
type BrainConfig struct {
	// SystemPrompt is the system instruction sent at the start of every
	// completion. It should describe the agent's role, constraints, and the
	// tools available to it.
	SystemPrompt string

	// Completer is the language model to call.
	Completer Completer

	// Tools are the actions the model may invoke as tool calls.
	Tools []ToolDef
}

// Brain is a brain.Brain backed by an LLM. It translates the call engine's
// Input into an LLM request, maps tool calls to control actions, and returns
// the text the agent should speak.
type Brain struct {
	cfg BrainConfig
}

// NewBrain returns an LLM-backed brain.
func NewBrain(cfg BrainConfig) *Brain {
	return &Brain{cfg: cfg}
}

// Respond implements brain.Brain.
func (b *Brain) Respond(ctx context.Context, in brain.Input) (brain.Reply, error) {
	msgs := b.buildMessages(in)
	req := Request{Messages: msgs, Tools: b.cfg.Tools}

	resp, err := b.cfg.Completer.Complete(ctx, req)
	if err != nil {
		return brain.Reply{}, fmt.Errorf("llm brain: %w", err)
	}

	var sentences []string
	if resp.Text != "" {
		sentences = splitSentences(resp.Text)
	}

	actions := ToActions(resp.ToolCalls)

	return brain.Reply{Sentences: sentences, Actions: actions}, nil
}

func (b *Brain) buildMessages(in brain.Input) []Message {
	var msgs []Message

	system := b.cfg.SystemPrompt
	if in.Hint != "" {
		system += fmt.Sprintf("\n\nCaller context: %s", in.Hint)
	}
	msgs = append(msgs, Message{Role: "system", Content: system})

	for _, t := range in.History {
		if t.Caller != "" {
			msgs = append(msgs, Message{Role: "user", Content: t.Caller})
		}
		if len(t.Agent) > 0 {
			msgs = append(msgs, Message{Role: "assistant", Content: strings.Join(t.Agent, " ")})
		}
	}

	if in.Caller != "" {
		msgs = append(msgs, Message{Role: "user", Content: in.Caller})
	} else if len(in.History) == 0 {
		msgs = append(msgs, Message{Role: "user", Content: "[Call connected. Greet the caller.]"})
	}

	return msgs
}

func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return []string{text}
}
