// Package multiproject handles cross-project memory linking and global entity resolution.
package multiproject

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// LinkProjects creates a cross-project edge between two nodes in different stores.
// The edge is stored in the global store (dst) as a relates_to edge.
func LinkProjects(globalStore *storage.Store, nodeA, projectA, nodeB, projectB string) error {
	edge := &storage.Edge{
		ID:     uuid.New().String(),
		FromID: nodeA,
		ToID:   nodeB,
		Type:   "relates_to",
		Weight: 1.0,
		Metadata: `{"cross_project":true,"project_a":"` + projectA + `","project_b":"` + projectB + `"}`,
	}
	return globalStore.CreateEdge(edge)
}

// ResolveGlobalEntity finds or creates a global entity node that represents
// the same concept across multiple projects (e.g., "PostgreSQL", "jose").
func ResolveGlobalEntity(globalStore *storage.Store, name, entityType string) (*storage.Node, error) {
	hash := name + ":global:entity"
	existing, _ := globalStore.SearchNodeByHash(hashStr(hash), "global", "")
	if existing != nil {
		return existing, nil
	}
	node := &storage.Node{
		ID:          uuid.New().String(),
		Type:        entityType,
		Content:     name,
		ContentHash: hashStr(hash),
		Scope:       "global",
		Tier:        0,
		Confidence:  1.0,
		Version:     1,
	}
	return node, globalStore.CreateNode(node)
}

// CrossProjectSearch searches for nodes matching a query across multiple stores.
func CrossProjectSearch(stores []*storage.Store, query string, limit int) ([]*storage.Node, error) {
	var all []*storage.Node
	for _, store := range stores {
		nodes, err := store.SearchNodes(query, limit)
		if err != nil {
			continue
		}
		all = append(all, nodes...)
		if len(all) >= limit {
			break
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func hashStr(s string) string {
	// Simple deterministic hash for entity dedup
	h := uint64(14695981039346656037)
	for _, c := range s {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return fmt.Sprintf("%x", h)
}
