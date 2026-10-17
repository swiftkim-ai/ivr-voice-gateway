package eventlog

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c1.jsonl")
	w, err := Create(path, "c1")
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	w.Now = func() time.Time { return fixed }

	if _, err := w.Append("call_started", map[string]any{"hint": "h"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append("reply", map[string]any{"text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	events, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("seqs = %d,%d, want 1,2", events[0].Seq, events[1].Seq)
	}
	if events[0].CallID != "c1" {
		t.Fatalf("call id = %q", events[0].CallID)
	}
	if !events[0].At.Equal(fixed) {
		t.Fatalf("at = %v, want %v", events[0].At, fixed)
	}
	if events[1].Data["text"] != "hello" {
		t.Fatalf("data = %v", events[1].Data)
	}
}
