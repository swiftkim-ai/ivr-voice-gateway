package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClaudeTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("api key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Errorf("version = %q, want %s", got, anthropicVersion)
		}

		body, _ := io.ReadAll(r.Body)
		var req apiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.System != "You are a helpful agent." {
			t.Errorf("system = %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("messages = %+v", req.Messages)
		}

		json.NewEncoder(w).Encode(apiResponse{
			Content: []apiContentBlock{
				{Type: "text", Text: "Hello, how can I help?"},
			},
		})
	}))
	defer srv.Close()

	c := NewClaude(ClaudeConfig{
		APIKey:   "test-key",
		Endpoint: srv.URL,
	})

	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "You are a helpful agent."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hello, how can I help?" {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("unexpected tool calls: %v", resp.ToolCalls)
	}
}

func TestClaudeToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse{
			Content: []apiContentBlock{
				{Type: "text", Text: "Transferring you now."},
				{Type: "tool_use", Name: "transfer", Input: map[string]any{"dest": "200"}},
			},
		})
	}))
	defer srv.Close()

	c := NewClaude(ClaudeConfig{APIKey: "k", Endpoint: srv.URL})
	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "transfer me"}},
		Tools:    DefaultTools(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Transferring you now." {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "transfer" {
		t.Errorf("tool name = %q", resp.ToolCalls[0].Name)
	}
	if dest, _ := resp.ToolCalls[0].Args["dest"].(string); dest != "200" {
		t.Errorf("dest = %q", dest)
	}
}

func TestClaudeAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := NewClaude(ClaudeConfig{APIKey: "bad", Endpoint: srv.URL})
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestClaudeRequestFormat(t *testing.T) {
	var captured apiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(apiResponse{
			Content: []apiContentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer srv.Close()

	c := NewClaude(ClaudeConfig{
		APIKey:    "k",
		Endpoint:  srv.URL,
		Model:     "claude-haiku-3-20240307",
		MaxTokens: 512,
	})

	tools := []ToolDef{{Name: "hangup", Description: "End the call."}}
	c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "bye"},
			{Role: "assistant", Content: "goodbye"},
			{Role: "user", Content: "thanks"},
		},
		Tools: tools,
	})

	if captured.Model != "claude-haiku-3-20240307" {
		t.Errorf("model = %q", captured.Model)
	}
	if captured.MaxTokens != 512 {
		t.Errorf("max_tokens = %d", captured.MaxTokens)
	}
	if captured.System != "sys" {
		t.Errorf("system = %q", captured.System)
	}
	if len(captured.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (system excluded)", len(captured.Messages))
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Name != "hangup" {
		t.Errorf("tools = %+v", captured.Tools)
	}
}

func TestDefaultToolsCoversAllActions(t *testing.T) {
	tools := DefaultTools()
	names := make(map[string]bool)
	for _, td := range tools {
		names[td.Name] = true
	}
	want := []string{
		"send_dtmf", "route", "transfer", "play_prompt",
		"park", "hangup", "read_digits", "escalate",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("DefaultTools missing %q", w)
		}
	}
}
