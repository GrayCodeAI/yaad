// Package search provides intent-aware retrieval routing for yaad's graph.
// It classifies natural-language queries into intent categories, then
// returns edge-type weight maps that bias graph traversal toward the
// most relevant relationship types. Based on MAGMA (arxiv:2601.03236).
package search

import "strings"

// QueryIntent represents the classified intent of a user query.
type QueryIntent int

const (
	IntentFind    QueryIntent = iota // locate a node: "find", "where is", "locate"
	IntentWhy                        // causal: "why", "reason", "cause"
	IntentWhen                       // temporal: "when", "before", "after"
	IntentWhat                       // definitional: "what is", "describe"
	IntentHow                        // procedural: "how to", "steps"
	IntentWho                        // entity/attribution: "who", "which"
	IntentGeneral                    // fallback: balanced weights
)

// String returns a human-readable label for the intent.
func (qi QueryIntent) String() string {
	switch qi {
	case IntentFind:
		return "find"
	case IntentWhy:
		return "why"
	case IntentWhen:
		return "when"
	case IntentWhat:
		return "what"
	case IntentHow:
		return "how"
	case IntentWho:
		return "who"
	default:
		return "general"
	}
}

// keywords maps each intent to its trigger phrases.
// Order matters: first match wins, so more specific phrases come first.
var keywords = map[QueryIntent][]string{
	IntentWhy: {
		"why", "reason", "cause", "because", "rationale",
		"led to", "resulted in", "decided", "chose", "motivation",
	},
	IntentWhen: {
		"when", "since", "before", "after", "timeline",
		"history", "last session", "previously", "ago", "date",
		"last time", "recently", "earlier",
	},
	IntentHow: {
		"how to", "how do", "how does", "how can",
		"steps", "process", "workflow", "procedure",
		"deploy", "setup", "install", "configure",
	},
	IntentWho: {
		"who", "which library", "which tool", "which package",
		"what library", "what tool", "authored by", "maintained by",
	},
	IntentFind: {
		"find", "where is", "locate", "search for", "look up",
		"show me", "get me", "path to",
	},
	IntentWhat: {
		"what is", "what are", "what does",
		"describe", "explain", "tell me about", "define",
	},
}

// intentOrder controls precedence when multiple intents could match.
var intentOrder = []QueryIntent{
	IntentWhy,
	IntentWhen,
	IntentHow,
	IntentWho,
	IntentFind,
	IntentWhat,
}

// ClassifyIntent detects query intent from natural language using keyword
// heuristics. No LLM call required -- keyword matching is sufficient for
// the coding-assistant domain where queries are direct and unambiguous.
func ClassifyIntent(query string) QueryIntent {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return IntentGeneral
	}
	for _, intent := range intentOrder {
		for _, kw := range keywords[intent] {
			if strings.Contains(q, kw) {
				return intent
			}
		}
	}
	return IntentGeneral
}

// IntentWeights returns a map of edge-type to weight multiplier for the
// given intent. Higher weights bias graph traversal toward those edge types.
func IntentWeights(intent QueryIntent) map[string]float64 {
	switch intent {
	case IntentWhy:
		return map[string]float64{
			"caused_by":  5.0,
			"led_to":     4.0,
			"supersedes": 2.0,
			"learned_in": 1.0,
			"part_of":    1.5,
			"relates_to": 1.0,
			"depends_on": 1.0,
			"touches":    0.5,
		}
	case IntentWhen:
		return map[string]float64{
			"caused_by":  1.0,
			"led_to":     1.0,
			"supersedes": 1.5,
			"learned_in": 5.0,
			"part_of":    1.0,
			"relates_to": 1.0,
			"depends_on": 1.0,
			"touches":    0.5,
		}
	case IntentHow:
		return map[string]float64{
			"caused_by":  1.0,
			"led_to":     2.0,
			"supersedes": 1.0,
			"learned_in": 1.0,
			"part_of":    4.0,
			"relates_to": 2.0,
			"depends_on": 3.0,
			"touches":    1.0,
		}
	case IntentWho:
		return map[string]float64{
			"caused_by":  1.0,
			"led_to":     1.0,
			"supersedes": 1.0,
			"learned_in": 1.0,
			"part_of":    2.0,
			"relates_to": 2.0,
			"depends_on": 1.0,
			"touches":    5.0,
		}
	case IntentFind:
		return map[string]float64{
			"caused_by":  1.0,
			"led_to":     1.0,
			"supersedes": 1.0,
			"learned_in": 1.0,
			"part_of":    3.0,
			"relates_to": 3.0,
			"depends_on": 2.0,
			"touches":    4.0,
		}
	case IntentWhat:
		return map[string]float64{
			"caused_by":  1.0,
			"led_to":     1.5,
			"supersedes": 1.0,
			"learned_in": 1.0,
			"part_of":    3.0,
			"relates_to": 3.0,
			"depends_on": 1.5,
			"touches":    2.0,
		}
	default:
		// IntentGeneral: balanced traversal -- all edges weighted equally.
		return map[string]float64{
			"caused_by":  1.0,
			"led_to":     1.0,
			"supersedes": 1.0,
			"learned_in": 1.0,
			"part_of":    1.0,
			"relates_to": 1.0,
			"depends_on": 1.0,
			"touches":    1.0,
		}
	}
}
