package gateway

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"path/filepath"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/media"
	"github.com/swiftkimani/ivr-voice-gateway/internal/wire"
)

// AudioSocket frame kinds. Every frame is a one-byte kind, a two-byte
// big-endian payload length, then that many bytes.
//
// The audio kind carries signed linear 16-bit mono PCM, little-endian, at
// 8 kHz — the engine's native format, so this adapter neither resamples nor
// transcodes. Asterisk handles G.711 on the trunk side.
const (
	asTerminate = 0x00 // the far end is done with the call
	asUUID      = 0x01 // 16 raw bytes identifying the call
	asDTMF      = 0x03 // one ASCII digit
	asAudio8k   = 0x10 // SLIN 8 kHz mono little-endian
	asError     = 0xff // an error code from Asterisk
)

// maxFrame is the largest payload the 16-bit length field can describe.
const maxFrame = 0xFFFF

// ChannelLookup resolves an AudioSocket UUID to what the dialplan announced
// about that call. AudioSocket itself carries no channel name, and actions must
// be addressed to one.
type ChannelLookup interface {
	Lookup(uuid string) (control.CallInfo, bool)
}

// AudioSocketOptions configures the AudioSocket listener.
type AudioSocketOptions struct {
	// LogDir is where per-call event logs are written (one JSONL per call).
	LogDir string
	// Brain and Speaker are the shared decision and speech components.
	Brain   brain.Brain
	Speaker brain.Speaker

	// Control carries actions out to the switch. Without one, actions are
	// still recorded but nothing performs them.
	Control control.Controller
	// Channels resolves the UUID to a switch channel. Without one, actions
	// have no address and the call runs media-only.
	Channels ChannelLookup
	// Redactor keys the token standing in for a sensitive digit group.
	Redactor *call.Redactor

	// FramePace is the gap between outbound audio frames, and so the rate the
	// stream runs at. It defaults to the frame duration, which is real time;
	// tests shorten it to finish quickly.
	FramePace time.Duration
	// ChannelWait bounds how long to wait for the dialplan's UserEvent before
	// running the call without a channel. Defaults to 2s.
	ChannelWait time.Duration
}

func (o AudioSocketOptions) framePace() time.Duration {
	if o.FramePace <= 0 {
		return media.FrameMS * time.Millisecond
	}
	return o.FramePace
}

func (o AudioSocketOptions) channelWait() time.Duration {
	if o.ChannelWait == 0 {
		return 2 * time.Second
	}
	return o.ChannelWait
}

// ListenAndServeAudioSocket serves the AudioSocket protocol on addr until the
// listener fails.
func ListenAndServeAudioSocket(addr string, opts AudioSocketOptions) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("gateway: audiosocket listen %s: %w", addr, err)
	}
	return ServeAudioSocket(ln, opts)
}

// ServeAudioSocket accepts calls on ln, one goroutine per call.
func ServeAudioSocket(ln net.Listener, opts AudioSocketOptions) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			if err := serveAudioSocketCall(context.Background(), conn, opts); err != nil {
				log.Printf("gateway: audiosocket call: %v", err)
			}
		}()
	}
}

// serveAudioSocketCall runs one call: identify it, greet, then pump frames
// until the far end terminates.
func serveAudioSocketCall(ctx context.Context, conn net.Conn, opts AudioSocketOptions) error {
	br := bufio.NewReader(conn)

	uuid, err := readCallUUID(br)
	if err != nil {
		return err
	}

	var info control.CallInfo
	if opts.Channels != nil {
		info = waitForChannel(opts, uuid)
		if info.Channel == "" {
			log.Printf("gateway: audiosocket %s: no channel announced; actions cannot be addressed", uuid)
		}
	}

	logw := openCallLog(opts.LogDir, uuid)
	if logw != nil {
		defer logw.Close()
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	sink := &audioSocketSink{
		ctx: ctx, conn: conn, log: logw,
		ctl: opts.Control, channel: info.Channel,
		out: make(chan []byte, outboundFrames),
	}
	// The stream has to start before anything is said: Asterisk is already
	// counting from the moment it connects.
	go streamAudio(ctx, conn, sink.out, opts.framePace())

	c := &call.Call{
		ID: uuid, Hint: info.Hint,
		Brain: opts.Brain, Speaker: opts.Speaker,
		Sink: sink, Log: logw, Redactor: opts.Redactor,
	}

	if err := c.Start(); err != nil {
		return err
	}
	if err := c.Respond(ctx); err != nil { // greeting
		return err
	}
	return pumpAudioSocket(ctx, br, c)
}

// pumpAudioSocket turns inbound frames into engine input until the call ends.
func pumpAudioSocket(ctx context.Context, br *bufio.Reader, c *call.Call) error {
	for {
		kind, payload, err := readFrame(br)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return c.End("disconnect")
			}
			_ = c.End("disconnect")
			return err
		}

		switch kind {
		case asAudio8k:
			// Caller audio. Phase 1 has no STT, so it is accepted and dropped;
			// Phase 4 runs it through the transcriber.

		case asDTMF:
			if len(payload) == 0 {
				continue
			}
			if err := c.Digit(ctx, string(payload[:1]), false); err != nil {
				return err
			}

		case asTerminate:
			return c.End("hangup")

		case asError:
			code := "audiosocket_error"
			if len(payload) > 0 {
				code = fmt.Sprintf("audiosocket_error_%d", payload[0])
			}
			_ = c.End(code)
			return fmt.Errorf("gateway: audiosocket: switch reported %s", code)
		}
	}
}

