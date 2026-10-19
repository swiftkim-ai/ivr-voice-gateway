// Package gateway adapts the call engine to a transport. The WebSocket adapter
// is the v1 contract: binary frames carry PCM audio, text frames carry the JSON
// messages in internal/wire. Phone-system adapters (AudioSocket, Twilio, a
// generic IVR hook) implement the same call.Sink in later phases.
package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"

	"github.com/gorilla/websocket"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// Options configures the HTTP handler.
type Options struct {
	// LogDir is where per-call event logs are written (one JSONL per call).
	LogDir string
	// Brain and Speaker are the shared decision and speech components.
	Brain   brain.Brain
	Speaker brain.Speaker
	// Redactor keys the token standing in for a sensitive digit group.
	Redactor *call.Redactor
}

// Handler returns the HTTP routes: the media WebSocket and a health check.
func Handler(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/media", func(w http.ResponseWriter, r *http.Request) {
		serveCall(opts, w, r)
	})
	return mux
}

// upgrader accepts any origin: this endpoint faces other servers (an IVR, a
// test harness), not a browser, so origin checks do not apply.
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func serveCall(opts Options, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the error
	}
	defer conn.Close()

	conn.SetReadLimit(1 << 20)

	ctx := context.Background()

	// The first text message must be "start".
	mt, data, err := conn.ReadMessage()
	if err != nil || mt != websocket.TextMessage {
		_ = conn.WriteJSON(wire.Message{Type: "error", Code: "expected_start", ErrMessage: "first message must be start"})
		return
	}
	var start wire.Message
	if err := json.Unmarshal(data, &start); err != nil || start.Type != "start" {
		_ = conn.WriteJSON(wire.Message{Type: "error", Code: "bad_start", ErrMessage: "malformed start"})
		return
	}

	callID := start.CallID
	if callID == "" {
		callID = r.URL.Query().Get("call_id")
	}
	hint := start.Hint
	if hint == "" {
		hint = r.URL.Query().Get("hint")
	}

	var logw *eventlog.Writer
	if opts.LogDir != "" {
		path := filepath.Join(opts.LogDir, callID+".jsonl")
		if lg, err := eventlog.Create(path, callID); err == nil {
			logw = lg
			defer lg.Close()
		} else {
			log.Printf("gateway: log create: %v", err)
		}
	}

	c := &call.Call{
		ID: callID, Hint: hint,
		Brain: opts.Brain, Speaker: opts.Speaker,
		Sink: wsSink{conn: conn}, Log: logw, Redactor: opts.Redactor,
	}

	if err := c.Start(); err != nil {
		return
	}
	if err := c.Respond(ctx); err != nil { // greeting
		return
	}

	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			_ = c.End("disconnect")
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			// Caller audio. Phase 0 has no STT, so it is accepted and ignored;
			// Phase 4 runs it through the transcriber.
		case websocket.TextMessage:
			var m wire.Message
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			switch m.Type {
			case "dtmf":
				if err := c.Digit(ctx, m.Digit, m.Sensitive); err != nil {
					return
				}
			case "stop":
				_ = c.End("hangup")
				return
			}
		}
	}
}

// wsSink writes contract messages and audio frames to the socket.
type wsSink struct{ conn *websocket.Conn }

func (s wsSink) Message(m wire.Message) error { return s.conn.WriteJSON(m) }

func (s wsSink) Audio(pcm []byte) error {
	return s.conn.WriteMessage(websocket.BinaryMessage, pcm)
}
