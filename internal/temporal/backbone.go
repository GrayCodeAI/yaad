// Package temporal implements an immutable temporal backbone.
// Every memory node is auto-linked to the previous node in chronological order.
// Based on MAGMA's temporal graph and Zep's Graphiti temporal knowledge graph.
//
// The temporal backbone enables:
// - "When did we decide X?" → walk the timeline
// - "What happened before/after Y?" → traverse prev/next edges
// - Chronological ordering for session reconstruction
package temporal

import (
	"sync"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Backbone maintains the immutable temporal chain per project.
type Backbone struct {
	store    storage.Storage
	lastNode map[string]string // project → last node ID
	mu       sync.Mutex
}

func New(store storage.Storage) *Backbone {
	return &Backbone{store: store, lastNode: map[string]string{}}
}

// Link adds a node to the temporal backbone.
// Creates an immutable "next" edge from the previous node to this one.
// This edge is never modified or deleted — it's the ground truth timeline.
func (b *Backbone) Link(nodeID, project string) error {
	b.mu.Lock()
	prevID := b.lastNode[project]
	b.lastNode[project] = nodeID
	b.mu.Unlock()

	if prevID == "" || prevID == nodeID {
		return nil
	}

	return b.store.CreateEdge(&storage.Edge{
		ID:      uuid.New().String(),
		FromID:  prevID,
		ToID:    nodeID,
		Type:    "learned_in",
		Acyclic: true,
		Weight:  1.0,
	})
}

// Timeline returns nodes in chronological order for a project,
// walking the temporal backbone from the given start node.
func (b *Backbone) Timeline(startID string, direction string, limit int) ([]*storage.Node, error) {
	if limit <= 0 {
		limit = 20
	}

	var nodes []*storage.Node
	currentID := startID

	for i := 0; i < limit; i++ {
		node, err := b.store.GetNode(currentID)
		if err != nil {
			break
		}
		nodes = append(nodes, node)

		// Walk forward or backward
		var edges []*storage.Edge
		if direction == "forward" {
			edges, _ = b.store.GetEdgesFrom(currentID)
		} else {
			edges, _ = b.store.GetEdgesTo(currentID)
		}

		found := false
		for _, e := range edges {
			if e.Type == "learned_in" {
				if direction == "forward" {
					currentID = e.ToID
				} else {
					currentID = e.FromID
				}
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return nodes, nil
}
