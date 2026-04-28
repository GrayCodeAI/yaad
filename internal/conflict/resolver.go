// Package conflict detects and resolves contradictory memories.
// When a new memory contradicts an existing one (same entity/topic, different fact),
// the old memory is auto-superseded. Based on Mem0's conflict resolution approach.
package conflict

import (
	"strings"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Resolver detects and resolves memory conflicts.
type Resolver struct {
	store *storage.Store
}

func New(store *storage.Store) *Resolver {
	return &Resolver{store: store}
}

// CheckAndResolve checks if a new node contradicts existing nodes.
// If a contradiction is found, creates a supersedes edge and lowers the old node's confidence.
// Returns the list of superseded node IDs.
func (r *Resolver) CheckAndResolve(newNode *storage.Node) ([]string, error) {
	// Find existing nodes of the same type in the same project
	existing, err := r.store.ListNodes(storage.NodeFilter{
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
			r.store.CreateEdge(&storage.Edge{
				ID:      uuid.New().String(),
				FromID:  newNode.ID,
				ToID:    old.ID,
				Type:    "supersedes",
				Acyclic: true,
				Weight:  1.0,
			})
			// Lower old node confidence
			old.Confidence *= 0.3
			r.store.UpdateNode(old)
			// Save version for audit trail
			r.store.SaveVersion(old.ID, old.Content, "conflict-resolver",
				"superseded by "+newNode.ID[:8])
			superseded = append(superseded, old.ID)
		}
	}
	return superseded, nil
}

// isContradiction detects if two nodes about the same topic say different things.
func isContradiction(newNode, oldNode *storage.Node) bool {
	// Extract key entities from both
	newEntities := extractKeyTerms(newNode.Content)
	oldEntities := extractKeyTerms(oldNode.Content)

	// Must share at least 2 key terms (same topic)
	shared := 0
	for term := range newEntities {
		if oldEntities[term] {
			shared++
		}
	}
	if shared < 2 {
		return false // different topics, not a contradiction
	}

	// Check for negation patterns
	newLower := strings.ToLower(newNode.Content)

	// "Use X" vs "Don't use X" or "Use Y instead of X"
	if strings.Contains(newLower, "instead of") || strings.Contains(newLower, "not ") ||
		strings.Contains(newLower, "replaced") || strings.Contains(newLower, "switched") ||
		strings.Contains(newLower, "migrated") || strings.Contains(newLower, "changed") {
		return true
	}

	// "Chose X" vs "Chose Y" for same topic (shared entities but different choice)
	if newNode.Type == "decision" && oldNode.Type == "decision" && shared >= 2 {
		// Different content but same topic = likely updated decision
		if newNode.Content != oldNode.Content {
			return true
		}
	}

	// Same convention type, same entities, different content = updated convention
	if newNode.Type == "convention" && oldNode.Type == "convention" && shared >= 2 {
		if newNode.Content != oldNode.Content {
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
