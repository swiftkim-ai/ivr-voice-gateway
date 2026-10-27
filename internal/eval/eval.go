// Package eval runs a bank of call scenarios against the engine and reports
// which ones passed. Each scenario is a recorded call fixture (an LLM exchange
// sequence) plus a set of assertions (expected actions, expected text patterns).
// A passing scenario means the engine still produces the same decisions for the
// same model outputs — so a prompt change or code refactor can be checked
// against the bank in CI.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/swiftkimani/ivr-voice-gateway/internal/brain"
	"github.com/swiftkimani/ivr-voice-gateway/internal/call"
	"github.com/swiftkimani/ivr-voice-gateway/internal/llm"
)

// Scenario is one test case in the eval bank.
type Scenario struct {
	Name    string   `json:"name"`
	Fixture string   `json:"fixture"`
	Inputs  []string `json:"inputs"`
	Expect  Expect   `json:"expect"`
}

// Expect is what the scenario asserts about the call.
type Expect struct {
	Actions      []string `json:"actions,omitempty"`
	TextContains []string `json:"text_contains,omitempty"`
	TextAbsent   []string `json:"text_absent,omitempty"`
	EndReason    string   `json:"end_reason,omitempty"`
}

// Result is the outcome of running one scenario.
type Result struct {
	Name     string   `json:"name"`
	Pass     bool     `json:"pass"`
	Failures []string `json:"failures,omitempty"`
}

// Report is the outcome of running the full bank.
type Report struct {
	Total   int      `json:"total"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Results []Result `json:"results"`
}

// OK reports whether every scenario passed.
func (r Report) OK() bool { return r.Failed == 0 }

// LoadBank reads a scenario bank from a JSON file. The file is an array of
// Scenario objects.
func LoadBank(path string) ([]Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read bank: %w", err)
	}
	var scenarios []Scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		return nil, fmt.Errorf("eval: parse bank: %w", err)
	}
	return scenarios, nil
}

// Run executes every scenario in the bank and returns the report.
func Run(ctx context.Context, scenarios []Scenario, fixtureDir string) Report {
	var rep Report
	rep.Total = len(scenarios)
	for _, sc := range scenarios {
		r := runOne(ctx, sc, fixtureDir)
		rep.Results = append(rep.Results, r)
		if r.Pass {
			rep.Passed++
		} else {
			rep.Failed++
		}
	}
	return rep
}

func runOne(ctx context.Context, sc Scenario, fixtureDir string) Result {
	res := Result{Name: sc.Name}

	fixPath := sc.Fixture
	if !filepath.IsAbs(fixPath) {
		fixPath = filepath.Join(fixtureDir, fixPath)
	}
	fix, err := llm.LoadFixture(fixPath)
	if err != nil {
		res.Failures = append(res.Failures, fmt.Sprintf("load fixture: %v", err))
		return res
	}

	comp := llm.RecordedFromFixture(fix)
	b := llm.NewBrain(llm.BrainConfig{
		SystemPrompt: "You are a helpful IVR agent.",
		Completer:    comp,
	})

	sink := &call.MemorySink{}
	c := &call.Call{
		ID: sc.Name, Hint: "eval",
		Brain: b, Speaker: brain.Silence{},
		Sink: sink,
	}

	if err := c.Start(); err != nil {
		res.Failures = append(res.Failures, fmt.Sprintf("start: %v", err))
		return res
	}

	// Greeting
	if err := c.Respond(ctx); err != nil {
		res.Failures = append(res.Failures, fmt.Sprintf("greeting: %v", err))
		return res
	}

	for _, input := range sc.Inputs {
		sink.Reset()
		if strings.HasPrefix(input, "dtmf:") {
			digit := strings.TrimPrefix(input, "dtmf:")
			if err := c.Digit(ctx, digit, false); err != nil {
				res.Failures = append(res.Failures, fmt.Sprintf("digit %s: %v", digit, err))
				return res
			}
		} else {
			if err := c.Input(ctx, input); err != nil {
				res.Failures = append(res.Failures, fmt.Sprintf("input %q: %v", input, err))
				return res
			}
		}
	}

	if sc.Expect.EndReason != "" {
		_ = c.End(sc.Expect.EndReason)
	}

	res.Failures = check(sink, sc.Expect)
	res.Pass = len(res.Failures) == 0
	return res
}

func check(sink *call.MemorySink, exp Expect) []string {
	var failures []string
	msgs := sink.Messages()

	var actions []string
	var texts []string
	for _, m := range msgs {
		if m.Type == "action" {
			actions = append(actions, m.Kind)
		}
		if m.Type == "reply" {
			texts = append(texts, m.Text)
		}
	}

	for _, want := range exp.Actions {
		found := false
		for _, got := range actions {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			failures = append(failures, fmt.Sprintf("expected action %q not found (got %v)", want, actions))
		}
	}

	allText := strings.Join(texts, " ")
	for _, substr := range exp.TextContains {
		if !strings.Contains(strings.ToLower(allText), strings.ToLower(substr)) {
			failures = append(failures, fmt.Sprintf("expected text to contain %q", substr))
		}
	}
	for _, substr := range exp.TextAbsent {
		if strings.Contains(strings.ToLower(allText), strings.ToLower(substr)) {
			failures = append(failures, fmt.Sprintf("expected text NOT to contain %q", substr))
		}
	}

	return failures
}
