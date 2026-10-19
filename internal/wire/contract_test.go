package wire_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// fixturePath is the golden fixture both languages validate. The TypeScript
// side reads the same file, so a contract change has to satisfy both.
const fixturePath = "../../testdata/contract/messages.jsonl"

func fixture(t *testing.T) []json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(fixturePath))
	if err != nil {
		t.Fatal(err)
	}
	var out []json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, json.RawMessage(line))
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty", fixturePath)
	}
	return out
}

func TestFixtureMatchesTheSchema(t *testing.T) {
	for i, msg := range fixture(t) {
		if err := wire.Validate(msg); err != nil {
			t.Errorf("line %d: %v\n  %s", i+1, err, msg)
		}
	}
}

// Every message type in the contract needs a fixture, so adding one to the
// schema fails here until both languages have an example to agree on.
func TestFixtureCoversEveryMessageType(t *testing.T) {
	types, err := wire.MessageTypes()
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, msg := range fixture(t) {
		var m struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			t.Fatal(err)
		}
		seen[m.Type] = true
	}

	for _, want := range types {
		if !seen[want] {
			t.Errorf("the contract defines %q but no fixture exercises it", want)
		}
	}
}

// Likewise every field: an optional one nothing exercises is a field neither
// language is proven to carry.
func TestFixtureExercisesEveryField(t *testing.T) {
	types, err := wire.MessageTypes()
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]map[string]bool{}
	for _, msg := range fixture(t) {
		var obj map[string]any
		if err := json.Unmarshal(msg, &obj); err != nil {
			t.Fatal(err)
		}
		kind, _ := obj["type"].(string)
		if seen[kind] == nil {
			seen[kind] = map[string]bool{}
		}
		for name := range obj {
			seen[kind][name] = true
		}
	}

	for _, kind := range types {
		props, err := wire.Properties(kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range props {
			if !seen[kind][name] {
				t.Errorf("%s.%s is in the contract but no fixture carries it", kind, name)
			}
		}
	}
}

// The Go type has to survive a round trip without dropping anything the
// contract declares. This is what catches a field added to the schema and the
// TypeScript types but forgotten in Go.
func TestGoTypeCarriesEveryFixtureField(t *testing.T) {
	for i, msg := range fixture(t) {
		var before map[string]any
		if err := json.Unmarshal(msg, &before); err != nil {
			t.Fatal(err)
		}

		var m wire.Message
		if err := json.Unmarshal(msg, &m); err != nil {
			t.Fatalf("line %d: wire.Message cannot hold it: %v", i+1, err)
		}
		encoded, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var after map[string]any
		if err := json.Unmarshal(encoded, &after); err != nil {
			t.Fatal(err)
		}

		for name, want := range before {
			got, ok := after[name]
			if !ok {
				t.Errorf("line %d (%v): wire.Message dropped %q", i+1, before["type"], name)
				continue
			}
			if !sameJSON(want, got) {
				t.Errorf("line %d (%v): %q round-tripped as %v, want %v", i+1, before["type"], name, got, want)
			}
		}
	}
}

// A validator that accepts anything is worse than none, so prove it rejects.
func TestValidateRejectsMalformedMessages(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"no type", `{"seq":1}`, "no type"},
		{"unknown type", `{"type":"whisper","seq":1}`, "not a message type"},
		{"missing required field", `{"type":"call_ended","seq":1,"at":"2026-10-19T08:15:16Z"}`, "required field \"reason\""},
		{"undefined field", `{"type":"reply","seq":1,"at":"2026-10-19T08:15:16Z","text":"hi","mood":"warm"}`, "does not define"},
		{"wrong field type", `{"type":"reply","seq":1,"at":"2026-10-19T08:15:16Z","text":42}`, "must be a string"},
		{"unknown action kind", `{"type":"action","seq":1,"at":"2026-10-19T08:15:16Z","kind":"launch_rocket"}`, "not one of"},
		{"digit outside the keypad", `{"type":"dtmf","digit":"Z"}`, "does not match"},
		{"negative seq", `{"type":"reply","seq":-1,"at":"2026-10-19T08:15:16Z","text":"hi"}`, "at least"},
		{"timestamp not RFC 3339", `{"type":"reply","seq":1,"at":"last Tuesday","text":"hi"}`, "RFC 3339"},
		{"not an object", `"hello"`, "not an object"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := wire.Validate([]byte(c.msg))
			if err == nil {
				t.Fatalf("accepted a malformed message: %s", c.msg)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// The reference SDK stamps its own seq and at on everything it sends. That is
// allowed, and this is the test that says so — the contract and its own client
// disagreed about it until the fixtures went in.
func TestReferenceSDKMessagesValidate(t *testing.T) {
	for _, raw := range []string{
		`{"seq":1,"at":"2026-10-19T08:15:00.000Z","type":"start","call_id":"c1","hint":"+254722123456"}`,
		`{"seq":2,"at":"2026-10-19T08:15:04.000Z","type":"dtmf","digit":"2","sensitive":false}`,
		`{"seq":3,"at":"2026-10-19T08:15:16.000Z","type":"stop","reason":"hangup"}`,
	} {
		if err := wire.Validate([]byte(raw)); err != nil {
			t.Errorf("the reference SDK sends a message the contract rejects:\n  %s\n  %v", raw, err)
		}
	}
}

// Likewise the engine: every message it emits carries seq and at, which the
// contract has to permit on each type the engine can send.
func TestEmittedMessagesValidate(t *testing.T) {
	at := "2026-10-19T08:15:00Z"
	for _, raw := range []string{
		`{"type":"call_started","seq":1,"at":"` + at + `","call_id":"c1"}`,
		`{"type":"reply","seq":2,"at":"` + at + `","text":"hello"}`,
		`{"type":"transcript","seq":3,"at":"` + at + `","text":"hi","final":true}`,
		`{"type":"dtmf","seq":4,"at":"` + at + `","digit":"2"}`,
		`{"type":"digits","seq":5,"at":"` + at + `","count":4,"redacted":true,"hash":"abcd1234abcd1234"}`,
		`{"type":"action","seq":6,"at":"` + at + `","kind":"transfer","args":{"dest":"200"}}`,
		`{"type":"error","seq":7,"at":"` + at + `","code":"action_failed","message":"nope"}`,
		`{"type":"call_ended","seq":8,"at":"` + at + `","reason":"hangup"}`,
	} {
		if err := wire.Validate([]byte(raw)); err != nil {
			t.Errorf("the engine emits a message the contract rejects:\n  %s\n  %v", raw, err)
		}
	}
}

// sameJSON compares two decoded JSON values structurally, treating two RFC
// 3339 timestamps for the same instant as equal. The contract asks for RFC
// 3339, which does not fix how many fractional digits are written: Go emits
// ".1Z" where the fixture has ".100Z", and both are the same moment.
func sameJSON(a, b any) bool {
	if sameInstant(a, b) {
		return true
	}
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func sameInstant(a, b any) bool {
	left, ok := a.(string)
	if !ok {
		return false
	}
	right, ok := b.(string)
	if !ok {
		return false
	}
	lt, err := time.Parse(time.RFC3339, left)
	if err != nil {
		return false
	}
	rt, err := time.Parse(time.RFC3339, right)
	if err != nil {
		return false
	}
	return lt.Equal(rt)
}
