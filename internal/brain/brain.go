// Package brain holds the decision-making side of a call: the interface a real
// model will implement, a deterministic scripted implementation used by Phase 0
// and tests, and the tool registry the model calls to act on the call.
package brain

import (
	"context"
	"strings"

	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/media"
)

// Turn is one exchange already spoken on the call, replayed back to the brain
// as context. Caller is empty for the opening greeting turn.
type Turn struct {
	Caller string   `json:"caller"`
	Agent  []string `json:"agent"`
}

// Input is everything the brain needs to produce the next turn.
type Input struct {
	Hint    string // IVR context (caller id, menu choice, account)
	Caller  string // recognised caller speech, or "dtmf:<digit>", or "" for the greeting
	History []Turn
}

// Reply is the brain's next move: sentences to speak and actions to take.
type Reply struct {
	Sentences []string
	Actions   []control.Action
}

// Brain decides what to say and do. A real STT→LLM→TTS pipeline implements it in
// Phase 4; the scripted brain implements it now.
type Brain interface {
	Respond(ctx context.Context, in Input) (Reply, error)
}

// Rule maps an input to a canned reply. Match is a case-insensitive substring of
// the caller text; "*" matches anything (used last). Rules are checked in order.
type Rule struct {
	Match   string
	Say     []string
	Actions []control.Action
}

// Scripted is a deterministic Brain driven by ordered rules. It is what makes
// Phase 0 replayable and what stands in for a model until Phase 4.
type Scripted struct {
	Rules []Rule
}

// Respond returns the first rule whose Match applies.
func (s *Scripted) Respond(_ context.Context, in Input) (Reply, error) {
	text := strings.ToLower(in.Caller)
	for _, r := range s.Rules {
		if r.Match == "*" || (r.Match != "" && strings.Contains(text, strings.ToLower(r.Match))) {
			return Reply{Sentences: r.Say, Actions: r.Actions}, nil
		}
	}
	return Reply{Sentences: []string{"Sorry, I did not catch that."}}, nil
}

// Speaker turns text into PCM frames for the caller to hear. The real TTS
// providers implement it in Phase 4; Silence stands in until then so the media
// path is exercised end to end.
type Speaker interface {
	Frames(text string) [][]byte
}

// Silence is a Speaker that returns fixed-size silence. It is the Phase 0
// placeholder for TTS: enough to drive the audio path deterministically.
type Silence struct {
	FrameBytes        int // defaults to media.FrameBytes
	FramesPerSentence int // defaults to 25 (0.5 s per sentence)
}

// Frames ignores text and returns a fixed burst of silence.
func (s Silence) Frames(_ string) [][]byte {
	fb := s.FrameBytes
	if fb <= 0 {
		fb = media.FrameBytes
	}
	n := s.FramesPerSentence
	if n <= 0 {
		n = 25
	}
	out := make([][]byte, n)
	for i := range out {
		out[i] = make([]byte, fb)
	}
	return out
}
