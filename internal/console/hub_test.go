package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

func TestHubBroadcastsToConnectedClients(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	// Wait for the client to register.
	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}

	hub.Broadcast("call-001", wire.Message{
		Type: "reply", Seq: 1, Text: "Hello from the agent.",
	})

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var ev consoleEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.CallID != "call-001" {
		t.Errorf("call_id = %q, want call-001", ev.CallID)
	}
	if ev.Message.Type != "reply" {
		t.Errorf("event type = %q, want reply", ev.Message.Type)
	}
	if ev.Message.Text != "Hello from the agent." {
		t.Errorf("text = %q, want the greeting", ev.Message.Text)
	}
}

func TestHubBroadcastsEscalation(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	handoff := &control.Handoff{
		Identity:       "+254722123456",
		Intent:         "billing inquiry",
		Sentiment:      "frustrated",
		EscalationCode: "BILLING",
		Summary:        "Caller wants to dispute a charge on account 48217.",
	}
	hub.Broadcast("call-002", wire.Message{
		Type: "escalation", Seq: 5, Handoff: handoff,
	})

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var ev consoleEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Message.Type != "escalation" {
		t.Errorf("type = %q, want escalation", ev.Message.Type)
	}
	if ev.Message.Handoff == nil {
		t.Fatal("handoff is nil")
	}
	if ev.Message.Handoff.EscalationCode != "BILLING" {
		t.Errorf("escalation_code = %q, want BILLING", ev.Message.Handoff.EscalationCode)
	}
	if ev.Message.Handoff.Identity != "+254722123456" {
		t.Errorf("identity = %q, want +254722123456", ev.Message.Handoff.Identity)
	}
}

func TestHubUnregisterRemovesClient(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}

	ws.Close()

	deadline = time.Now().Add(2 * time.Second)
	for hub.ClientCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients after close, got %d", hub.ClientCount())
	}
}

// suppress unused import
var _ = http.StatusOK
