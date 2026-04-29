package engine

import (
	"context"
	"sort"

	"github.com/GrayCodeAI/yaad/internal/embeddings"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

const rrfK = 60 // RRF constant

// HybridSearch performs 3-stage retrieval: BM25 → vector → graph expansion → RRF fusion.
type HybridSearch struct {
	store    storage.Storage
	graph    *graph.Graph
	provider embeddings.Provider // nil = BM25 only
}

// NewHybridSearch creates a hybrid search engine.
func NewHybridSearch(store storage.Storage, g *graph.Graph, provider embeddings.Provider) *HybridSearch {
	return &HybridSearch{store: store, graph: g, provider: provider}
}

// ScoredNode is a node with a combined relevance score.
type ScoredNode struct {
	Node  *storage.Node
	Score float64
}

// Search runs hybrid search and returns ranked nodes.
// 4-path retrieval: BM25 + vector + graph (intent-aware) + temporal recency
// Based on Hindsight's multi-strategy approach.
func (h *HybridSearch) Search(ctx context.Context, query string, opts RecallOpts) ([]*ScoredNode, error) {
	if opts.Depth == 0 {
		opts.Depth = 2
	}
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	// Classify query intent (MAGMA: intent-aware routing)
	queryIntent := intent.Classify(query)

	// Path 1: BM25 seed nodes
	bm25Nodes, _ := h.store.SearchNodes(query, opts.Limit*2)
	bm25Ranks := rankMap(bm25Nodes)

	// Path 2: Vector seed nodes (if provider available)
	vectorRanks := map[string]int{}
	if h.provider != nil {
		vec, err := h.provider.Embed(ctx, query)
		if err == nil {
			vectorRanks = h.vectorSearch(vec, opts.Limit*2)
		}
	}

	// Path 3: Intent-aware graph expansion (MAGMA: adaptive traversal)
	graphRanks := map[string]int{}
	rank := 1
	for _, id := range mergeKeys(bm25Ranks, vectorRanks) {
		ids, err := h.graph.IntentBFS(id, opts.Depth, queryIntent)
		if err != nil {
			continue
		}
		for _, nid := range ids {
			if _, seen := graphRanks[nid]; !seen {
				graphRanks[nid] = rank
				rank++
			}
		}
	}

	// Path 4: Temporal recency (Hindsight-inspired)
	// For When/temporal queries, boost recently accessed nodes
	temporalRanks := map[string]int{}
	if queryIntent == intent.IntentWhen {
		recent, _ := h.store.ListNodes(storage.NodeFilter{Project: opts.Project})
		// Sort by accessed_at descending (most recent first)
		for i := 1; i < len(recent); i++ {
			for j := i; j > 0 && recent[j].AccessedAt.After(recent[j-1].AccessedAt); j-- {
				recent[j], recent[j-1] = recent[j-1], recent[j]
			}
		}
		for i, n := range recent {
			if i >= opts.Limit*2 {
				break
			}
			temporalRanks[n.ID] = i + 1
		}
	}

	// Stage 3: RRF fusion of all 4 paths
	allIDs := mergeKeys(bm25Ranks, vectorRanks, graphRanks, temporalRanks)
	scored := make([]*ScoredNode, 0, len(allIDs))
	for _, id := range allIDs {
		node, err := h.store.GetNode(id)
		if err != nil {
			continue
		}
		if opts.Type != "" && node.Type != opts.Type {
			continue
		}
		if opts.Project != "" && node.Project != opts.Project {
			continue
		}
		rrf := rrfScore(bm25Ranks[id], vectorRanks[id], graphRanks[id], temporalRanks[id])
		scored = append(scored, &ScoredNode{Node: node, Score: rrf})
	}

	// Sort by RRF score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > opts.Limit {
		scored = scored[:opts.Limit]
	}
	return scored, nil
}

// vectorSearch returns a rank map of node IDs by cosine similarity.
func (h *HybridSearch) vectorSearch(queryVec []float32, limit int) map[string]int {
	all, err := h.store.AllEmbeddings()
	if err != nil {
		return nil
	}
	type pair struct {
		id    string
		score float32
	}
	var pairs []pair
	for id, vec := range all {
		pairs = append(pairs, pair{id, embeddings.Cosine(queryVec, vec)})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	ranks := map[string]int{}
	for i, p := range pairs {
		if i >= limit {
			break
		}
		ranks[p.id] = i + 1
	}
	return ranks
}

// rrfScore computes Reciprocal Rank Fusion score from multiple rank lists.
// rank=0 means not present in that list.
func rrfScore(ranks ...int) float64 {
	score := 0.0
	for _, r := range ranks {
		if r > 0 {
			score += 1.0 / float64(rrfK+r)
		}
	}
	return score
}

func rankMap(nodes []*storage.Node) map[string]int {
	m := map[string]int{}
	for i, n := range nodes {
		m[n.ID] = i + 1
	}
	return m
}

func mergeKeys(maps ...map[string]int) []string {
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
