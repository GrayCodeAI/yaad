// Package compact implements memory compaction — auto-summarize when
// the graph exceeds a token budget. Based on Engram and Letta approaches.
package compact

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Compactor summarizes old, low-confidence memories to keep the graph lean.
type Compactor struct {
	store     storage.Storage
	maxTokens int // max total tokens before compaction triggers
}

func New(store storage.Storage, maxTokens int) *Compactor {
	if maxTokens <= 0 {
		maxTokens = 50000 // ~200KB of content
	}
	return &Compactor{store: store, maxTokens: maxTokens}
}

// NeedsCompaction returns true if total content exceeds the token budget.
func (c *Compactor) NeedsCompaction(project string) (bool, int) {
	nodes, _ := c.store.ListNodes(storage.NodeFilter{Project: project})
	totalTokens := 0
	for _, n := range nodes {
		totalTokens += len(n.Content) / 4 // ~4 chars per token
	}
	return totalTokens > c.maxTokens, totalTokens
}

// Compact merges low-confidence, old memories of the same type into summary nodes.
// Returns the number of nodes compacted.
func (c *Compactor) Compact(project string) (int, error) {
	nodes, err := c.store.ListNodes(storage.NodeFilter{Project: project})
	if err != nil {
		return 0, err
	}

	// Group by type
	byType := map[string][]*storage.Node{}
	for _, n := range nodes {
		if n.Type == "file" || n.Type == "entity" || n.Type == "session" {
			continue // don't compact anchors or sessions
		}
		if n.Confidence < 0.5 && n.AccessCount < 3 {
			byType[n.Type] = append(byType[n.Type], n)
		}
	}

	compacted := 0
	for typ, group := range byType {
		if len(group) < 3 {
			continue // not enough to compact
		}

		// Build summary from group
		var contents []string
		var ids []string
		for _, n := range group {
			contents = append(contents, n.Content)
			ids = append(ids, n.ID)
		}

		summary := buildCompactSummary(typ, contents)

		// Create summary node
		summaryNode := &storage.Node{
			ID:          uuid.New().String(),
			Type:        typ,
			Content:     summary,
			ContentHash: fmt.Sprintf("compact:%s:%d", typ, len(ids)),
			Summary:     fmt.Sprintf("Compacted %d %s memories", len(ids), typ),
			Scope:       group[0].Scope,
			Project:     project,
			Tier:        3, // cold
			Confidence:  0.6,
			Version:     1,
		}
		if err := c.store.CreateNode(summaryNode); err != nil {
			continue
		}

		// Archive compacted nodes
		for _, id := range ids {
			old, _ := c.store.GetNode(id)
			if old != nil {
				c.store.SaveVersion(old.ID, old.Content, "compactor", "compacted into "+summaryNode.ID[:8])
				old.Confidence = 0
				c.store.UpdateNode(old)
				compacted++
			}
		}
	}
	return compacted, nil
}

func buildCompactSummary(typ string, contents []string) string {
	// Take first 5 items as representative
	limit := 5
	if len(contents) < limit {
		limit = len(contents)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Summary of %d %s memories:\n", len(contents), typ))
	for i, c := range contents[:limit] {
		if len(c) > 100 {
			c = c[:100] + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
	}
	if len(contents) > limit {
		sb.WriteString(fmt.Sprintf("... and %d more\n", len(contents)-limit))
	}
	return sb.String()
}
