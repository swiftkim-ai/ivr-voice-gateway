// Package wire defines the JSON control/event messages of the v1 contract.
//
// Audio never travels here: it is sent as binary frames alongside these messages.
// The field set mirrors schema/ivrvg.v1.schema.json; keep the two in step.
package wire

import (
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
)

// Message is one JSON text frame in either direction. Fields are optional by
// type, exactly as the schema's oneOf describes; omitempty keeps the wire thin.
type Message struct {
	Type string    `json:"type"`
	Seq  int       `json:"seq"`
	At   time.Time `json:"at"`

	// call_started / start
	CallID string `json:"call_id,omitempty"`
	Hint   string `json:"hint,omitempty"`

	// transcript / reply
	Text  string `json:"text,omitempty"`
	Final *bool  `json:"final,omitempty"`

	// dtmf
	Digit     string `json:"digit,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`

	// digits (a completed read_digits group)
	Label    string `json:"label,omitempty"`
	Digits   string `json:"digits,omitempty"`
	Count    int    `json:"count,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
	Hash     string `json:"hash,omitempty"`

	// action
	Kind string         `json:"kind,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	// escalation
	Handoff *control.Handoff `json:"handoff,omitempty"`

	// call_ended / error
	Reason     string `json:"reason,omitempty"`
	Code       string `json:"code,omitempty"`
	ErrMessage string `json:"message,omitempty"`
}
