package engine

import (
	"math"
	"time"

	"github.com/yaadmemory/yaad/internal/storage"
)

// DecayConfig controls decay behaviour.
type DecayConfig struct {
	HalfLifeDays  float64 // default 30
	MinConfidence float64 // default 0.1 — below this, eligible for GC
	BoostOnAccess float64 // default 0.2
}

var DefaultDecayConfig = DecayConfig{
	HalfLifeDays:  30,
	MinConfidence: 0.1,
	BoostOnAccess: 0.2,
}

// RunDecay applies half-life decay to all nodes in the store.
// Orphan nodes (0 edges) and superseded nodes decay 2× faster.
func RunDecay(store *storage.Store, cfg DecayConfig) error {
	nodes, err := store.ListNodes(storage.NodeFilter{})
	if err != nil {
		return err
	}
	now := time.Now()
	for _, n := range nodes {
		if n.Confidence <= 0 {
			continue // already archived
		}
		ref := n.UpdatedAt
		if !n.AccessedAt.IsZero() && n.AccessedAt.After(ref) {
			ref = n.AccessedAt
		}
		days := now.Sub(ref).Hours() / 24
		if days <= 0 {
			continue
		}

		multiplier := 1.0
		// Orphan nodes decay 2× faster
		edges, _ := store.GetEdgesFrom(n.ID)
		edgesTo, _ := store.GetEdgesTo(n.ID)
		if len(edges)+len(edgesTo) == 0 {
			multiplier = 2.0
		}
		// Superseded nodes decay 2× faster
		if n.Type == "bug" || n.Type == "decision" {
			for _, e := range edges {
				if e.Type == "supersedes" {
					multiplier = 2.0
					break
				}
			}
		}

		// Half-life formula: confidence *= 0.5^(days / half_life * multiplier)
		decay := math.Pow(0.5, days/cfg.HalfLifeDays*multiplier)
		n.Confidence = math.Max(n.Confidence*decay, 0)
		if err := store.UpdateNode(n); err != nil {
			return err
		}
	}
	return nil
}

// GarbageCollect removes nodes below min_confidence (except anchors: file/entity).
func GarbageCollect(store *storage.Store, cfg DecayConfig) (int, error) {
	nodes, err := store.ListNodes(storage.NodeFilter{})
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, n := range nodes {
		if n.Type == "file" || n.Type == "entity" {
			continue // keep anchors
		}
		if n.Confidence < cfg.MinConfidence {
			if err := store.DeleteNode(n.ID); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// BoostNode increases confidence of a node on access (capped at 1.0).
func BoostNode(store *storage.Store, id string, boost float64) error {
	n, err := store.GetNode(id)
	if err != nil {
		return err
	}
	n.Confidence = math.Min(n.Confidence+boost, 1.0)
	n.AccessCount++
	n.AccessedAt = time.Now()
	return store.UpdateNode(n)
}
