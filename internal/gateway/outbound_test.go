package gateway

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/control"
	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
	"github.com/swiftkimani/ivr-voice-gateway/internal/scenario"
)

func TestRunOutboundScenario(t *testing.T) {
	const uuid = "aabbccdd-1122-3344-5566-778899aabbcc"
	dir := t.TempDir()

	sc := scenario.Scenario{
		Name:   "test-sales",
		Target: "+15551234567",
		Steps: []scenario.Step{
			{Label: "greeting", Wait: "10ms", Press: "1"},
			{Label: "sales", Wait: "10ms"},
		},
		FinalWait: "10ms",
	}

	ctl := &control.Loopback{}
	server, client := net.Pipe()

	// Simulate Asterisk: UUID frame first, then audio, then terminate.
	// All writes in one goroutine — net.Pipe is not safe for concurrent writes.
	go func() {
		defer client.Close()
		// Drain server-to-client traffic (silence frames) in the background.
		go io.Copy(io.Discard, client)
		// UUID frame.
		writeTestFrame(client, asUUID, parseUUID(uuid))
		// A few audio frames as if the remote IVR is speaking.
		for i := 0; i < 5; i++ {
			writeTestFrame(client, asAudio8k, make([]byte, 320))
			time.Sleep(time.Millisecond)
		}
		// Wait for the scenario to run, then terminate.
		time.Sleep(200 * time.Millisecond)
		writeTestFrame(client, asTerminate, nil)
	}()

	opts := AudioSocketOptions{
		LogDir:      dir,
		Control:     ctl,
		FramePace:   time.Millisecond,
		ChannelWait: time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gotUUID, err := RunOutboundScenario(ctx, server, opts, sc)
	if err != nil {
		t.Fatal(err)
	}
	if gotUUID != uuid {
		t.Errorf("uuid = %q, want %q", gotUUID, uuid)
	}

	// Verify the event log was written.
	events, err := eventlog.Read(filepath.Join(dir, uuid+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	types := make(map[string]int)
	for _, ev := range events {
		types[ev.Type]++
	}

	if types["call_started"] != 1 {
		t.Errorf("call_started = %d", types["call_started"])
	}
	if types["step"] != 2 {
		t.Errorf("step = %d, want 2", types["step"])
	}
	if types["action"] != 1 {
		t.Errorf("action = %d, want 1 (send_dtmf for digit 1)", types["action"])
	}
	if types["call_ended"] != 1 {
		t.Errorf("call_ended = %d", types["call_ended"])
	}

	// Verify step labels.
	var labels []string
	for _, ev := range events {
		if ev.Type == "step" {
			labels = append(labels, ev.Data["label"].(string))
		}
	}
	if len(labels) != 2 || labels[0] != "greeting" || labels[1] != "sales" {
		t.Errorf("step labels = %v", labels)
	}
}

func TestOutboundScenarioBuildsAFlowMap(t *testing.T) {
	// Verify the event log from a scenario run can produce a valid flow map.
	const uuid = "11223344-5566-7788-99aa-bbccddeeff00"
	dir := t.TempDir()

	sc := scenario.Scenario{
		Name:   "sales-path",
		Target: "+15550001234",
		Steps: []scenario.Step{
			{Label: "greeting", Wait: "10ms", Press: "1"},
			{Label: "sales_menu", Wait: "10ms", Press: "2"},
			{Label: "new_orders", Wait: "10ms"},
		},
		FinalWait: "10ms",
	}

	server, client := net.Pipe()
	go func() {
		defer client.Close()
		go io.Copy(io.Discard, client)
		writeTestFrame(client, asUUID, parseUUID(uuid))
		time.Sleep(200 * time.Millisecond)
		writeTestFrame(client, asTerminate, nil)
	}()

	opts := AudioSocketOptions{
		LogDir:      dir,
		FramePace:   time.Millisecond,
		ChannelWait: time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := RunOutboundScenario(ctx, server, opts, sc)
	if err != nil {
		t.Fatal(err)
	}

	events, err := eventlog.Read(filepath.Join(dir, uuid+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	// Count step events.
	var steps int
	for _, ev := range events {
		if ev.Type == "step" {
			steps++
		}
	}
	if steps != 3 {
		t.Errorf("steps = %d, want 3", steps)
	}
}

func writeTestFrame(w io.Writer, kind byte, payload []byte) {
	hdr := [3]byte{kind, 0, 0}
	binary.BigEndian.PutUint16(hdr[1:3], uint16(len(payload)))
	w.Write(hdr[:])
	if len(payload) > 0 {
		w.Write(payload)
	}
}
