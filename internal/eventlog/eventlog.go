// Package eventlog writes and reads the append-only record of a call.
//
// One call is one JSONL file: one event per line, each with a monotonic Seq and
// a UTC At. The live path and replay both use these events, so a call is a file
// that can be copied, diffed, and replayed. Events are never mutated or
// renumbered (ADR-0003).
package eventlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Event is one line of the log.
type Event struct {
	Seq    int            `json:"seq"`
	At     time.Time      `json:"at"`
	CallID string         `json:"call_id"`
	Type   string         `json:"type"`
	Data   map[string]any `json:"data,omitempty"`
}

// Writer appends events to one call's file. The zero value is not usable;
// construct with Create. Now is injectable so tests are deterministic.
type Writer struct {
	path   string
	callID string
	f      *os.File
	seq    int

	// Now returns the timestamp for each event. Defaults to time.Now().UTC().
	Now func() time.Time
}

// Create opens (creating or truncating) the log at path for callID.
func Create(path, callID string) (*Writer, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("eventlog: create dir: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("eventlog: create: %w", err)
	}
	return &Writer{
		path:   path,
		callID: callID,
		f:      f,
		Now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

// Append writes one event and returns it with its assigned Seq and At.
func (w *Writer) Append(typ string, data map[string]any) (Event, error) {
	w.seq++
	ev := Event{Seq: w.seq, At: w.Now(), CallID: w.callID, Type: typ, Data: data}
	line, err := json.Marshal(ev)
	if err != nil {
		return Event{}, fmt.Errorf("eventlog: marshal: %w", err)
	}
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return Event{}, fmt.Errorf("eventlog: write: %w", err)
	}
	return ev, nil
}

// Path is the file the writer targets.
func (w *Writer) Path() string { return w.path }

// Close flushes and closes the file.
func (w *Writer) Close() error { return w.f.Close() }

// Read loads every event from the log at path.
func Read(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open: %w", err)
	}
	defer f.Close()
	return ReadFrom(f)
}

// ReadFrom parses events from r until EOF.
func ReadFrom(r io.Reader) ([]Event, error) {
	var events []Event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("eventlog: parse line %d: %w", len(events)+1, err)
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("eventlog: scan: %w", err)
	}
	return events, nil
}
