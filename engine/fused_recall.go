package engine

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/GrayCodeAI/yaad/intent"
	"github.com/GrayCodeAI/yaad/storage"
)

// fusedRRFK is the standard RRF constant used for fused recall scoring.
// A value of 60 is the canonical default from the Reciprocal Rank Fusion
// literature (Cormack et al. 2009), balancing emphasis between top-ranked
// and lower-ranked results across heterogeneous retrieval signals.
const fusedRRFK = 60

// FusedRecall performs multi-signal retrieval combining:
//  1. BM25 keyword search (via existing SearchNodes FTS5)
//  2. Graph traversal (via existing IntentBFS from seed nodes)
//  3. Recency signal (recently accessed/modified nodes)
//
// Then fuses results using Reciprocal Rank Fusion (RRF).
//
// This is inspired by mem0's multi-signal retrieval architecture which
// combines semantic search, BM25, and entity-graph traversal with RRF
// to produce higher-quality recall than any single signal alone.
//
// Unlike the existing Recall method (which uses BM25 seeds then graph
// expansion with a confidence*recency heuristic), FusedRecall treats each
// signal as an independent ranked list and merges them with a principled
// rank-fusion algorithm. This avoids the score-space mismatch problem
// where BM25 scores, graph distances, and timestamps are on different
// scales.
func (e *Engine) FusedRecall(ctx context.Context, opts RecallOpts) (*RecallResult, error) {
	atomic.AddInt64(&e.metrics.Recalls, 1)
	if err := ctx.Err(); err != nil {
		atomic.AddInt64(&e.metrics.Errors, 1)
		return nil, err
	}

	// Apply defaults
	if opts.Depth == 0 {
		opts.Depth = defaultRecallDepth
	}
	if opts.Limit == 0 {
		opts.Limit = defaultRecallLimit
	}

	// --- Signal 1: BM25 keyword search ---
	bm25Nodes, err := e.store.SearchNodes(ctx, opts.Query, opts.Limit*3)
	if err != nil {
		atomic.AddInt64(&e.metrics.Errors, 1)
		return nil, fmt.Errorf("fused recall bm25: %w", err)
	}
	bm25Nodes = filterNodes(bm25Nodes, opts)
	bm25Ranks := fusedRankMap(bm25Nodes)

	// Early exit: if BM25 finds nothing, there is nothing to fuse.
	if len(bm25Ranks) == 0 {
		return &RecallResult{}, nil
	}

	// --- Signal 2: Graph traversal (intent-aware BFS from BM25 seeds) ---
	queryIntent := intent.Classify(opts.Query)
	graphRanks := map[string]int{}
	graphRank := 1
	for _, seed := range bm25Nodes {
		if ctx.Err() != nil {
			break
		}
		ids, err := e.graph.IntentBFS(ctx, seed.ID, opts.Depth, queryIntent)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if _, seen := graphRanks[id]; !seen {
				graphRanks[id] = graphRank
				graphRank++
			}
		}
	}

	// --- Signal 3: Recency (recently accessed/modified nodes) ---
	recencyRanks := map[string]int{}
	recentNodes, err := e.store.ListNodes(ctx, storage.NodeFilter{Project: opts.Project})
	if err == nil && len(recentNodes) > 0 {
		// Sort by most-recently-accessed first, falling back to updated_at
		sort.Slice(recentNodes, func(i, j int) bool {
			ai := recentNodes[i].AccessedAt
			if ai.IsZero() {
				ai = recentNodes[i].UpdatedAt
			}
			aj := recentNodes[j].AccessedAt
			if aj.IsZero() {
				aj = recentNodes[j].UpdatedAt
			}
			return ai.After(aj)
		})
		cap := opts.Limit * 3
		if cap > len(recentNodes) {
			cap = len(recentNodes)
		}
		for i := 0; i < cap; i++ {
			n := recentNodes[i]
			if opts.Type != "" && n.Type != opts.Type {
				continue
			}
			recencyRanks[n.ID] = i + 1
		}
	}

	// --- Fusion: Reciprocal Rank Fusion ---
	allIDs := fusedMergeKeys(bm25Ranks, graphRanks, recencyRanks)

	// Batch-fetch all candidate nodes
	nodes, err := e.store.GetNodesBatch(ctx, allIDs)
	if err != nil {
		atomic.AddInt64(&e.metrics.Errors, 1)
		return nil, fmt.Errorf("fused recall batch fetch: %w", err)
	}
	nodeByID := make(map[string]*storage.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}

	// Score each candidate via RRF
	type scoredEntry struct {
		node  *storage.Node
		score float64
	}
	scored := make([]scoredEntry, 0, len(allIDs))
	for _, id := range allIDs {
		node, ok := nodeByID[id]
		if !ok {
			continue
		}
		// Apply type/tier/project filters
		if opts.Type != "" && node.Type != opts.Type {
			continue
		}
		if opts.Tier != 0 && node.Tier != opts.Tier {
			continue
		}
		if opts.Project != "" && node.Project != opts.Project {
			continue
		}
		// Skip archived nodes (confidence == 0)
		if node.Confidence <= 0 {
			continue
		}

		s := fusedRRFScore(bm25Ranks[id], graphRanks[id], recencyRanks[id])
		scored = append(scored, scoredEntry{node: node, score: s})
	}

	// Sort by fused RRF score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Apply limit
	if len(scored) > opts.Limit {
		scored = scored[:opts.Limit]
	}

	// Collect result nodes and log access
	resultNodes := make([]*storage.Node, len(scored))
	for i, s := range scored {
		e.access.Log(ctx, s.node.ID)
		resultNodes[i] = s.node
	}

	// Collect edges between result nodes for subgraph context
	resultIDs := make([]string, len(resultNodes))
	for i, n := range resultNodes {
		resultIDs[i] = n.ID
	}
	edges, _ := e.store.GetEdgesBetween(ctx, resultIDs)

	// Enforce token budget if specified
	if opts.Budget > 0 {
		resultNodes = TrimToTokenBudget(resultNodes, opts.Budget)
	}

	return &RecallResult{Nodes: resultNodes, Edges: edges}, nil
}

// fusedRRFScore computes the Reciprocal Rank Fusion score from three signals.
// For each signal where the item appears at rank r (1-based), the contribution
// is 1/(k+r). A rank of 0 means the item did not appear in that signal.
func fusedRRFScore(ranks ...int) float64 {
	score := 0.0
	for _, r := range ranks {
		if r > 0 {
			score += 1.0 / float64(fusedRRFK+r)
		}
	}
	return score
}

// fusedRankMap converts a slice of nodes into a 1-based rank map keyed by ID.
func fusedRankMap(nodes []*storage.Node) map[string]int {
	m := make(map[string]int, len(nodes))
	for i, n := range nodes {
		m[n.ID] = i + 1
	}
	return m
}

// fusedMergeKeys returns the deduplicated union of keys from multiple rank maps,
// preserving first-seen order.
func fusedMergeKeys(maps ...map[string]int) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	return keys
}