// readCallUUID reads the identifying frame Asterisk sends first.
func readCallUUID(br *bufio.Reader) (string, error) {
	kind, payload, err := readFrame(br)
	if err != nil {
		return "", fmt.Errorf("gateway: audiosocket: reading the id frame: %w", err)
	}
	if kind != asUUID {
		return "", fmt.Errorf("gateway: audiosocket: first frame was kind 0x%02X, want the id frame 0x%02X", kind, asUUID)
	}
	uuid := formatUUID(payload)
	if uuid == "" {
		return "", fmt.Errorf("gateway: audiosocket: id frame carried %d bytes, want 16", len(payload))
	}
	return uuid, nil
}

// waitForChannel polls the index until the dialplan's UserEvent lands. The
// AudioSocket connection and the manager event race, and either may win.
func waitForChannel(opts AudioSocketOptions, uuid string) control.CallInfo {
	deadline := time.Now().Add(opts.channelWait())
	for {
		if info, ok := opts.Channels.Lookup(uuid); ok {
			return info
		}
		if time.Now().After(deadline) {
			return control.CallInfo{}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func openCallLog(dir, uuid string) *eventlog.Writer {
	if dir == "" {
		return nil
	}
	lg, err := eventlog.Create(filepath.Join(dir, uuid+".jsonl"), uuid)
	if err != nil {
		log.Printf("gateway: log create: %v", err)
		return nil
	}
	return lg
}

// outboundFrames is how much agent audio may queue before the engine is made
// to wait — two seconds, well past any single sentence.
const outboundFrames = 100

// audioSocketSink queues agent audio for the stream and hands actions to the
// control channel, because AudioSocket carries no actions of its own (ADR-0004).
type audioSocketSink struct {
	ctx     context.Context
	conn    net.Conn
	log     *eventlog.Writer
	ctl     control.Controller
	channel string

	out chan []byte
}

// Message forwards actions to the switch. Other message types are already in
// the event log, and AudioSocket has nowhere to put them.
func (s *audioSocketSink) Message(m wire.Message) error {
	if m.Type != "action" || s.ctl == nil {
		return nil
	}
	err := s.ctl.Do(s.ctx, s.channel, control.Action{Kind: m.Kind, Args: m.Args})
	if err == nil {
		return nil
	}
	// An action the switch refuses is an outcome to record, not a reason to
	// drop a live call. Replay and the eval loop read it from the log.
	if s.log != nil {
		_, _ = s.log.Append("error", map[string]any{
			"code": "action_failed", "kind": m.Kind, "message": err.Error(),
		})
	}
	log.Printf("gateway: audiosocket action %s: %v", m.Kind, err)
	return nil
}

// Audio queues one frame for the stream. The copy is deliberate: the speaker
// may reuse its buffer once this returns.
func (s *audioSocketSink) Audio(pcm []byte) error {
	frame := make([]byte, len(pcm))
	copy(frame, pcm)
	select {
	case s.out <- frame:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// streamAudio keeps frames flowing for the whole call, sending silence
// whenever the agent has nothing queued.
//
// This is not a nicety. Asterisk's AudioSocket gives up after two seconds
// without traffic — MAX_WAIT_TIMEOUT_MSEC in app_audiosocket.c, which the
// source notes is hard-coded — so a server that only speaks when it has
// something to say gets hung up on mid-call. It is a real-time media stream,
// not a message channel.
func streamAudio(ctx context.Context, conn net.Conn, out <-chan []byte, pace time.Duration) {
	silence := media.Silence(media.FrameBytes)
	tick := time.NewTicker(pace)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			frame := silence
			select {
			case queued := <-out:
				frame = queued
			default: // nothing to say; keep the stream alive
			}
			if err := writeFrame(conn, asAudio8k, frame); err != nil {
				return // the call is going away; the pump loop reports why
			}
		}
	}
}

func readFrame(r io.Reader) (kind byte, payload []byte, err error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[1:3]))
	if n == 0 {
		return hdr[0], nil, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return hdr[0], buf, nil
}

func writeFrame(w io.Writer, kind byte, payload []byte) error {
	if len(payload) > maxFrame {
		return fmt.Errorf("gateway: audiosocket: frame of %d bytes exceeds %d", len(payload), maxFrame)
	}
	buf := make([]byte, 0, 3+len(payload))
	buf = append(buf, kind, byte(len(payload)>>8), byte(len(payload)))
	buf = append(buf, payload...)
	_, err := w.Write(buf)
	return err
}

// formatUUID renders the 16 raw bytes of the id frame in the canonical form the
// dialplan used, so the two can be matched.
func formatUUID(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
