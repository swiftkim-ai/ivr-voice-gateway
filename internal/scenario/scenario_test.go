package scenario

import (
	"testing"
	"time"
)

func TestParseSingle(t *testing.T) {
	data := []byte(`{
		"name": "check-sales",
		"target": "+15551234567",
		"steps": [
			{"label": "greeting", "wait": "3s", "press": "1"},
			{"label": "sales", "wait": "2s"}
		],
		"final_wait": "4s"
	}`)
	scs, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(scs) != 1 {
		t.Fatalf("got %d scenarios, want 1", len(scs))
	}
	sc := scs[0]
	if sc.Name != "check-sales" {
		t.Errorf("name = %q", sc.Name)
	}
	if sc.Target != "+15551234567" {
		t.Errorf("target = %q", sc.Target)
	}
	if len(sc.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(sc.Steps))
	}
	if sc.Steps[0].ParsedWait() != 3*time.Second {
		t.Errorf("step[0].wait = %v", sc.Steps[0].ParsedWait())
	}
	if sc.Steps[0].Press != "1" {
		t.Errorf("step[0].press = %q", sc.Steps[0].Press)
	}
	if sc.ParsedFinalWait() != 4*time.Second {
		t.Errorf("final_wait = %v", sc.ParsedFinalWait())
	}
}

func TestParseArray(t *testing.T) {
	data := []byte(`[
		{"name": "path-1", "steps": [{"label": "a", "wait": "1s"}]},
		{"name": "path-2", "steps": [{"label": "b", "wait": "2s"}]}
	]`)
	scs, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(scs) != 2 {
		t.Fatalf("got %d, want 2", len(scs))
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"no name", `{"steps": [{"label":"a","wait":"1s"}]}`},
		{"no steps", `{"name":"x","steps":[]}`},
		{"no label", `{"name":"x","steps":[{"wait":"1s"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.data)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDefaultFinalWait(t *testing.T) {
	sc := Scenario{Name: "x"}
	if sc.ParsedFinalWait() != 5*time.Second {
		t.Errorf("default final_wait = %v, want 5s", sc.ParsedFinalWait())
	}
}
