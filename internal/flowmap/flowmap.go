// Package flowmap represents an IVR's menu tree as observed by outbound
// scenario runs. Each node is a labelled point in the tree (a prompt heard,
// a queue reached, a dead end). Each edge is a transition made between nodes
// (a DTMF digit pressed). Multiple scenario runs build one map, and two maps
// can be diffed to detect regressions between releases.
package flowmap

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

const MapVersion = "1"

// FlowMap is the observed graph of a remote IVR's menu tree.
type FlowMap struct {
	Version  string    `json:"version"`
	Target   string    `json:"target"`
	MappedAt time.Time `json:"mapped_at"`
	Nodes    []Node    `json:"nodes"`
	Edges    []Edge    `json:"edges"`
}

// Node is one observed point in the IVR.
type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Terminal bool   `json:"terminal,omitempty"`
}

// Edge is one observed transition between nodes.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Input string `json:"input"`
}

// Save writes the map as indented JSON.
func Save(path string, m FlowMap) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("flowmap: marshal: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Load reads a map from a JSON file.
func Load(path string) (FlowMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FlowMap{}, fmt.Errorf("flowmap: load %s: %w", path, err)
	}
	var m FlowMap
	if err := json.Unmarshal(data, &m); err != nil {
		return FlowMap{}, fmt.Errorf("flowmap: parse %s: %w", path, err)
	}
	return m, nil
}

// Normalize sorts nodes and edges for deterministic comparison.
func (m *FlowMap) Normalize() {
	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].ID < m.Nodes[j].ID })
	sort.Slice(m.Edges, func(i, j int) bool {
		if m.Edges[i].From != m.Edges[j].From {
			return m.Edges[i].From < m.Edges[j].From
		}
		return m.Edges[i].Input < m.Edges[j].Input
	})
}
