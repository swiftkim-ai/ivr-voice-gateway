package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/scenario"
)

// RemoteEvent is something observed from the remote IVR during a scenario.
type RemoteEvent struct {
	Kind  string // "dtmf", "terminate", "error"
	Digit string // for dtmf
}

// RunOutboundScenario executes a scenario on an established AudioSocket
// connection. It writes step and action events to the event log, sends DTMF
// via the controller, and observes events from the remote IVR.
//
// The call's audio stream (silence when idle) runs in a background goroutine
// so Asterisk does not hang up.
func RunOutboundScenario(ctx context.Context, conn net.Conn, opts AudioSocketOptions, sc scenario.Scenario) (string, error) {
	br := bufio.NewReader(conn)

	uuid, err := readCallUUID(br)
	if err != nil {
		return "", err
	}

	var info control.CallInfo
	if opts.Channels != nil {
		info = waitForChannel(opts, uuid)
		if info.Channel == "" {
			log.Printf("gateway: outbound %s: no channel announced; DTMF will not be sent", uuid)
		}
	}

	logw := openCallLog(opts.LogDir, uuid)
	if logw != nil {
		defer logw.Close()
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	out := make(chan []byte, outboundFrames)
	go streamAudio(ctx, conn, out, opts.framePace())

	if logw != nil {
		logw.Append("call_started", map[string]any{
			"hint":     sc.Target,
			"scenario": sc.Name,
		})
	}

	remote := make(chan RemoteEvent, 64)
	callDone := make(chan error, 1)
	go func() { callDone <- drainAudioSocket(br, remote) }()

	for i, step := range sc.Steps {
		wait := step.ParsedWait()
		if wait > 0 {
			if err := waitDuration(ctx, wait, remote, logw, callDone); err != nil {
				endLog(logw, endReason(err))
				return uuid, nil
			}
		}

		if logw != nil {
			logw.Append("step", map[string]any{"label": step.Label, "index": i})
		}

		if step.Press != "" {
			for _, d := range step.Press {
				digit := string(d)
				if opts.Control != nil && info.Channel != "" {
					if err := opts.Control.Do(ctx, info.Channel, control.Action{
						Kind: control.SendDTMF,
						Args: map[string]any{"digit": digit},
					}); err != nil {
						log.Printf("gateway: outbound %s: send_dtmf %s: %v", uuid, digit, err)
					}
				}
				if logw != nil {
					logw.Append("action", map[string]any{
						"kind": "send_dtmf",
						"args": map[string]any{"digit": digit},
					})
				}
			}
		}
	}

	finalWait := sc.ParsedFinalWait()
	if err := waitDuration(ctx, finalWait, remote, logw, callDone); err != nil {
		endLog(logw, endReason(err))
		return uuid, nil
	}

	endLog(logw, "scenario_complete")

	if opts.Control != nil && info.Channel != "" {
		_ = opts.Control.Do(ctx, info.Channel, control.Action{Kind: control.Hangup})
	}

	return uuid, nil
}

// AcceptOutbound accepts exactly one AudioSocket connection on ln, with a
// timeout. It is how the map command waits for an originated call to answer
// and connect back.
func AcceptOutbound(ln net.Listener, timeout time.Duration) (net.Conn, error) {
	if dl, ok := ln.(interface{ SetDeadline(time.Time) error }); ok {
		dl.SetDeadline(time.Now().Add(timeout))
	}
	conn, err := ln.Accept()
	if err != nil {
		return nil, fmt.Errorf("gateway: accept outbound: %w", err)
	}
	if dl, ok := ln.(interface{ SetDeadline(time.Time) error }); ok {
		dl.SetDeadline(time.Time{})
	}
	return conn, nil
}

func drainAudioSocket(br *bufio.Reader, out chan<- RemoteEvent) error {
	for {
		kind, payload, err := readFrame(br)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		switch kind {
		case asAudio8k:
			// consumed — no STT in Phase 2
		case asDTMF:
			if len(payload) > 0 {
				select {
				case out <- RemoteEvent{Kind: "dtmf", Digit: string(payload[:1])}:
				default:
				}
			}
		case asTerminate:
			return nil
		case asError:
			return fmt.Errorf("audiosocket error")
		}
	}
}

func waitDuration(ctx context.Context, d time.Duration, remote <-chan RemoteEvent, logw *eventlog.Writer, callDone <-chan error) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return nil
		case ev := <-remote:
			if ev.Kind == "dtmf" && logw != nil {
				logw.Append("dtmf", map[string]any{"digit": ev.Digit, "source": "remote"})
			}
		case err := <-callDone:
			if err != nil {
				return fmt.Errorf("call ended: %w", err)
			}
			return fmt.Errorf("call ended")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func endReason(err error) string {
	if err != nil {
		return err.Error()
	}
	return "remote_hangup"
}

func endLog(logw *eventlog.Writer, reason string) {
	if logw != nil {
		logw.Append("call_ended", map[string]any{"reason": reason})
	}
}

func parseUUID(s string) []byte {
	var b [16]byte
	clean := make([]byte, 0, 32)
	for _, c := range []byte(s) {
		if c != '-' {
			clean = append(clean, c)
		}
	}
	if len(clean) != 32 {
		return b[:]
	}
	for i := 0; i < 16; i++ {
		b[i] = hexVal(clean[2*i])<<4 | hexVal(clean[2*i+1])
	}
	return b[:]
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}
