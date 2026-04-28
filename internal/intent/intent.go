// Package intent classifies query intent to route graph traversal.
// Based on MAGMA (arxiv:2601.03236): intent-aware retrieval boosts
// the right edge types, improving LoCoMo score from 0.58 to 0.70.
package intent

import "strings"

// Intent represents the detected query intent.
type Intent int

const (
	IntentGeneral Intent = iota // default: balanced traversal
	IntentWhy                   // causal: "why", "reason", "cause", "because"
	IntentWhen                  // temporal: "when", "since", "before", "after", "timeline"
	IntentWho                   // entity: "who", "which library", "what tool"
	IntentHow                   // procedural: "how", "steps", "process", "workflow"
	IntentWhat                  // spec/fact: "what is", "describe", "explain"
)

// EdgeWeights maps edge types to boost multipliers for a given intent.
// Higher = more likely to traverse this edge type.
// Based on MAGMA's adaptive weight vector w_Tq.
type EdgeWeights struct {
	CausedBy  float64
	LedTo     float64
	Supersedes float64
	LearnedIn float64
	PartOf    float64
	RelatesTo float64
	DependsOn float64
	Touches   float64
}

// Weights returns edge traversal weights for the given intent.
func Weights(i Intent) EdgeWeights {
	switch i {
	case IntentWhy:
		// Causal queries: boost causal edges strongly
		return EdgeWeights{CausedBy: 5.0, LedTo: 4.0, Supersedes: 2.0, LearnedIn: 1.0, PartOf: 1.5, RelatesTo: 1.0, DependsOn: 1.0, Touches: 0.5}
	case IntentWhen:
		// Temporal queries: boost temporal backbone
		return EdgeWeights{CausedBy: 1.0, LedTo: 1.0, Supersedes: 1.0, LearnedIn: 4.0, PartOf: 1.0, RelatesTo: 1.0, DependsOn: 1.0, Touches: 0.5}
	case IntentWho:
		// Entity queries: boost entity/touches edges
		return EdgeWeights{CausedBy: 1.0, LedTo: 1.0, Supersedes: 1.0, LearnedIn: 1.0, PartOf: 2.0, RelatesTo: 2.0, DependsOn: 1.0, Touches: 5.0}
	case IntentHow:
		// Procedural queries: boost spec/skill/part_of
		return EdgeWeights{CausedBy: 1.0, LedTo: 2.0, Supersedes: 1.0, LearnedIn: 1.0, PartOf: 4.0, RelatesTo: 2.0, DependsOn: 3.0, Touches: 1.0}
	case IntentWhat:
		// Fact/spec queries: boost relates_to and part_of
		return EdgeWeights{CausedBy: 1.0, LedTo: 1.5, Supersedes: 1.0, LearnedIn: 1.0, PartOf: 3.0, RelatesTo: 3.0, DependsOn: 1.5, Touches: 2.0}
	default:
		// Balanced: all edges equal
		return EdgeWeights{CausedBy: 1.0, LedTo: 1.0, Supersedes: 1.0, LearnedIn: 1.0, PartOf: 1.0, RelatesTo: 1.0, DependsOn: 1.0, Touches: 1.0}
	}
}

// EdgeWeight returns the boost for a specific edge type given intent weights.
func (w EdgeWeights) EdgeWeight(edgeType string) float64 {
	switch edgeType {
	case "caused_by":
		return w.CausedBy
	case "led_to":
		return w.LedTo
	case "supersedes":
		return w.Supersedes
	case "learned_in":
		return w.LearnedIn
	case "part_of":
		return w.PartOf
	case "relates_to":
		return w.RelatesTo
	case "depends_on":
		return w.DependsOn
	case "touches":
		return w.Touches
	default:
		return 1.0
	}
}

// Classify detects query intent from natural language.
// No LLM needed — keyword matching is sufficient for coding queries.
func Classify(query string) Intent {
	q := strings.ToLower(query)

	// Why: causal reasoning
	if containsAny(q, "why", "reason", "cause", "because", "led to", "resulted in", "decided", "chose", "choice") {
		return IntentWhy
	}

	// When: temporal reasoning
	if containsAny(q, "when", "since", "before", "after", "timeline", "history", "last session", "previously", "ago", "date") {
		return IntentWhen
	}

	// How: procedural
	if containsAny(q, "how to", "how do", "steps", "process", "workflow", "procedure", "deploy", "setup", "install") {
		return IntentHow
	}

	// Who/Which: entity
	if containsAny(q, "who", "which library", "which tool", "which package", "what library", "what tool") {
		return IntentWho
	}

	// What: fact/spec
	if containsAny(q, "what is", "what are", "describe", "explain", "tell me about", "show me") {
		return IntentWhat
	}

	return IntentGeneral
}

func containsAny(s string, keywords ...string) bool {
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// String returns a human-readable intent name.
func (i Intent) String() string {
	switch i {
	case IntentWhy:
		return "Why"
	case IntentWhen:
		return "When"
	case IntentWho:
		return "Who"
	case IntentHow:
		return "How"
	case IntentWhat:
		return "What"
	default:
		return "General"
	}
}
