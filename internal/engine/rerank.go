package engine

import (
	"sort"
	"time"

	"github.com/yaadmemory/yaad/internal/storage"
)

// Rerank re-scores nodes combining RRF score, graph centrality, recency, and confidence.
func Rerank(nodes []*ScoredNode, store *storage.Store) []*ScoredNode {
	now := time.Now()
	for _, sn := range nodes {
		sn.Score = combinedScore(sn, store, now)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Score > nodes[j].Score
	})
	return nodes
}

func combinedScore(sn *ScoredNode, store *storage.Store, now time.Time) float64 {
	n := sn.Node

	// Centrality: count inbound edges (more connections = more important)
	inbound, _ := store.GetEdgesTo(n.ID)
	centrality := 1.0 + float64(len(inbound))*0.1

	// Recency: exponential decay over 30 days
	recency := 1.0
	ref := n.UpdatedAt
	if !n.AccessedAt.IsZero() && n.AccessedAt.After(ref) {
		ref = n.AccessedAt
	}
	if !ref.IsZero() {
		days := now.Sub(ref).Hours() / 24
		recency = 1.0 / (1.0 + days/30.0)
	}

	// Tier boost: hot tier nodes rank higher
	tierBoost := 1.0
	switch n.Tier {
	case 1:
		tierBoost = 2.0
	case 2:
		tierBoost = 1.5
	}

	return sn.Score * n.Confidence * centrality * recency * tierBoost
}
