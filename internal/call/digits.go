package call

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// digitGroup is an in-flight read_digits collection. Digits pressed while one is
// open belong to it: they do not reach the brain individually, and a sensitive
// group never reaches it at all.
type digitGroup struct {
	label      string
	want       int    // digits expected; 0 means "until the terminator"
	terminator string // digit that ends the group early; "" means none
	sensitive  bool
	got        []byte
}

// arm starts collecting digits, as asked for by a read_digits action.
//
// args: label (string), count (number), terminator (string), sensitive (bool).
func (c *Call) arm(args map[string]any) {
	g := &digitGroup{
		label:      argStr(args, "label"),
		want:       argInt(args, "count"),
		terminator: argStr(args, "terminator"),
		sensitive:  argBool(args, "sensitive"),
	}
	if g.want <= 0 && g.terminator == "" {
		// Without either, the group could never close and the call would hang
		// on the caller's first digit. "#" is the convention callers expect.
		g.terminator = "#"
	}
	c.collect = g
}

// collectDigit adds one digit to the open group and reports whether the group
// is now complete.
func (c *Call) collectDigit(digit string) bool {
	g := c.collect
	if g.terminator != "" && digit == g.terminator {
		return true
	}
	g.got = append(g.got, digit...)
	return g.want > 0 && len(g.got) >= g.want
}

// DigitGroup is a completed collection of keypad digits. The live path builds
// one as the caller types; replay reads one back out of a recording.
type DigitGroup struct {
	// Label is what was collected, from the read_digits action.
	Label string
	// Digits is what the caller entered. Empty when Redacted.
	Digits string
	// Count is how many digits were entered, which survives redaction.
	Count int
	// Redacted marks a group whose digits were never kept.
	Redacted bool
	// Hash is the correlation token standing in for a redacted group.
	Hash string
}

// Digits feeds an already-collected group into the call. Replay uses it: a
// recording holds the group as one event, and a redacted group holds no digits
// to press again, so the group is handed back whole.
func (c *Call) Digits(ctx context.Context, g DigitGroup) error {
	c.collect = nil
	return c.emitGroup(ctx, g)
}

// finishDigits closes the open group: it records one event for the whole group,
// tells the client, and gives the brain its turn.
//
// A sensitive group is reduced here, at the engine, to a count and a keyed
// token. The digits are never written to the append-only log, which cannot be
// rewritten later, and never reach the brain. Replay stays deterministic
// because the brain is fed the same surrogate the recording holds (ADR-0003).
func (c *Call) finishDigits(ctx context.Context) error {
	g := c.collect
	c.collect = nil

	digits := string(g.got)
	group := DigitGroup{Label: g.label, Count: len(g.got)}
	switch {
	case !g.sensitive:
		group.Digits = digits

	case c.Redactor == nil:
		// Refuse rather than write sensitive digits in the clear.
		const msg = "sensitive digits collected without a redaction key configured"
		c.append("error", map[string]any{"code": "no_redactor", "message": msg})
		return c.emit(wire.Message{Type: "error", Code: "no_redactor", ErrMessage: msg})

	default:
		group.Redacted = true
		group.Hash = c.Redactor.Token(digits)
	}
	return c.emitGroup(ctx, group)
}

// emitGroup records the group, tells the client, and gives the brain its turn.
// It is the one place a group becomes a decision, so the live path and replay
// cannot drift apart.
func (c *Call) emitGroup(ctx context.Context, g DigitGroup) error {
	data := map[string]any{"count": g.Count}
	msg := wire.Message{Type: "digits", Count: g.Count}
	if g.Label != "" {
		data["label"] = g.Label
		msg.Label = g.Label
	}

	var surrogate string
	if g.Redacted {
		data["redacted"] = true
		msg.Redacted = true
		if g.Hash != "" {
			data["hash"] = g.Hash
			msg.Hash = g.Hash
		}
		surrogate = fmt.Sprintf("digits:<redacted:%d>", g.Count)
	} else {
		data["digits"] = g.Digits
		msg.Digits = g.Digits
		surrogate = "digits:" + g.Digits
	}

	c.append("digits", data)
	if err := c.emit(msg); err != nil {
		return err
	}
	c.pending = surrogate
	return c.Respond(ctx)
}

// Redactor turns a sensitive digit group into a correlation token.
//
// The token is a keyed HMAC, not a bare hash: digit groups are short enough
// that an unkeyed hash of a PIN or card number is recoverable by brute force.
// It identifies a repeat entry across calls; it is not a secret, and the key
// never goes near the event log.
type Redactor struct{ key []byte }

// NewRedactor keys a redactor from a deployment secret, which must not be
// empty. Callers read it from configuration, never from the log directory.
func NewRedactor(salt string) *Redactor {
	return &Redactor{key: []byte(salt)}
}

// Token returns the correlation token for digits: 16 hex characters, enough to
// match repeat entries without inviting use as an identifier.
func (r *Redactor) Token(digits string) string {
	mac := hmac.New(sha256.New, r.key)
	_, _ = mac.Write([]byte(digits))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

func argStr(args map[string]any, name string) string {
	if v, ok := args[name].(string); ok {
		return v
	}
	return ""
}

func argBool(args map[string]any, name string) bool {
	if v, ok := args[name].(bool); ok {
		return v
	}
	return false
}

// argInt accepts the float64 a JSON number decodes to, as well as int and a
// numeric string, so an action is read the same whether it came from a model,
// a recording, or a config file.
func argInt(args map[string]any, name string) int {
	switch v := args[name].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return 0
}
