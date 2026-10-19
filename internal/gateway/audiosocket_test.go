package gateway_test

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/gateway"
	"github.com/swiftkimani/ivr-voice-gateway/internal/media"
)

// AudioSocket frame kinds, restated here so the test asserts against the
// protocol rather than against the implementation's constants.
const (
	kindTerminate = 0x00
	kindUUID      = 0x01
	kindDTMF      = 0x03
	kindAudio     = 0x10
)

const testUUID = "7a1f1f3e-0d1a-4a0e-9a1b-2c3d4e5f6071"

// speechMarker fills a frame so tests can tell the agent's voice from the
// silence the stream sends whenever there is nothing to say.
const speechMarker = 0x41

// markerSpeaker is brain.Silence with a signature, so "did the agent speak?"
// is a different question from "is the stream alive?".
type markerSpeaker struct{ frames int }

func (m markerSpeaker) Frames(string) [][]byte {
	n := m.frames
	if n <= 0 {
		n = 2
	}
	out := make([][]byte, n)
	for i := range out {
		frame := make([]byte, media.FrameBytes)
		for j := range frame {
			frame[j] = speechMarker
		}
		out[i] = frame
	}
	return out
}

func isSpeech(frame []byte) bool {
	for _, b := range frame {
		if b != 0 {
			return true
		}
	}
	return false
}

// fakeChannels stands in for the AMI index.
type fakeChannels struct{ info control.CallInfo }

func (f fakeChannels) Lookup(string) (control.CallInfo, bool) {
	if f.info.Channel == "" {
		return control.CallInfo{}, false
	}
	return f.info, true
}

// asteriskClient is the Asterisk side of an AudioSocket connection.
type asteriskClient struct {
	conn net.Conn
	br   *bufio.Reader
	t    *testing.T
}

func (a *asteriskClient) send(kind byte, payload []byte) {
	a.t.Helper()
	buf := append([]byte{kind, byte(len(payload) >> 8), byte(len(payload))}, payload...)
	if _, err := a.conn.Write(buf); err != nil {
		a.t.Fatalf("write frame 0x%02X: %v", kind, err)
	}
}

func (a *asteriskClient) sendUUID(uuid string) {
	a.t.Helper()
	raw, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil {
		a.t.Fatal(err)
	}
	a.send(kindUUID, raw)
}

func (a *asteriskClient) sendDTMF(d byte) { a.send(kindDTMF, []byte{d}) }

// read returns the next frame, or fails if none arrives in time.
func (a *asteriskClient) read() (byte, []byte) {
	a.t.Helper()
	if err := a.conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		a.t.Fatal(err)
	}
	var hdr [3]byte
	if _, err := io.ReadFull(a.br, hdr[:]); err != nil {
		a.t.Fatalf("read frame header: %v", err)
	}
	n := int(binary.BigEndian.Uint16(hdr[1:3]))
	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(a.br, payload); err != nil {
			a.t.Fatalf("read frame payload: %v", err)
		}
	}
	return hdr[0], payload
}

// readSpeech drains the stream until n frames of the agent's voice have
// arrived, ignoring the silence that keeps the connection alive.
func (a *asteriskClient) readSpeech(n int) [][]byte {
	a.t.Helper()
	var out [][]byte
	for len(out) < n {
		kind, payload := a.read()
		if kind == kindAudio && isSpeech(payload) {
			out = append(out, payload)
		}
	}
	return out
}

// readSilence waits for n keepalive frames, which is how a test observes that
// the stream stays alive while the agent has nothing to say.
func (a *asteriskClient) readSilence(n int) int {
	a.t.Helper()
	got := 0
	for got < n {
		kind, payload := a.read()
		if kind == kindAudio && !isSpeech(payload) {
			got++
		}
	}
	return got
}

