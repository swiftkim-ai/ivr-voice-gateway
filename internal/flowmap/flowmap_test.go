package flowmap

import (
	"testing"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
)

func TestBuildSinglePath(t *testing.T) {
	events := []eventlog.Event{
		{Type: "call_started", Data: map[string]any{"hint": "+1555", "scenario": "test"}},
		{Type: "step", Data: map[string]any{"label": "greeting", "index": 0}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "1"}}},
		{Type: "step", Data: map[string]any{"label": "sales", "index": 1}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "2"}}},
		{Type: "step", Data: map[string]any{"label": "orders", "index": 2}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}

	m := Build("+1555", [][]eventlog.Event{events})
	if m.Version != MapVersion {
		t.Errorf("version = %q", m.Version)
	}
	if m.Target != "+1555" {
		t.Errorf("target = %q", m.Target)
	}
	if len(m.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(m.Nodes))
	}
	if len(m.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(m.Edges))
	}

	nodeMap := map[string]Node{}
	for _, n := range m.Nodes {
		nodeMap[n.ID] = n
	}
	if !nodeMap["orders"].Terminal {
		t.Error("orders should be terminal")
	}
	if nodeMap["greeting"].Terminal {
		t.Error("greeting should not be terminal")
	}

	edgeMap := map[string]Edge{}
	for _, e := range m.Edges {
		edgeMap[e.From+"->"+e.To] = e
	}
	if e, ok := edgeMap["greeting->sales"]; !ok || e.Input != "1" {
		t.Errorf("missing or wrong greeting->sales edge: %+v", e)
	}
	if e, ok := edgeMap["sales->orders"]; !ok || e.Input != "2" {
		t.Errorf("missing or wrong sales->orders edge: %+v", e)
	}
}

func TestBuildMergePaths(t *testing.T) {
	path1 := []eventlog.Event{
		{Type: "step", Data: map[string]any{"label": "greeting"}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "1"}}},
		{Type: "step", Data: map[string]any{"label": "sales"}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}
	path2 := []eventlog.Event{
		{Type: "step", Data: map[string]any{"label": "greeting"}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "2"}}},
		{Type: "step", Data: map[string]any{"label": "support"}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}

	m := Build("+1555", [][]eventlog.Event{path1, path2})
	if len(m.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (greeting, sales, support)", len(m.Nodes))
	}
	if len(m.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(m.Edges))
	}
}

func TestDiffIdentical(t *testing.T) {
	m := FlowMap{
		Nodes: []Node{{ID: "a", Label: "a"}, {ID: "b", Label: "b", Terminal: true}},
		Edges: []Edge{{From: "a", To: "b", Input: "1"}},
	}
	changes := Diff(m, m)
	if len(changes) != 0 {
		t.Fatalf("identical maps should have 0 changes, got %d", len(changes))
	}
}

func TestDiffAddedRemoved(t *testing.T) {
	before := FlowMap{
		Nodes: []Node{{ID: "a"}, {ID: "b", Terminal: true}},
		Edges: []Edge{{From: "a", To: "b", Input: "1"}},
	}
	after := FlowMap{
		Nodes: []Node{{ID: "a"}, {ID: "c", Terminal: true}},
		Edges: []Edge{{From: "a", To: "c", Input: "2"}},
	}
	changes := Diff(before, after)

	has := func(kind, element, id string) bool {
		for _, c := range changes {
			if c.Kind == kind && c.Element == element && c.ID == id {
				return true
			}
		}
		return false
	}

	if !has("removed", "node", "b") {
		t.Error("missing: removed node b")
	}
	if !has("added", "node", "c") {
		t.Error("missing: added node c")
	}
	if !has("removed", "edge", "a->b:1") {
		t.Error("missing: removed edge a->b:1")
	}
	if !has("added", "edge", "a->c:2") {
		t.Error("missing: added edge a->c:2")
	}
}

func TestDiffTerminalChanged(t *testing.T) {
	before := FlowMap{Nodes: []Node{{ID: "a", Terminal: false}}}
	after := FlowMap{Nodes: []Node{{ID: "a", Terminal: true}}}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Kind != "changed" || changes[0].Detail != "became terminal" {
		t.Errorf("unexpected changes: %+v", changes)
	}
}

func TestSaveLoad(t *testing.T) {
	m := FlowMap{
		Version:  MapVersion,
		Target:   "+1555",
		MappedAt: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
		Nodes:    []Node{{ID: "a", Label: "a"}, {ID: "b", Label: "b", Terminal: true}},
		Edges:    []Edge{{From: "a", To: "b", Input: "1"}},
	}
	path := t.TempDir() + "/map.json"
	if err := Save(path, m); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Target != m.Target {
		t.Errorf("target = %q, want %q", loaded.Target, m.Target)
	}
	if len(loaded.Nodes) != len(m.Nodes) {
		t.Errorf("nodes = %d, want %d", len(loaded.Nodes), len(m.Nodes))
	}
	if len(loaded.Edges) != len(m.Edges) {
		t.Errorf("edges = %d, want %d", len(loaded.Edges), len(m.Edges))
	}
}
