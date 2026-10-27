package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultOpenAIEndpoint = "https://api.openai.com/v1/chat/completions"

// OpenAIConfig configures an OpenAI-compatible provider. Works with OpenAI,
// Groq, Together, Ollama, llama.cpp, or any server that speaks the
// /v1/chat/completions endpoint.
type OpenAIConfig struct {
	APIKey    string
	Model     string // e.g. "gpt-4o", "llama-3.3-70b-versatile", "llama3"
	Endpoint  string // defaults to OpenAI; set to "http://localhost:11434/v1/chat/completions" for Ollama
	MaxTokens int    // defaults to 1024
}

// OpenAI implements Completer via the OpenAI chat completions API.
type OpenAI struct {
	apiKey    string
	model     string
	endpoint  string
	maxTokens int
	client    *http.Client
}

// NewOpenAI returns an OpenAI-compatible completer.
func NewOpenAI(cfg OpenAIConfig) *OpenAI {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultOpenAIEndpoint
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	return &OpenAI{
		apiKey:    cfg.APIKey,
		model:     cfg.Model,
		endpoint:  endpoint,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: 60 * time.Second},
	}
}

// ModelID returns the model string sent to the API.
func (o *OpenAI) ModelID() string { return o.model }

// Wire types for the OpenAI chat completions API.

type oaiRequest struct {
	Model     string       `json:"model"`
	Messages  []oaiMessage `json:"messages"`
	Tools     []oaiTool    `json:"tools,omitempty"`
	MaxTokens int          `json:"max_tokens,omitempty"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Error   *oaiError   `json:"error,omitempty"`
}

type oaiChoice struct {
	Message oaiAssistantMessage `json:"message"`
}

type oaiAssistantMessage struct {
	Content   string        `json:"content"`
	ToolCalls []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiToolCall struct {
	Function oaiToolCallFunction `json:"function"`
}

type oaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Complete sends a completion request to an OpenAI-compatible endpoint.
func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	or := oaiRequest{
		Model:     o.model,
		MaxTokens: o.maxTokens,
	}

	for _, m := range req.Messages {
		or.Messages = append(or.Messages, oaiMessage{Role: m.Role, Content: m.Content})
	}

	for _, t := range req.Tools {
		schema := t.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		or.Tools = append(or.Tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}

	body, err := json.Marshal(or)
	if err != nil {
		return Response{}, fmt.Errorf("openai: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai: request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("openai: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("openai: API %d: %s", httpResp.StatusCode, respBody)
	}

	var apiResp oaiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return Response{}, fmt.Errorf("openai: parse response: %w", err)
	}
	if apiResp.Error != nil {
		return Response{}, fmt.Errorf("openai: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return Response{}, fmt.Errorf("openai: no choices in response")
	}

	msg := apiResp.Choices[0].Message
	var resp Response
	resp.Text = msg.Content

	for _, tc := range msg.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				continue
			}
		}
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			Name: tc.Function.Name,
			Args: args,
		})
	}

	return resp, nil
}
