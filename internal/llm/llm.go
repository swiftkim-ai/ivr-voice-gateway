// Package llm defines the language-model interface for the call brain. A real
// provider (Claude, GPT, a local model via llama.cpp) implements Completer;
// the Recorded implementation replays saved completions for deterministic tests.
package llm

import (
	"context"

	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
)

// Message is one turn in the conversation history sent to the model.
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// ToolDef describes one tool the model may call.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall is one tool invocation the model returned.
type ToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// Response is what the model returned for one completion.
type Response struct {
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Request is everything sent to the model for one turn.
type Request struct {
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

// Completer sends a prompt to a language model and returns its response.
type Completer interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// ToActions converts tool calls to control actions. The tool name is the action
// kind; the arguments pass through. Only valid action kinds are returned.
func ToActions(calls []ToolCall) []control.Action {
	var actions []control.Action
	for _, tc := range calls {
		if control.Valid(tc.Name) {
			actions = append(actions, control.Action{Kind: tc.Name, Args: tc.Args})
		}
	}
	return actions
}
