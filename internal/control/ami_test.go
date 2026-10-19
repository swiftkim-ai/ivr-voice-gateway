package control_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
)

// fakeAMI is a stand-in Asterisk manager interface: it speaks the real line
// protocol, records the action blocks it receives, and answers each one.
type fakeAMI struct {
	ln net.Listener

	mu       sync.Mutex
	received []map[string]string
	conns    []net.Conn

	// refuse names an action the server answers with Response: Error.
	refuse string
}

func startFakeAMI(t *testing.T) *fakeAMI {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAMI{ln: ln}
	go f.accept()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeAMI) addr() string { return f.ln.Addr().String() }

func (f *fakeAMI) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		go f.serve(conn)
	}
}

func (f *fakeAMI) serve(conn net.Conn) {
	defer conn.Close()
	if _, err := conn.Write([]byte("Asterisk Call Manager/7.0.3\r\n")); err != nil {
		return
	}
	br := bufio.NewReader(conn)
	for {
		blk, err := readTestBlock(br)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.received = append(f.received, blk)
		refuse := f.refuse
		f.mu.Unlock()

		resp := "Success"
		msg := "Action completed"
		if refuse != "" && strings.EqualFold(blk["action"], refuse) {
			resp, msg = "Error", "No such channel"
		}
		out := "Response: " + resp + "\r\nActionID: " + blk["actionid"] + "\r\nMessage: " + msg + "\r\n\r\n"
		if _, err := conn.Write([]byte(out)); err != nil {
			return
		}
	}
}

// push sends an unsolicited event to every connected client.
func (f *fakeAMI) push(lines ...string) {
	f.mu.Lock()
	conns := append([]net.Conn(nil), f.conns...)
	f.mu.Unlock()
	block := strings.Join(lines, "\r\n") + "\r\n\r\n"
	for _, c := range conns {
		_, _ = c.Write([]byte(block))
	}
}

func (f *fakeAMI) actions() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]string, len(f.received))
	copy(out, f.received)
	return out
}

// lastAction returns the most recent block whose Action matches name.
func (f *fakeAMI) lastAction(name string) map[string]string {
	all := f.actions()
	for i := len(all) - 1; i >= 0; i-- {
		if strings.EqualFold(all[i]["action"], name) {
			return all[i]
		}
	}
	return nil
}

func readTestBlock(r *bufio.Reader) (map[string]string, error) {
	blk := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(blk) == 0 {
				continue
			}
			return blk, nil
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			blk[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
}

func dialTestAMI(t *testing.T, f *fakeAMI) *control.AMI {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := control.DialAMI(ctx, control.AMIConfig{
		Addr: f.addr(), Username: "ivrvg", Secret: "s3cret", Context: "from-internal",
	})
	if err != nil {
		t.Fatalf("DialAMI: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestAMILogsIn(t *testing.T) {
	f := startFakeAMI(t)
	dialTestAMI(t, f)

	login := f.lastAction("Login")
	if login == nil {
		t.Fatal("no Login action received")
	}
	if login["username"] != "ivrvg" || login["secret"] != "s3cret" {
		t.Fatalf("login credentials = %q/%q", login["username"], login["secret"])
	}
	if login["actionid"] == "" {
		t.Fatal("Login carried no ActionID, so no response could be matched to it")
	}
}

func TestAMIRejectsNonManagerBanner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))
		conn.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := control.DialAMI(ctx, control.AMIConfig{Addr: ln.Addr().String()}); err == nil {
		t.Fatal("expected a connection to a non-manager port to fail")
	}
}

func TestAMIActionMapping(t *testing.T) {
	f := startFakeAMI(t)
	a := dialTestAMI(t, f)
	ctx := context.Background()
	const channel = "PJSIP/alice-00000001"

	cases := []struct {
		name   string
		action control.Action
		want   string            // expected AMI Action
		fields map[string]string // expected fields on that action
	}{
		{
			name:   "send_dtmf",
			action: control.Action{Kind: control.SendDTMF, Args: map[string]any{"digit": "5"}},
			want:   "PlayDTMF",
			fields: map[string]string{"channel": channel, "digit": "5"},
		},
		{
			name:   "transfer uses dest and the configured context",
			action: control.Action{Kind: control.Transfer, Args: map[string]any{"dest": "200"}},
			want:   "Redirect",
			fields: map[string]string{"channel": channel, "exten": "200", "context": "from-internal", "priority": "1"},
		},
		{
			name: "route may override context and priority",
			action: control.Action{Kind: control.Route, Args: map[string]any{
				"dest": "9000", "context": "ivr-menu", "priority": "3",
			}},
			want:   "Redirect",
			fields: map[string]string{"exten": "9000", "context": "ivr-menu", "priority": "3"},
		},
		{
			name:   "park",
			action: control.Action{Kind: control.Park},
			want:   "Park",
			fields: map[string]string{"channel": channel},
		},
		{
			name:   "hangup",
			action: control.Action{Kind: control.Hangup},
			want:   "Hangup",
			fields: map[string]string{"channel": channel},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := a.Do(ctx, channel, c.action); err != nil {
				t.Fatalf("Do(%s): %v", c.action.Kind, err)
			}
			got := f.lastAction(c.want)
			if got == nil {
				t.Fatalf("no %s action reached the switch", c.want)
			}
			for k, want := range c.fields {
				if got[k] != want {
					t.Fatalf("%s %s = %q, want %q", c.want, k, got[k], want)
				}
			}
		})
	}
}

