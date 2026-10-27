package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")

	original := Fixture{Exchanges: []Exchange{
		{
			Request:  Request{Messages: []Message{{Role: "user", Content: "hello"}}},
			Response: Response{Text: "Hi there!"},
		},
		{
			Request:  Request{Messages: []Message{{Role: "user", Content: "transfer me"}}},
			Response: Response{ToolCalls: []ToolCall{{Name: "transfer", Args: map[string]any{"dest": "200"}}}},
		},
	}}

	if err := SaveFixture(path, original); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFixture(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Exchanges) != 2 {
		t.Fatalf("expected 2 exchanges, got %d", len(loaded.Exchanges))
	}
	if loaded.Exchanges[0].Response.Text != "Hi there!" {
		t.Fatalf("unexpected text: %s", loaded.Exchanges[0].Response.Text)
	}
}

func TestRecordingCaptures(t *testing.T) {
	inner := &Recorded{Responses: []Response{
		{Text: "Hello"},
		{Text: "Goodbye"},
	}}
	rec := &Recording{Inner: inner}

	if _, err := rec.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "bye"}},
	}); err != nil {
		t.Fatal(err)
	}

	fix := rec.Fixture()
	if len(fix.Exchanges) != 2 {
		t.Fatalf("expected 2 recorded exchanges, got %d", len(fix.Exchanges))
	}
	if fix.Exchanges[0].Request.Messages[0].Content != "hi" {
		t.Fatalf("first request not recorded correctly")
	}
}

func TestRecordedFromFixture(t *testing.T) {
	fix := Fixture{Exchanges: []Exchange{
		{Response: Response{Text: "one"}},
		{Response: Response{Text: "two"}},
	}}
	comp := RecordedFromFixture(fix)

	r1, err := comp.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Text != "one" {
		t.Fatalf("expected 'one', got %q", r1.Text)
	}

	r2, err := comp.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Text != "two" {
		t.Fatalf("expected 'two', got %q", r2.Text)
	}
}

func TestLoadFixtureMissing(t *testing.T) {
	_, err := LoadFixture("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadFixtureMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0o644)

	_, err := LoadFixture(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
