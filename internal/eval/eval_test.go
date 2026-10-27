package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/swiftkimani/ivr-voice-gateway/internal/llm"
)

func writeFixture(t *testing.T, dir, name string, f llm.Fixture) {
	t.Helper()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvalPassingScenario(t *testing.T) {
	dir := t.TempDir()

	writeFixture(t, dir, "greeting.json", llm.Fixture{
		Exchanges: []llm.Exchange{
			{Response: llm.Response{Text: "Hello! How can I help you?"}},
			{Response: llm.Response{
				Text:      "Transferring you now.",
				ToolCalls: []llm.ToolCall{{Name: "transfer", Args: map[string]any{"dest": "200"}}},
			}},
		},
	})

	scenarios := []Scenario{{
		Name:    "greeting-and-transfer",
		Fixture: "greeting.json",
		Inputs:  []string{"Transfer me to sales"},
		Expect: Expect{
			Actions:      []string{"transfer"},
			TextContains: []string{"transferring"},
		},
	}}

	rep := Run(context.Background(), scenarios, dir)
	if !rep.OK() {
		t.Fatalf("expected pass, got failures: %v", rep.Results[0].Failures)
	}
	if rep.Passed != 1 {
		t.Fatalf("expected 1 passed, got %d", rep.Passed)
	}
}

func TestEvalFailingScenario(t *testing.T) {
	dir := t.TempDir()

	writeFixture(t, dir, "no-transfer.json", llm.Fixture{
		Exchanges: []llm.Exchange{
			{Response: llm.Response{Text: "Hello!"}},
			{Response: llm.Response{Text: "I cannot do that."}},
		},
	})

	scenarios := []Scenario{{
		Name:    "expects-transfer-but-none",
		Fixture: "no-transfer.json",
		Inputs:  []string{"Transfer me"},
		Expect: Expect{
			Actions: []string{"transfer"},
		},
	}}

	rep := Run(context.Background(), scenarios, dir)
	if rep.OK() {
		t.Fatal("expected failure")
	}
	if rep.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", rep.Failed)
	}
}

func TestEvalTextAbsent(t *testing.T) {
	dir := t.TempDir()

	writeFixture(t, dir, "polite.json", llm.Fixture{
		Exchanges: []llm.Exchange{
			{Response: llm.Response{Text: "Hello! Welcome."}},
			{Response: llm.Response{Text: "Let me help you with that."}},
		},
	})

	scenarios := []Scenario{{
		Name:    "no-sorry",
		Fixture: "polite.json",
		Inputs:  []string{"Help me"},
		Expect: Expect{
			TextContains: []string{"help"},
			TextAbsent:   []string{"sorry"},
		},
	}}

	rep := Run(context.Background(), scenarios, dir)
	if !rep.OK() {
		t.Fatalf("expected pass, got: %v", rep.Results[0].Failures)
	}
}

func TestEvalMissingFixture(t *testing.T) {
	scenarios := []Scenario{{
		Name:    "missing",
		Fixture: "nonexistent.json",
		Inputs:  []string{"hi"},
	}}

	rep := Run(context.Background(), scenarios, "/nonexistent")
	if rep.OK() {
		t.Fatal("expected failure for missing fixture")
	}
	if len(rep.Results[0].Failures) == 0 {
		t.Fatal("expected failure message")
	}
}

func TestLoadBank(t *testing.T) {
	dir := t.TempDir()
	bank := []Scenario{
		{Name: "test-1", Fixture: "f1.json", Inputs: []string{"hi"}},
		{Name: "test-2", Fixture: "f2.json", Inputs: []string{"bye"}},
	}
	data, _ := json.MarshalIndent(bank, "", "  ")
	path := filepath.Join(dir, "bank.json")
	os.WriteFile(path, data, 0o644)

	loaded, err := LoadBank(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 scenarios, got %d", len(loaded))
	}
}
