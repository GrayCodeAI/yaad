package engine

import (
	"context"
	"sort"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

// ProactiveContext predicts what context the agent will likely need next
// by analyzing recent access patterns and graph connectivity.
type ProactiveContext struct {
	eng    *Engine
	search *HybridSearch
}

// NewProactiveContext creates a proactive context predictor.
func NewProactiveContext(eng *Engine, search *HybridSearch) *ProactiveContext {
	return &ProactiveContext{eng: eng, search: search}
}

// Predict returns nodes likely needed in the next session based on:
// 1. Recently accessed nodes and their neighbors
// 2. Active tasks and their dependencies
// 3. High-centrality nodes in the project subgraph
func (p *ProactiveContext) Predict(ctx context.Context, project string, budget int) ([]*storage.Node, error) {
	candidates := map[string]*storage.Node{}

	// 1. Recently accessed nodes (last 5 sessions worth)
	recent, err := p.eng.store.ListNodes(storage.NodeFilter{Project: project})
	if err != nil {
		return nil, err
	}
	// Sort by access time descending
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].AccessedAt.After(recent[j].AccessedAt)
	})
	// Take top 5 most recently accessed and expand their neighborhoods
	for i, n := range recent {
		if i >= 5 {
			break
		}
		candidates[n.ID] = n
		neighbors, _ := p.eng.store.GetNeighbors(n.ID)
		for _, nb := range neighbors {
			candidates[nb.ID] = nb
		}
	}

	// 2. Active tasks and their dependencies
	tasks, _ := p.eng.store.ListNodes(storage.NodeFilter{Type: "task", Project: project})
	for _, t := range tasks {
		if t.Confidence > 0.3 {
			candidates[t.ID] = t
			// Pull in nodes this task depends on
			edges, _ := p.eng.store.GetEdgesFrom(t.ID)
			for _, e := range edges {
				if e.Type == "depends_on" || e.Type == "relates_to" {
					if n, err := p.eng.store.GetNode(e.ToID); err == nil {
						candidates[n.ID] = n
					}
				}
			}
		}
	}

	// 3. High-centrality nodes (many inbound edges = important)
	all, _ := p.eng.store.ListNodes(storage.NodeFilter{Project: project})
	type centNode struct {
		node     *storage.Node
		inDegree int
	}
	var ranked []centNode
	for _, n := range all {
		edges, _ := p.eng.store.GetEdgesTo(n.ID)
		ranked = append(ranked, centNode{n, len(edges)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].inDegree > ranked[j].inDegree
	})
	for i, cn := range ranked {
		if i >= 10 {
			break
		}
		candidates[cn.node.ID] = cn.node
	}

	// Collect, sort by score, trim to budget
	nodes := make([]*storage.Node, 0, len(candidates))
	for _, n := range candidates {
		nodes = append(nodes, n)
	}
	sortByScore(nodes)
	return TrimToTokenBudget(nodes, budget), nil
}
