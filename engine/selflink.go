package engine

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/storage"
)

// SelfLink finds existing nodes semantically related to the new node and creates
// typed edges. Inspired by A-MEM's Zettelkasten self-organizing approach.
// Runs as a best-effort step during Remember() — failures don't block storage.
func (e *Engine) SelfLink(ctx context.Context, node *storage.Node) {
	candidates, err := e.store.SearchNodes(ctx, node.Content, 6)
	if err != nil || len(candidates) == 0 {
		return
	}

	edges, _ := e.store.GetEdgesFrom(ctx, node.ID)

	for _, candidate := range candidates {
		if candidate.ID == node.ID {
			continue
		}
		if hasEdgeTo(edges, candidate.ID) {
			continue
		}

		edgeType := classifyRelationship(node, candidate)
		if edgeType == "" {
			continue
		}

		_ = e.graph.AddEdge(ctx, &storage.Edge{
			ID:     uuid.New().String(),
			FromID: node.ID,
			ToID:   candidate.ID,
			Type:   edgeType,
			Weight: 0.6,
		})
	}
}

// classifyRelationship determines the edge type between two nodes
// based on their types and content relationship.
func classifyRelationship(newNode, existing *storage.Node) string {
	// Same key = supersedes (handled elsewhere by keyed upsert)
	if newNode.Key != "" && newNode.Key == existing.Key {
		return ""
	}

	newType := newNode.Type
	existType := existing.Type

	// Type-based heuristic classification (no LLM needed)
	switch {
	// decision → led_to → convention
	case existType == "decision" && newType == "convention":
		return "led_to"
	// bug → caused_by → decision
	case newType == "bug" && existType == "decision":
		return "caused_by"
	// convention → part_of → spec
	case newType == "convention" && existType == "spec":
		return "part_of"
	// task → depends_on → task
	case newType == "task" && existType == "task":
		if hasContentOverlap(newNode.Content, existing.Content) {
			return "depends_on"
		}
	// same type, high overlap = relates_to
	case newType == existType:
		if hasContentOverlap(newNode.Content, existing.Content) {
			return "relates_to"
		}
	// cross-type with shared entities = touches
	default:
		if hasContentOverlap(newNode.Content, existing.Content) {
			return "relates_to"
		}
	}
	return ""
}

// hasContentOverlap checks if two content strings share significant key terms.
func hasContentOverlap(a, b string) bool {
	aTerms := extractTerms(a)
	bTerms := extractTerms(b)
	if len(aTerms) == 0 || len(bTerms) == 0 {
		return false
	}
	shared := 0
	for term := range aTerms {
		if bTerms[term] {
			shared++
		}
	}
	minSet := len(aTerms)
	if len(bTerms) < minSet {
		minSet = len(bTerms)
	}
	return shared >= 2 && float64(shared)/float64(minSet) >= 0.25
}

func extractTerms(content string) map[string]bool {
	terms := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(content)) {
		w = strings.Trim(w, ".,;:!?\"'()[]{}/-")
		if len(w) > 3 && !selfLinkStopWords[w] {
			terms[w] = true
		}
	}
	return terms
}

func hasEdgeTo(edges []*storage.Edge, targetID string) bool {
	for _, e := range edges {
		if e.ToID == targetID {
			return true
		}
	}
	return false
}

var selfLinkStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"was": true, "one": true, "our": true, "use": true, "with": true,
	"this": true, "that": true, "from": true, "they": true, "will": true,
	"have": true, "been": true, "should": true, "would": true, "could": true,
	"also": true, "than": true, "then": true, "into": true, "over": true,
	"using": true, "when": true, "which": true, "there": true, "their": true,
}
