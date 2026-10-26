package flowmap

import "sort"

// Change is one difference between two flow maps.
type Change struct {
	Kind    string `json:"kind"`             // "added", "removed", "changed"
	Element string `json:"element"`          // "node" or "edge"
	ID      string `json:"id"`               // node id or "from->to:input"
	Detail  string `json:"detail,omitempty"` // what changed
}

// Diff compares before and after, returning the changes needed to turn before
// into after. An empty slice means the maps are equivalent.
func Diff(before, after FlowMap) []Change {
	var changes []Change

	bn := nodeIndex(before.Nodes)
	an := nodeIndex(after.Nodes)

	for id := range bn {
		if _, ok := an[id]; !ok {
			changes = append(changes, Change{Kind: "removed", Element: "node", ID: id})
		}
	}
	for id, n := range an {
		old, ok := bn[id]
		if !ok {
			changes = append(changes, Change{Kind: "added", Element: "node", ID: id})
			continue
		}
		if old.Terminal != n.Terminal {
			detail := "became terminal"
			if !n.Terminal {
				detail = "no longer terminal"
			}
			changes = append(changes, Change{Kind: "changed", Element: "node", ID: id, Detail: detail})
		}
	}

	be := edgeIndex(before.Edges)
	ae := edgeIndex(after.Edges)

	for key := range be {
		if _, ok := ae[key]; !ok {
			changes = append(changes, Change{Kind: "removed", Element: "edge", ID: key})
		}
	}
	for key := range ae {
		if _, ok := be[key]; !ok {
			changes = append(changes, Change{Kind: "added", Element: "edge", ID: key})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Element != changes[j].Element {
			return changes[i].Element < changes[j].Element
		}
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].ID < changes[j].ID
	})
	return changes
}

func nodeIndex(nodes []Node) map[string]Node {
	m := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	return m
}

func edgeKey(e Edge) string { return e.From + "->" + e.To + ":" + e.Input }

func edgeIndex(edges []Edge) map[string]Edge {
	m := make(map[string]Edge, len(edges))
	for _, e := range edges {
		m[edgeKey(e)] = e
	}
	return m
}
