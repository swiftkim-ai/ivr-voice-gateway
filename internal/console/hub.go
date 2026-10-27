// Package console is the operator console: a WebSocket endpoint that streams
// live call events and escalation handoff packets to human agents. It is
// read-only in Phase 3; the human takes over the call on the switch side.
package console

import (
	"encoding/json"
	"sync"

	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// Hub fans call events out to every connected console client. Goroutine-safe.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

// NewHub returns a ready hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]struct{})}
}

// Register adds a console client.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

// Unregister removes a console client.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast sends a call event to every connected console client. Non-blocking:
// slow clients get dropped rather than stalling the call.
func (h *Hub) Broadcast(callID string, m wire.Message) {
	data, err := json.Marshal(consoleEvent{CallID: callID, Message: m})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.Send(data)
	}
}

// ClientCount returns how many console clients are connected.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// consoleEvent wraps a wire message with the call it belongs to, since the
// console may be watching several calls at once.
type consoleEvent struct {
	CallID  string       `json:"call_id"`
	Message wire.Message `json:"event"`
}
