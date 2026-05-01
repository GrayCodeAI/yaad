// Package conflict detects and resolves contradictory memories.
// When a new memory contradicts an existing one (same entity/topic, different fact),
// the old memory is auto-superseded. Based on Mem0's conflict resolution approach.
package conflict

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/storage"
)

// Resolver detects and resolves memory conflicts.
type Resolver struct {
	store storage.Storage
}

func New(store storage.Storage) *Resolver {
	return &Resolver{store: store}
}

// CheckAndResolve checks if a new node contradicts existing nodes.
// If a contradiction is found, creates a supersedes edge and lowers the old node's confidence.
// Returns the list of superseded node IDs.
func (r *Resolver) CheckAndResolve(ctx context.Context, newNode *storage.Node) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Find existing nodes of the same type in the same project
	existing, err := r.store.ListNodes(ctx, storage.NodeFilter{
		Type:    newNode.Type,
		Project: newNode.Project,
		Scope:   newNode.Scope,
	})
	if err != nil {
		return nil, err
	}

	var superseded []string
	for _, old := range existing {
		if old.ID == newNode.ID || old.Confidence <= 0 {
			continue
		}
		if isContradiction(newNode, old) {
			// Create supersedes edge
			r.store.CreateEdge(ctx, &storage.Edge{
				ID:      uuid.New().String(),
				FromID:  newNode.ID,
				ToID:    old.ID,
				Type:    "supersedes",
				Acyclic: true,
				Weight:  1.0,
			})
			// Lower old node confidence
			old.Confidence *= 0.3
			r.store.UpdateNode(ctx, old)
			// Save version for audit trail
			r.store.SaveVersion(ctx, old.ID, old.Content, "conflict-resolver",
				"superseded by "+newNode.ID[:8])
			superseded = append(superseded, old.ID)
		}
	}
	return superseded, nil
}

// isContradiction detects if two nodes about the same topic say different things.
// Conservative: requires high term overlap AND explicit contradiction signals.
func isContradiction(newNode, oldNode *storage.Node) bool {
	newEntities := extractKeyTerms(newNode.Content)
	oldEntities := extractKeyTerms(oldNode.Content)

	// Must share significant key terms (prevents false positives on loosely related content)
	shared := 0
	for term := range newEntities {
		if oldEntities[term] {
			shared++
		}
	}

	// Require at least 3 shared terms, or >40% overlap with the smaller set
	minSet := len(newEntities)
	if len(oldEntities) < minSet {
		minSet = len(oldEntities)
	}
	if minSet == 0 {
		return false
	}
	overlapRatio := float64(shared) / float64(minSet)
	if shared < 3 && overlapRatio < 0.4 {
		return false
	}

	// Check for explicit negation/replacement patterns
	newLower := strings.ToLower(newNode.Content)
	hasContradictionSignal := strings.Contains(newLower, "instead of") ||
		strings.Contains(newLower, "replaced") ||
		strings.Contains(newLower, "switched from") ||
		strings.Contains(newLower, "migrated from") ||
		strings.Contains(newLower, "no longer")

	if hasContradictionSignal {
		return true
	}

	// Same decision/convention topic (high overlap) with different content = updated
	if overlapRatio >= 0.5 && newNode.Content != oldNode.Content {
		if newNode.Type == "decision" || newNode.Type == "convention" {
			return true
		}
	}

	return false
}

func extractKeyTerms(content string) map[string]bool {
	terms := map[string]bool{}
	words := strings.Fields(strings.ToLower(content))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}") 
		if len(w) > 3 && !isStopWord(w) {
			terms[w] = true
		}
	}
	return terms
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"was": true, "one": true, "our": true, "use": true, "with": true,
	"this": true, "that": true, "from": true, "they": true, "will": true,
	"have": true, "been": true, "should": true, "would": true, "could": true,
	"also": true, "than": true, "then": true, "into": true, "over": true,
}

func isStopWord(w string) bool { return stopWords[w] }
