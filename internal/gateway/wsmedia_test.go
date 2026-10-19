package gateway_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/gateway"
	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

func testBrain() brain.Brain {
	return &brain.Scripted{Rules: []brain.Rule{
		{Match: "1", Say: []string{"Connecting you to sales."},
			Actions: []control.Action{{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}}},
		{Match: "*", Say: []string{"Hello. Press 1 for sales."}},
	}}
}

// readType reads until a text message of the wanted type arrives, skipping
// binary audio frames.
func readType(t *testing.T, conn *websocket.Conn, want string) wire.Message {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if mt != websocket.TextMessage {
			continue
		}
		var m wire.Message
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Type == want {
			return m
		}
	}
}

// dialTestGateway starts a gateway and opens one media socket to it.
func dialTestGateway(t *testing.T, opts gateway.Options) (*websocket.Conn, string) {
	t.Helper()
	if opts.LogDir == "" {
		opts.LogDir = t.TempDir()
	}
	srv := httptest.NewServer(gateway.Handler(opts))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/media"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, opts.LogDir
}

func TestMediaCallEndToEnd(t *testing.T) {
	logdir := t.TempDir()
	srv := httptest.NewServer(gateway.Handler(gateway.Options{
		LogDir: logdir, Brain: testBrain(), Speaker: brain.Silence{},
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/media"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(wire.Message{Type: "start", CallID: "c9", Hint: "h"}); err != nil {
		t.Fatal(err)
	}
	if got := readType(t, conn, "call_started"); got.CallID != "c9" {
		t.Fatalf("call_started id = %q", got.CallID)
	}
	if got := readType(t, conn, "reply"); !strings.Contains(got.Text, "Press 1") {
		t.Fatalf("greeting = %q", got.Text)
	}

	if err := conn.WriteJSON(wire.Message{Type: "dtmf", Digit: "1"}); err != nil {
		t.Fatal(err)
	}
	_ = readType(t, conn, "dtmf")
	if got := readType(t, conn, "action"); got.Kind != control.Transfer {
		t.Fatalf("action kind = %q", got.Kind)
	}
	if got := readType(t, conn, "reply"); !strings.Contains(got.Text, "sales") {
		t.Fatalf("dtmf reply = %q", got.Text)
	}

	if err := conn.WriteJSON(wire.Message{Type: "stop"}); err != nil {
		t.Fatal(err)
	}
	if got := readType(t, conn, "call_ended"); got.Reason != "hangup" {
		t.Fatalf("end reason = %q", got.Reason)
	}

	// The call must have been recorded.
	path := filepath.Join(logdir, "c9.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("event log missing: %v", err)
	}
	events, err := eventlog.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Type != "call_started" {
		t.Fatalf("unexpected first event: %+v", events)
	}
	last := events[len(events)-1]
	if last.Type != "call_ended" {
		t.Fatalf("last event = %q, want call_ended", last.Type)
	}
}

func TestWSRejectsAMessageTheContractForbids(t *testing.T) {
	conn, _ := dialTestGateway(t, gateway.Options{Brain: testBrain(), Speaker: brain.Silence{FramesPerSentence: 1}})

	if err := conn.WriteJSON(map[string]any{"type": "start", "call_id": "ws-invalid-1"}); err != nil {
		t.Fatal(err)
	}
	readType(t, conn, "call_started")

	// A digit outside the keypad: the schema's pattern forbids it.
	if err := conn.WriteJSON(map[string]any{"type": "dtmf", "digit": "Z"}); err != nil {
		t.Fatal(err)
	}
	msg := readType(t, conn, "error")
	if msg.Code != "invalid_message" {
		t.Fatalf("error code = %q, want invalid_message", msg.Code)
	}

	// The call survives it: a valid digit still works.
	if err := conn.WriteJSON(map[string]any{"type": "dtmf", "digit": "1"}); err != nil {
		t.Fatal(err)
	}
	if got := readType(t, conn, "reply"); got.Text == "" {
		t.Fatal("the call did not continue after a rejected message")
	}
}

func TestWSRejectsAStartTheContractForbids(t *testing.T) {
	conn, _ := dialTestGateway(t, gateway.Options{Brain: testBrain(), Speaker: brain.Silence{FramesPerSentence: 1}})

	// call_id is required by the contract.
	if err := conn.WriteJSON(map[string]any{"type": "start", "hint": "no id"}); err != nil {
		t.Fatal(err)
	}
	msg := readType(t, conn, "error")
	if msg.Code != "bad_start" {
		t.Fatalf("error code = %q, want bad_start", msg.Code)
	}
}
