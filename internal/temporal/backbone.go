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
	"context"
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
func (b *Backbone) Link(ctx context.Context, nodeID, project string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	prevID := b.lastNode[project]
	if prevID == "" {
		// On restart, recover the tail of the temporal chain from storage.
		prevID = b.recoverTail(ctx, project)
	}
	b.lastNode[project] = nodeID
	b.mu.Unlock()

	if prevID == "" || prevID == nodeID {
		return nil
	}

	return b.store.CreateEdge(ctx, &storage.Edge{
		ID:      uuid.New().String(),
		FromID:  prevID,
		ToID:    nodeID,
		Type:    "learned_in",
		Acyclic: true,
		Weight:  1.0,
	})
}

// recoverTail finds the most recent node in the temporal chain by looking for
// a node that has an outgoing "learned_in" edge but no incoming "learned_in"
// from a newer node (i.e., the tail of the chain). Falls back to the most
// recently created node in the project.
func (b *Backbone) recoverTail(ctx context.Context, project string) string {
	nodes, err := b.store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil || len(nodes) == 0 {
		return ""
	}
	// Find nodes that have outgoing learned_in edges (they're in the chain)
	// The tail is the one with no outgoing learned_in edge
	for i := len(nodes) - 1; i >= 0; i-- {
		edges, _ := b.store.GetEdgesFrom(ctx, nodes[i].ID)
		hasOutgoing := false
		for _, e := range edges {
			if e.Type == "learned_in" {
				hasOutgoing = true
				break
			}
		}
		if !hasOutgoing {
			// Check it has at least one incoming learned_in (it's actually in the chain)
			edgesTo, _ := b.store.GetEdgesTo(ctx, nodes[i].ID)
			for _, e := range edgesTo {
				if e.Type == "learned_in" {
					return nodes[i].ID
				}
			}
		}
	}
	// Fallback: use the most recently created node
	if len(nodes) > 0 {
		return nodes[len(nodes)-1].ID
	}
	return ""
}

// Timeline returns nodes in chronological order for a project,
// walking the temporal backbone from the given start node.
func (b *Backbone) Timeline(ctx context.Context, startID string, direction string, limit int) ([]*storage.Node, error) {
	if limit <= 0 {
		limit = 20
	}

	var nodes []*storage.Node
	currentID := startID

	for i := 0; i < limit; i++ {
		node, err := b.store.GetNode(ctx, currentID)
		if err != nil {
			break
		}
		nodes = append(nodes, node)

		// Walk forward or backward
		var edges []*storage.Edge
		if direction == "forward" {
			edges, _ = b.store.GetEdgesFrom(ctx, currentID)
		} else {
			edges, _ = b.store.GetEdgesTo(ctx, currentID)
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
