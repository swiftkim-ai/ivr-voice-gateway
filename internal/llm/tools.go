package llm

// DefaultTools returns tool definitions for every control action the call
// engine supports. These are passed to the LLM so it can invoke them as
// tool calls; ToActions maps the calls back to control.Action values.
func DefaultTools() []ToolDef {
	return []ToolDef{
		{
			Name:        "send_dtmf",
			Description: "Press a DTMF digit on the caller's phone line.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"digit"},
				"properties": map[string]any{
					"digit": map[string]any{
						"type":        "string",
						"description": "The digit to send (0-9, *, #).",
					},
				},
			},
		},
		{
			Name:        "route",
			Description: "Route the call to a dialplan destination on the switch.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"dest"},
				"properties": map[string]any{
					"dest": map[string]any{
						"type":        "string",
						"description": "The extension or destination to route to.",
					},
				},
			},
		},
		{
			Name:        "transfer",
			Description: "Transfer the call to another extension or number.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"dest"},
				"properties": map[string]any{
					"dest": map[string]any{
						"type":        "string",
						"description": "The extension or number to transfer to.",
					},
				},
			},
		},
		{
			Name:        "play_prompt",
			Description: "Play a pre-recorded prompt or phrase on the switch.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"file"},
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "The prompt file name or phrase to play.",
					},
				},
			},
		},
		{
			Name:        "park",
			Description: "Park the call for later pickup.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "hangup",
			Description: "End the call.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "read_digits",
			Description: "Ask the caller to enter digits on the keypad. Use for account numbers, PINs, or other numeric input.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"label"},
				"properties": map[string]any{
					"label": map[string]any{
						"type":        "string",
						"description": "What is being collected (e.g. 'account', 'pin').",
					},
					"terminator": map[string]any{
						"type":        "string",
						"description": "The digit that ends input (default '#').",
					},
				},
			},
		},
		{
			Name:        "escalate",
			Description: "Escalate the call to a human agent with a structured handoff packet.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"summary"},
				"properties": map[string]any{
					"dest": map[string]any{
						"type":        "string",
						"description": "Extension of the human agent to transfer to.",
					},
					"identity": map[string]any{
						"type":        "string",
						"description": "Who the caller is (name, account, phone).",
					},
					"intent": map[string]any{
						"type":        "string",
						"description": "What the caller wants.",
					},
					"sentiment": map[string]any{
						"type":        "string",
						"description": "Caller mood: calm, frustrated, angry, etc.",
					},
					"escalation_code": map[string]any{
						"type":        "string",
						"description": "Category code: BILLING, TECHNICAL, COMPLAINT, etc.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Brief summary of the conversation so far.",
					},
				},
			},
		},
	}
}