func TestAMIReadDigitsTouchesNoSwitch(t *testing.T) {
	f := startFakeAMI(t)
	a := dialTestAMI(t, f)

	before := len(f.actions())
	if err := a.Do(context.Background(), "PJSIP/alice-1", control.Action{Kind: control.ReadDigits}); err != nil {
		t.Fatalf("read_digits: %v", err)
	}
	if after := len(f.actions()); after != before {
		t.Fatalf("read_digits sent %d action(s) to the switch; the engine collects digits itself", after-before)
	}
}

func TestAMIPlayPromptIsUnsupported(t *testing.T) {
	f := startFakeAMI(t)
	a := dialTestAMI(t, f)

	err := a.Do(context.Background(), "PJSIP/alice-1", control.Action{
		Kind: control.PlayPrompt, Args: map[string]any{"file": "welcome"},
	})
	if !errors.Is(err, control.ErrUnsupported) {
		t.Fatalf("play_prompt error = %v, want ErrUnsupported", err)
	}
}

func TestAMIReportsRefusedAction(t *testing.T) {
	f := startFakeAMI(t)
	f.refuse = "Hangup"
	a := dialTestAMI(t, f)

	err := a.Do(context.Background(), "PJSIP/gone-1", control.Action{Kind: control.Hangup})
	if err == nil {
		t.Fatal("expected an error when the switch refuses the action")
	}
	if !strings.Contains(err.Error(), "No such channel") {
		t.Fatalf("error = %v, want the switch's message", err)
	}
}

func TestAMINeedsAChannel(t *testing.T) {
	f := startFakeAMI(t)
	a := dialTestAMI(t, f)

	if err := a.Do(context.Background(), "", control.Action{Kind: control.Hangup}); err == nil {
		t.Fatal("expected an error when the call has no known channel")
	}
}

func TestAMIIndexesChannelsByUUID(t *testing.T) {
	f := startFakeAMI(t)
	a := dialTestAMI(t, f)

	const uuid = "7a1f1f3e-0d1a-4a0e-9a1b-2c3d4e5f6071"
	const channel = "PJSIP/alice-00000009"

	if _, ok := a.Lookup(uuid); ok {
		t.Fatal("channel known before the dialplan announced it")
	}

	f.push("Event: UserEvent", "UserEvent: IvrvgStart",
		"Channel: "+channel, "UUID: "+uuid, "Hint: +254722000000")
	got := waitForChannel(t, a, uuid, true)
	if got.Channel != channel {
		t.Fatalf("Lookup(uuid).Channel = %q, want %q", got.Channel, channel)
	}
	if got.Hint != "+254722000000" {
		t.Fatalf("Lookup(uuid).Hint = %q; the dialplan's context was dropped", got.Hint)
	}

	// Hangup must evict the entry, or the index grows for the life of the process.
	f.push("Event: Hangup", "Channel: "+channel, "Cause: 16")
	waitForChannel(t, a, uuid, false)
}

// waitForChannel polls until the index reaches want, since events arrive
// asynchronously on the read loop.
func waitForChannel(t *testing.T, a *control.AMI, uuid string, want bool) control.CallInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := a.Lookup(uuid); ok == want {
			return info
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Lookup(%q) known = %v, want %v", uuid, !want, want)
	return control.CallInfo{}
}

func TestLoopbackRecordsActions(t *testing.T) {
	var lb control.Loopback
	act := control.Action{Kind: control.Transfer, Args: map[string]any{"dest": "200"}}
	if err := lb.Do(context.Background(), "chan-1", act); err != nil {
		t.Fatal(err)
	}
	done := lb.Performed()
	if len(done) != 1 {
		t.Fatalf("recorded %d actions, want 1", len(done))
	}
	if done[0].Channel != "chan-1" || done[0].Action.Kind != control.Transfer {
		t.Fatalf("recorded %+v", done[0])
	}
}
