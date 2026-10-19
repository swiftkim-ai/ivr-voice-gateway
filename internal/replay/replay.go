// Package replay re-runs a recorded call through the real engine and reports
// where the result diverges from the recording.
//
// This is the eval primitive (ADR-0003): because replay drives the same call
// engine as the live path, a passing replay means the engine still produces the
// same decisions for the same inputs. A production failure is turned into a
// recorded call, and a prompt or rule change is checked against it.
package replay

import (
	"context"
	"fmt"
	"strings"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
)

// Divergence is one place the replayed output differed from the recording.
type Divergence struct {
	Step   int
	Caller string
	Field  string
	Expect string
	Got    string
}

// Report is the outcome of replaying one call.
type Report struct {
	CallID      string
	Turns       int
	Divergences []Divergence
}

// OK reports whether the replay matched the recording exactly.
func (r Report) OK() bool { return len(r.Divergences) == 0 }

type step struct {
	caller string
	// group is set when the caller input was a collected digit group rather
	// than speech or a single keypress.
	group      *call.DigitGroup
	expActions []control.Action
	expSay     []string
}

// Run replays the recorded events through b and reports divergences.
func Run(ctx context.Context, events []eventlog.Event, b brain.Brain, sp brain.Speaker) (Report, error) {
	callID, hint := header(events)
	steps := parse(events)

	sink := &call.MemorySink{}
	c := &call.Call{ID: callID, Hint: hint, Brain: b, Speaker: sp, Sink: sink}

	rep := Report{CallID: callID}
	if err := c.Start(); err != nil {
		return rep, err
	}
	// The first step is the greeting, produced with no caller input.
	if len(steps) > 0 {
		sink.Reset()
		if err := c.Respond(ctx); err != nil {
			return rep, err
		}
		rep.Turns++
		compare(&rep, 0, steps[0], sink)
	}

	for i := 1; i < len(steps); i++ {
		sink.Reset()
		if err := feed(ctx, c, steps[i]); err != nil {
			return rep, err
		}
		rep.Turns++
		compare(&rep, i, steps[i], sink)
	}
	return rep, nil
}

// feed gives the engine one recorded caller input, by the same route it
// arrived on live.
func feed(ctx context.Context, c *call.Call, st step) error {
	switch {
	case st.group != nil:
		// A group is replayed whole: the recording holds it as one event, and
		// a redacted group has no digits left to press.
		return c.Digits(ctx, *st.group)
	case strings.HasPrefix(st.caller, "dtmf:"):
		return c.Digit(ctx, strings.TrimPrefix(st.caller, "dtmf:"), false)
	default:
		return c.Input(ctx, st.caller)
	}
}

// header pulls the call id and hint from the opening event.
func header(events []eventlog.Event) (callID, hint string) {
	for _, ev := range events {
		if ev.Type == "call_started" {
			return ev.CallID, str(ev.Data["hint"])
		}
	}
	return "", ""
}

// parse turns the event stream into ordered steps: each caller input (or the
// leading greeting) with the actions and replies the engine produced for it.
func parse(events []eventlog.Event) []step {
	var steps []step
	cur := -1
	ensure := func() {
		if cur < 0 {
			steps = append(steps, step{})
			cur = len(steps) - 1
		}
	}
	for _, ev := range events {
		switch ev.Type {
		case "transcript":
			steps = append(steps, step{caller: str(ev.Data["text"])})
			cur = len(steps) - 1
		case "dtmf":
			steps = append(steps, step{caller: "dtmf:" + str(ev.Data["digit"])})
			cur = len(steps) - 1
		case "digits":
			g := call.DigitGroup{
				Label:    str(ev.Data["label"]),
				Digits:   str(ev.Data["digits"]),
				Count:    toInt(ev.Data["count"]),
				Redacted: ev.Data["redacted"] == true,
				Hash:     str(ev.Data["hash"]),
			}
			caller := "digits:" + g.Digits
			if g.Redacted {
				caller = fmt.Sprintf("digits:<redacted:%d>", g.Count)
			}
			steps = append(steps, step{caller: caller, group: &g})
			cur = len(steps) - 1
		case "reply":
			ensure()
			steps[cur].expSay = append(steps[cur].expSay, str(ev.Data["text"]))
		case "action":
			ensure()
			steps[cur].expActions = append(steps[cur].expActions, control.Action{
				Kind: str(ev.Data["kind"]),
				Args: asMap(ev.Data["args"]),
			})
		}
	}
	return steps
}

// compare checks what the engine produced against what was recorded.
func compare(rep *Report, idx int, st step, sink *call.MemorySink) {
	var gotSay []string
	var gotActions []control.Action
	for _, m := range sink.Messages() {
		switch m.Type {
		case "reply":
			gotSay = append(gotSay, m.Text)
		case "action":
			gotActions = append(gotActions, control.Action{Kind: m.Kind, Args: m.Args})
		}
	}

	if len(gotActions) != len(st.expActions) {
		rep.Divergences = append(rep.Divergences, Divergence{
			Step: idx, Caller: st.caller, Field: "actions.count",
			Expect: fmt.Sprint(len(st.expActions)), Got: fmt.Sprint(len(gotActions)),
		})
	}
	for i := range st.expActions {
		if i >= len(gotActions) {
			break
		}
		if st.expActions[i].Kind != gotActions[i].Kind {
			rep.Divergences = append(rep.Divergences, Divergence{
				Step: idx, Caller: st.caller, Field: fmt.Sprintf("action[%d].kind", i),
				Expect: st.expActions[i].Kind, Got: gotActions[i].Kind,
			})
		}
	}

	if len(gotSay) != len(st.expSay) {
		rep.Divergences = append(rep.Divergences, Divergence{
			Step: idx, Caller: st.caller, Field: "reply.count",
			Expect: fmt.Sprint(len(st.expSay)), Got: fmt.Sprint(len(gotSay)),
		})
	}
	for i := range st.expSay {
		if i >= len(gotSay) {
			break
		}
		if st.expSay[i] != gotSay[i] {
			rep.Divergences = append(rep.Divergences, Divergence{
				Step: idx, Caller: st.caller, Field: fmt.Sprintf("reply[%d]", i),
				Expect: st.expSay[i], Got: gotSay[i],
			})
		}
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// toInt reads a count back out of JSON, where numbers decode to float64.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
