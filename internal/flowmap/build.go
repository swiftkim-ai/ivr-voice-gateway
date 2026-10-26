package flowmap

import (
	"fmt"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/internal/eventlog"
)

// Build assembles a flow map from one or more outbound call event logs. Each
// log traces one scenario path through the IVR; the union of all paths is the
// map. Step events mark nodes; send_dtmf actions between steps mark edges.
func Build(target string, logs [][]eventlog.Event) FlowMap {
	m := FlowMap{
		Version:  MapVersion,
		Target:   target,
		MappedAt: time.Now().UTC(),
	}

	nodes := map[string]*Node{}
	edges := map[string]Edge{}

	for _, events := range logs {
		path := extractPath(events)
		for i, ps := range path {
			if _, ok := nodes[ps.label]; !ok {
				nodes[ps.label] = &Node{ID: ps.label, Label: ps.label}
			}
			if ps.terminal {
				nodes[ps.label].Terminal = true
			}
			if i > 0 && path[i-1].digit != "" {
				key := path[i-1].label + "->" + ps.label + ":" + path[i-1].digit
				if _, ok := edges[key]; !ok {
					edges[key] = Edge{
						From:  path[i-1].label,
						To:    ps.label,
						Input: path[i-1].digit,
					}
				}
			}
		}
	}

	for _, n := range nodes {
		m.Nodes = append(m.Nodes, *n)
	}
	for _, e := range edges {
		m.Edges = append(m.Edges, e)
	}
	m.Normalize()
	return m
}

type pathStep struct {
	label    string
	digit    string // DTMF pressed AFTER this step, before the next
	terminal bool
}

// extractPath reads step and action events from one call log into an ordered
// list of labelled steps with the digit pressed between them.
func extractPath(events []eventlog.Event) []pathStep {
	var path []pathStep
	var cur *pathStep

	for _, ev := range events {
		switch ev.Type {
		case "step":
			if cur != nil {
				path = append(path, *cur)
			}
			cur = &pathStep{label: str(ev.Data["label"])}

		case "action":
			if cur != nil && str(ev.Data["kind"]) == "send_dtmf" {
				if args, ok := ev.Data["args"].(map[string]any); ok {
					cur.digit = str(args["digit"])
				}
			}

		case "call_ended":
			if cur != nil {
				cur.terminal = true
				path = append(path, *cur)
				cur = nil
			}
		}
	}
	if cur != nil {
		cur.terminal = true
		path = append(path, *cur)
	}
	return path
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
