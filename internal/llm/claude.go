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

const (
	defaultEndpoint  = "https://api.anthropic.com/v1/messages"
	defaultModel     = "claude-sonnet-4-20250514"
	anthropicVersion = "2023-06-01"
)

// ClaudeConfig configures the Claude provider.
type ClaudeConfig struct {
	APIKey    string
	Model     string // defaults to claude-sonnet-4-20250514
	Endpoint  string // defaults to the Anthropic API
	MaxTokens int    // defaults to 1024
}

// Claude implements Completer via the Anthropic Messages API using net/http.
type Claude struct {
	apiKey    string
	model     string
	endpoint  string
	maxTokens int
	client    *http.Client
}

// NewClaude returns a Claude completer. The API key is required.
func NewClaude(cfg ClaudeConfig) *Claude {
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	return &Claude{
		apiKey:    cfg.APIKey,
		model:     model,
		endpoint:  endpoint,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Wire types for the Anthropic Messages API.

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []apiMessage `json:"messages"`
	Tools     []apiTool    `json:"tools,omitempty"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type apiResponse struct {
	Content []apiContentBlock `json:"content"`
	Error   *apiError         `json:"error,omitempty"`
}

type apiContentBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ModelID returns the model string sent to the API.
func (c *Claude) ModelID() string { return c.model }

// Complete sends a completion request to the Anthropic Messages API.
func (c *Claude) Complete(ctx context.Context, req Request) (Response, error) {
	ar := apiRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
	}

	for _, m := range req.Messages {
		if m.Role == "system" {
			ar.System = m.Content
			continue
		}
		ar.Messages = append(ar.Messages, apiMessage{Role: m.Role, Content: m.Content})
	}

	for _, t := range req.Tools {
		schema := t.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		ar.Tools = append(ar.Tools, apiTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}

	body, err := json.Marshal(ar)
	if err != nil {
		return Response{}, fmt.Errorf("claude: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("claude: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("claude: request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("claude: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("claude: API %d: %s", httpResp.StatusCode, respBody)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return Response{}, fmt.Errorf("claude: parse response: %w", err)
	}
	if apiResp.Error != nil {
		return Response{}, fmt.Errorf("claude: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	var resp Response
	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			if resp.Text != "" {
				resp.Text += " "
			}
			resp.Text += block.Text
		case "tool_use":
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				Name: block.Name,
				Args: block.Input,
			})
		}
	}
	return resp, nil
}
