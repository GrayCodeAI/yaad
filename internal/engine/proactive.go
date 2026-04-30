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

	// Load all project nodes once (reused for recent, tasks, centrality)
	all, err := p.eng.store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return nil, err
	}

	// 1. Recently accessed nodes (last 5 sessions worth)
	recent := make([]*storage.Node, len(all))
	copy(recent, all)
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].AccessedAt.After(recent[j].AccessedAt)
	})
	for i, n := range recent {
		if i >= 5 {
			break
		}
		candidates[n.ID] = n
		neighbors, _ := p.eng.store.GetNeighbors(ctx, n.ID)
		for _, nb := range neighbors {
			candidates[nb.ID] = nb
		}
	}

	// 2. Active tasks and their dependencies
	for _, t := range all {
		if t.Type != "task" || t.Confidence <= 0.3 {
			continue
		}
		candidates[t.ID] = t
		edges, _ := p.eng.store.GetEdgesFrom(ctx, t.ID)
		for _, e := range edges {
			if e.Type == "depends_on" || e.Type == "relates_to" {
				if n, err := p.eng.store.GetNode(ctx, e.ToID); err == nil {
					candidates[n.ID] = n
				}
			}
		}
	}

	// 3. High-centrality nodes (many inbound edges = important)
	// Use CountEdges to avoid loading full edge objects per node
	type centNode struct {
		node     *storage.Node
		inDegree int
	}
	var ranked []centNode
	for _, n := range all {
		inbound, _, _ := p.eng.store.CountEdges(ctx, n.ID)
		ranked = append(ranked, centNode{n, inbound})
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
