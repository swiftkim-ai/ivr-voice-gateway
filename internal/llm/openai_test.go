package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAITextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}

		body, _ := io.ReadAll(r.Body)
		var req oaiRequest
		json.Unmarshal(body, &req)

		if req.Model != "gpt-4o" {
			t.Errorf("model = %q", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Errorf("messages = %d", len(req.Messages))
		}

		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{
				Message: oaiAssistantMessage{Content: "Hello there!"},
			}},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(OpenAIConfig{
		APIKey:   "test-key",
		Model:    "gpt-4o",
		Endpoint: srv.URL,
	})

	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hello there!" {
		t.Errorf("text = %q", resp.Text)
	}
}

func TestOpenAIToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{
				Message: oaiAssistantMessage{
					Content: "Hanging up now.",
					ToolCalls: []oaiToolCall{{
						Function: oaiToolCallFunction{
							Name:      "hangup",
							Arguments: `{}`,
						},
					}},
				},
			}},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(OpenAIConfig{APIKey: "k", Model: "m", Endpoint: srv.URL})
	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "bye"}},
		Tools:    DefaultTools(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hanging up now." {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "hangup" {
		t.Errorf("tool calls = %+v", resp.ToolCalls)
	}
}

func TestOpenAINoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no auth header for local server")
		}
		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{
				Message: oaiAssistantMessage{Content: "ok"},
			}},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(OpenAIConfig{Model: "llama3", Endpoint: srv.URL})
	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "ok" {
		t.Errorf("text = %q", resp.Text)
	}
}

func TestOpenAIAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit","message":"slow down"}}`))
	}))
	defer srv.Close()

	c := NewOpenAI(OpenAIConfig{APIKey: "k", Model: "m", Endpoint: srv.URL})
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
}

func TestOpenAIRequestFormat(t *testing.T) {
	var captured oaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{Message: oaiAssistantMessage{Content: "ok"}}},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(OpenAIConfig{
		APIKey:    "k",
		Model:     "llama-3.3-70b-versatile",
		Endpoint:  srv.URL,
		MaxTokens: 256,
	})

	tools := []ToolDef{{Name: "transfer", Description: "Transfer.", Parameters: map[string]any{
		"type":       "object",
		"required":   []string{"dest"},
		"properties": map[string]any{"dest": map[string]any{"type": "string"}},
	}}}

	c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "connect me"},
		},
		Tools: tools,
	})

	if captured.Model != "llama-3.3-70b-versatile" {
		t.Errorf("model = %q", captured.Model)
	}
	if captured.MaxTokens != 256 {
		t.Errorf("max_tokens = %d", captured.MaxTokens)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Type != "function" {
		t.Errorf("tools = %+v", captured.Tools)
	}
	if captured.Tools[0].Function.Name != "transfer" {
		t.Errorf("tool name = %q", captured.Tools[0].Function.Name)
	}
}
