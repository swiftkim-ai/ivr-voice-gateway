package flowmap

import (
	"testing"

	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
)

func TestBuildFromScenarioLogs(t *testing.T) {
	// Simulates the event logs from two scenario runs against the same IVR:
	// one pressing 1 (sales), the other pressing 2 (support).
	salesPath := []eventlog.Event{
		{Type: "call_started", Data: map[string]any{"hint": "+1555", "scenario": "sales-path"}},
		{Type: "step", Data: map[string]any{"label": "greeting", "index": 0}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "1"}}},
		{Type: "step", Data: map[string]any{"label": "sales_menu", "index": 1}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "2"}}},
		{Type: "step", Data: map[string]any{"label": "new_orders", "index": 2}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}

	supportPath := []eventlog.Event{
		{Type: "call_started", Data: map[string]any{"hint": "+1555", "scenario": "support-path"}},
		{Type: "step", Data: map[string]any{"label": "greeting", "index": 0}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "2"}}},
		{Type: "step", Data: map[string]any{"label": "support_menu", "index": 1}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "1"}}},
		{Type: "step", Data: map[string]any{"label": "open_ticket", "index": 2}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}

	m := Build("+1555", [][]eventlog.Event{salesPath, supportPath})

	if len(m.Nodes) != 5 {
		t.Fatalf("nodes = %d, want 5 (greeting, sales_menu, new_orders, support_menu, open_ticket)", len(m.Nodes))
	}
	if len(m.Edges) != 4 {
		t.Fatalf("edges = %d, want 4", len(m.Edges))
	}

	nodeMap := map[string]Node{}
	for _, n := range m.Nodes {
		nodeMap[n.ID] = n
	}
	if !nodeMap["new_orders"].Terminal {
		t.Error("new_orders should be terminal")
	}
	if !nodeMap["open_ticket"].Terminal {
		t.Error("open_ticket should be terminal")
	}
	if nodeMap["greeting"].Terminal {
		t.Error("greeting should not be terminal (it has outgoing edges)")
	}

	edgeByInput := map[string]Edge{}
	for _, e := range m.Edges {
		edgeByInput[e.From+":"+e.Input] = e
	}

	if e := edgeByInput["greeting:1"]; e.To != "sales_menu" {
		t.Errorf("greeting --1--> %s, want sales_menu", e.To)
	}
	if e := edgeByInput["greeting:2"]; e.To != "support_menu" {
		t.Errorf("greeting --2--> %s, want support_menu", e.To)
	}
}

func TestBuildDuplicateEdgesDeduped(t *testing.T) {
	// Two scenarios that traverse the same first edge should produce only one
	// edge in the map.
	run1 := []eventlog.Event{
		{Type: "step", Data: map[string]any{"label": "greeting"}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "1"}}},
		{Type: "step", Data: map[string]any{"label": "sales"}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}
	run2 := []eventlog.Event{
		{Type: "step", Data: map[string]any{"label": "greeting"}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "1"}}},
		{Type: "step", Data: map[string]any{"label": "sales"}},
		{Type: "action", Data: map[string]any{"kind": "send_dtmf", "args": map[string]any{"digit": "3"}}},
		{Type: "step", Data: map[string]any{"label": "special"}},
		{Type: "call_ended", Data: map[string]any{"reason": "scenario_complete"}},
	}

	m := Build("+1555", [][]eventlog.Event{run1, run2})
	if len(m.Nodes) != 3 {
		t.Errorf("nodes = %d, want 3", len(m.Nodes))
	}
	if len(m.Edges) != 2 {
		t.Errorf("edges = %d, want 2 (greeting->sales:1, sales->special:3)", len(m.Edges))
	}
}

func TestDiffDetectsRegression(t *testing.T) {
	// before: greeting --1--> sales --2--> new_orders (terminal)
	// after:  greeting --1--> sales --2--> error_page (terminal, different destination)
	before := FlowMap{
		Nodes: []Node{
			{ID: "greeting", Label: "greeting"},
			{ID: "sales", Label: "sales"},
			{ID: "new_orders", Label: "new_orders", Terminal: true},
		},
		Edges: []Edge{
			{From: "greeting", To: "sales", Input: "1"},
			{From: "sales", To: "new_orders", Input: "2"},
		},
	}
	after := FlowMap{
		Nodes: []Node{
			{ID: "greeting", Label: "greeting"},
			{ID: "sales", Label: "sales"},
			{ID: "error_page", Label: "error_page", Terminal: true},
		},
		Edges: []Edge{
			{From: "greeting", To: "sales", Input: "1"},
			{From: "sales", To: "error_page", Input: "2"},
		},
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

	if !has("removed", "node", "new_orders") {
		t.Error("should detect removed node new_orders")
	}
	if !has("added", "node", "error_page") {
		t.Error("should detect added node error_page")
	}
	if !has("removed", "edge", "sales->new_orders:2") {
		t.Error("should detect removed edge sales->new_orders:2")
	}
	if !has("added", "edge", "sales->error_page:2") {
		t.Error("should detect added edge sales->error_page:2")
	}
}