// startAudioSocket runs the adapter on a local listener and dials it.
func startAudioSocket(t *testing.T, opts gateway.AudioSocketOptions) (*asteriskClient, string) {
	t.Helper()
	if opts.Brain == nil {
		opts.Brain = testBrain()
	}
	if opts.Speaker == nil {
		opts.Speaker = markerSpeaker{frames: 2}
	}
	if opts.LogDir == "" {
		opts.LogDir = t.TempDir()
	}
	opts.FramePace = time.Millisecond // run the stream fast in tests
	opts.ChannelWait = 200 * time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go gateway.ServeAudioSocket(ln, opts)
	t.Cleanup(func() { ln.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &asteriskClient{conn: conn, br: bufio.NewReader(conn), t: t}, opts.LogDir
}

func TestAudioSocketGreetsWithAudio(t *testing.T) {
	as, _ := startAudioSocket(t, gateway.AudioSocketOptions{})
	as.sendUUID(testUUID)

	frames := as.readSpeech(2)
	for i, f := range frames {
		if len(f) != media.FrameBytes {
			t.Fatalf("greeting frame %d is %d bytes, want %d", i, len(f), media.FrameBytes)
		}
	}
}

func TestAudioSocketDTMFDrivesTheCall(t *testing.T) {
	var ctl control.Loopback
	as, _ := startAudioSocket(t, gateway.AudioSocketOptions{
		Control:  &ctl,
		Channels: fakeChannels{control.CallInfo{Channel: "PJSIP/alice-1", Hint: "+254722000000"}},
	})
	as.sendUUID(testUUID)
	as.readSpeech(2) // greeting

	as.sendDTMF('1')
	as.readSpeech(2) // the reply to the digit

	done := waitFor(t, func() bool { return len(ctl.Performed()) > 0 })
	if !done {
		t.Fatal("pressing 1 produced no action on the control channel")
	}
	got := ctl.Performed()[0]
	if got.Action.Kind != control.Transfer {
		t.Fatalf("action = %q, want transfer", got.Action.Kind)
	}
	if got.Channel != "PJSIP/alice-1" {
		t.Fatalf("action addressed to %q, want the announced channel", got.Channel)
	}
}

func TestAudioSocketPassesDialplanHintToTheBrain(t *testing.T) {
	// The brain only greets this way when it was given the dialplan's hint.
	seen := make(chan string, 1)
	as, _ := startAudioSocket(t, gateway.AudioSocketOptions{
		Brain:    hintSpy{seen},
		Channels: fakeChannels{control.CallInfo{Channel: "PJSIP/alice-1", Hint: "+254722000000"}},
	})
	as.sendUUID(testUUID)

	select {
	case got := <-seen:
		if got != "+254722000000" {
			t.Fatalf("brain hint = %q, want the dialplan's", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the brain was never asked for a greeting")
	}
}

type hintSpy struct{ seen chan string }

func (h hintSpy) Respond(_ context.Context, in brain.Input) (brain.Reply, error) {
	select {
	case h.seen <- in.Hint:
	default:
	}
	return brain.Reply{Sentences: []string{"Hello."}}, nil
}

func TestAudioSocketWritesAReplayableLog(t *testing.T) {
	as, dir := startAudioSocket(t, gateway.AudioSocketOptions{
		Channels: fakeChannels{control.CallInfo{Channel: "PJSIP/alice-1"}},
		Control:  &control.Loopback{},
	})
	as.sendUUID(testUUID)
	as.readSpeech(2)
	as.sendDTMF('1')
	as.readSpeech(2)
	as.send(kindTerminate, nil)

	path := dir + "/" + testUUID + ".jsonl"
	waitFor(t, func() bool {
		events, err := eventlog.Read(path)
		return err == nil && hasType(events, "call_ended")
	})

	events, err := eventlog.Read(path)
	if err != nil {
		t.Fatalf("the call wrote no log at %s: %v", path, err)
	}
	for _, want := range []string{"call_started", "dtmf", "action", "reply", "call_ended"} {
		if !hasType(events, want) {
			t.Fatalf("log has no %q event; got %v", want, types(events))
		}
	}
	for i, e := range events {
		if e.Seq != i+1 {
			t.Fatalf("event %d has seq %d; the log must stay monotonic", i, e.Seq)
		}
	}
}

func TestAudioSocketRecordsRefusedActionAndKeepsTheCall(t *testing.T) {
	// play_prompt has no AMI implementation (ADR-0007). The call must survive
	// it, with the refusal recorded.
	as, dir := startAudioSocket(t, gateway.AudioSocketOptions{
		Brain: &brain.Scripted{Rules: []brain.Rule{
			{Match: "*", Say: []string{"One moment."},
				Actions: []control.Action{{Kind: control.PlayPrompt, Args: map[string]any{"file": "welcome"}}}},
		}},
		Control:  refusingControl{},
		Channels: fakeChannels{control.CallInfo{Channel: "PJSIP/alice-1"}},
	})
	as.sendUUID(testUUID)

	// The greeting still reaches the caller even though the action failed.
	as.readSpeech(2)

	path := dir + "/" + testUUID + ".jsonl"
	waitFor(t, func() bool {
		events, _ := eventlog.Read(path)
		return hasType(events, "error")
	})
	events, err := eventlog.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !hasType(events, "error") {
		t.Fatalf("the refused action was not recorded; got %v", types(events))
	}
	if !hasType(events, "reply") {
		t.Fatal("the call did not continue after the refused action")
	}
}

type refusingControl struct{}

func (refusingControl) Do(context.Context, string, control.Action) error {
	return control.ErrUnsupported
}

func TestAudioSocketRejectsAMissingIDFrame(t *testing.T) {
	as, dir := startAudioSocket(t, gateway.AudioSocketOptions{})
	// Audio before the id frame: the call cannot be identified or logged.
	as.send(kindAudio, make([]byte, media.FrameBytes))

	time.Sleep(200 * time.Millisecond)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("an unidentified call wrote %d log file(s)", len(entries))
	}
}

func TestAudioSocketCollectsSensitiveDigitsWithoutLoggingThem(t *testing.T) {
	const pin = "1234"
	as, dir := startAudioSocket(t, gateway.AudioSocketOptions{
		Brain: &brain.Scripted{Rules: []brain.Rule{
			{Match: "digits:", Say: []string{"Thank you."}},
			{Match: "*", Say: []string{"Enter your PIN."},
				Actions: []control.Action{{Kind: control.ReadDigits, Args: map[string]any{
					"label": "pin", "count": 4, "sensitive": true,
				}}}},
		}},
		Redactor: call.NewRedactor("test-salt"),
		Control:  &control.Loopback{},
		Channels: fakeChannels{control.CallInfo{Channel: "PJSIP/alice-1"}},
	})
	as.sendUUID(testUUID)
	as.readSpeech(2) // "Enter your PIN."

	for i := 0; i < len(pin); i++ {
		as.sendDTMF(pin[i])
	}
	as.readSpeech(2) // "Thank you."

	path := dir + "/" + testUUID + ".jsonl"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), pin) {
		t.Fatalf("the PIN was written to the call log:\n%s", raw)
	}
	events, err := eventlog.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !hasType(events, "digits") {
		t.Fatalf("no digits event; got %v", types(events))
	}
}

func hasType(events []eventlog.Event, typ string) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func types(events []eventlog.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Asterisk hangs up an AudioSocket connection after two seconds without
// traffic, so the stream has to keep running when the agent is silent. This is
// the regression test for a bug only a real switch revealed.
func TestAudioSocketKeepsTheStreamAliveWhenIdle(t *testing.T) {
	as, _ := startAudioSocket(t, gateway.AudioSocketOptions{
		Brain:   &brain.Scripted{Rules: []brain.Rule{{Match: "*", Say: []string{"Hello."}}}},
		Speaker: markerSpeaker{frames: 2},
	})
	as.sendUUID(testUUID)

	as.readSpeech(2) // the greeting

	// Nothing more will be said, yet frames must keep coming.
	if got := as.readSilence(10); got != 10 {
		t.Fatalf("read %d keepalive frames, want 10", got)
	}
}

func TestAudioSocketKeepalivesAreWellFormedFrames(t *testing.T) {
	as, _ := startAudioSocket(t, gateway.AudioSocketOptions{
		Brain:   &brain.Scripted{Rules: []brain.Rule{{Match: "*", Say: []string{"Hello."}}}},
		Speaker: markerSpeaker{frames: 1},
	})
	as.sendUUID(testUUID)
	as.readSpeech(1)

	// A short frame would be a protocol violation Asterisk would reject.
	for i := 0; i < 5; i++ {
		kind, payload := as.read()
		if kind != kindAudio {
			continue
		}
		if len(payload) != media.FrameBytes {
			t.Fatalf("frame %d is %d bytes, want %d", i, len(payload), media.FrameBytes)
		}
	}
}
