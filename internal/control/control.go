// Package control defines the actions the agent can take on a call.
//
// Media carriers (AudioSocket, Twilio Media Streams, raw RTP) carry audio only,
// so actions travel on a separate control channel. The engine emits these as
// typed messages; a per-deployment adapter translates them onto the switch's
// control API (AMI, ARI, ESL, REST, or an HTTP callback) — see ADR-0004.
package control

// Action kinds, matching schema/ivrvg.v1.schema.json.
const (
	SendDTMF   = "send_dtmf"   // press a key on the caller's leg
	Route      = "route"       // send the call to a dialplan destination
	Transfer   = "transfer"    // blind or attended transfer
	PlayPrompt = "play_prompt" // play a prompt file or phrase on the switch
	Park       = "park"        // park the call for pickup
	Hangup     = "hangup"      // end the call
	ReadDigits = "read_digits" // collect keypad digits (for sensitive input)
	Escalate   = "escalate"    // hand the call to a human with a structured packet
)

var kinds = map[string]bool{
	SendDTMF: true, Route: true, Transfer: true, PlayPrompt: true,
	Park: true, Hangup: true, ReadDigits: true, Escalate: true,
}

// Handoff is the structured packet the brain produces when escalating a call
// to a human agent. It carries everything the human needs to continue without
// asking the caller to repeat themselves.
type Handoff struct {
	Identity       string            `json:"identity,omitempty"`
	Intent         string            `json:"intent,omitempty"`
	Entities       map[string]string `json:"entities,omitempty"`
	ActionsTaken   []string          `json:"actions_taken,omitempty"`
	Sentiment      string            `json:"sentiment,omitempty"`
	EscalationCode string            `json:"escalation_code,omitempty"`
	Summary        string            `json:"summary,omitempty"`
}

// Action is one instruction for the call, with adapter-specific arguments.
type Action struct {
	Kind string         `json:"kind"`
	Args map[string]any `json:"args,omitempty"`
}

// Originate describes an outbound call request.
type Originate struct {
	Channel     string
	Exten       string
	Context     string
	Priority    int
	CallerID    string
	Timeout     int
	Async       bool
	Application string
	Data        string
	Variables   map[string]string
	CallID      string
	Hint        string
}

// Valid reports whether kind is one of defined actions.
func Valid(kind string) bool { return kinds[kind] }
