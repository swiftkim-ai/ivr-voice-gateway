// Package scenario defines the scripts that drive outbound IVR test calls.
//
// A scenario is a timed sequence of steps: wait for the remote IVR to speak,
// then press a digit. Multiple scenarios together map every path through the
// IVR, and the flow map (package flowmap) is assembled from their event logs.
package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Scenario is one path through a remote IVR. Running it produces an event log;
// running several builds the flow map.
type Scenario struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Steps  []Step `json:"steps"`
	// FinalWait is how long to observe after the last step before hanging up.
	// Parsed as a Go duration string; defaults to 5s.
	FinalWait string `json:"final_wait,omitempty"`
}

// Step is one beat in the scenario: wait, then act.
type Step struct {
	Label string `json:"label"`
	// Wait is the pause before acting, as a Go duration string ("3s", "500ms").
	// During this time, remote audio and DTMF are observed but not acted on.
	Wait string `json:"wait"`
	// Press is the DTMF digit(s) to send after the wait. Empty means observe
	// only (useful for the final step, which just records the outcome).
	Press string `json:"press,omitempty"`
}

// ParsedFinalWait returns the parsed FinalWait, defaulting to 5s.
func (s Scenario) ParsedFinalWait() time.Duration {
	if s.FinalWait == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(s.FinalWait)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

// ParsedWait returns the parsed Wait duration.
func (st Step) ParsedWait() time.Duration {
	d, err := time.ParseDuration(st.Wait)
	if err != nil {
		return 0
	}
	return d
}

// Load reads scenarios from a JSON file. The file may contain a single
// scenario object or an array of them.
func Load(path string) ([]Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scenario: load %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes scenarios from JSON bytes.
func Parse(data []byte) ([]Scenario, error) {
	var scenarios []Scenario
	if err := json.Unmarshal(data, &scenarios); err == nil && len(scenarios) > 0 {
		return validate(scenarios)
	}
	var single Scenario
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("scenario: parse: %w", err)
	}
	return validate([]Scenario{single})
}

func validate(scenarios []Scenario) ([]Scenario, error) {
	for i, sc := range scenarios {
		if sc.Name == "" {
			return nil, fmt.Errorf("scenario[%d]: name is required", i)
		}
		if len(sc.Steps) == 0 {
			return nil, fmt.Errorf("scenario %q: at least one step is required", sc.Name)
		}
		for j, st := range sc.Steps {
			if st.Label == "" {
				return nil, fmt.Errorf("scenario %q step[%d]: label is required", sc.Name, j)
			}
		}
	}
	return scenarios, nil
}
