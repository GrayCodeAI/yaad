package search

import (
	"testing"
)

func TestClassifyIntent(t *testing.T) {
	tests := []struct {
		query string
		want  QueryIntent
	}{
		// Why / causal.
		{"Why did we switch to Postgres?", IntentWhy},
		{"What was the reason for the refactor?", IntentWhy},
		{"Who decided to use gRPC and why?", IntentWhy},
		{"Because the tests kept failing", IntentWhy},

		// When / temporal.
		{"When did we add caching?", IntentWhen},
		{"What happened before the migration?", IntentWhen},
		{"Show me the timeline of changes", IntentWhen},
		{"What did we do in the last session?", IntentWhen},
		{"Changes since Monday", IntentWhen},

		// How / procedural.
		{"How to deploy to staging?", IntentHow},
		{"How do I run the integration tests?", IntentHow},
		{"What are the steps to release?", IntentHow},
		{"Describe the deploy process", IntentHow},

		// Who / entity.
		{"Who wrote the auth module?", IntentWho},
		{"Which library handles JSON parsing?", IntentWho},
		{"Which tool do we use for linting?", IntentWho},

		// Find / locate.
		{"Find the rate limiter implementation", IntentFind},
		{"Where is the database config?", IntentFind},
		{"Locate the error handling middleware", IntentFind},

		// What / definitional.
		{"What is the storage interface?", IntentWhat},
		{"Describe the edge schema", IntentWhat},
		{"Explain the graph model", IntentWhat},

		// General / fallback.
		{"hello", IntentGeneral},
		{"fix the bug in main.go", IntentGeneral},
		{"refactor the handler", IntentGeneral},
		{"", IntentGeneral},
	}

	for _, tt := range tests {
		got := ClassifyIntent(tt.query)
		if got != tt.want {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestIntentString(t *testing.T) {
	tests := []struct {
		intent QueryIntent
		want   string
	}{
		{IntentFind, "find"},
		{IntentWhy, "why"},
		{IntentWhen, "when"},
		{IntentWhat, "what"},
		{IntentHow, "how"},
		{IntentWho, "who"},
		{IntentGeneral, "general"},
	}
	for _, tt := range tests {
		if got := tt.intent.String(); got != tt.want {
			t.Errorf("QueryIntent(%d).String() = %q, want %q", tt.intent, got, tt.want)
		}
	}
}

func TestIntentWeightsKeys(t *testing.T) {
	expectedEdges := []string{
		"caused_by", "led_to", "supersedes", "learned_in",
		"part_of", "relates_to", "depends_on", "touches",
	}

	intents := []QueryIntent{
		IntentFind, IntentWhy, IntentWhen, IntentWhat,
		IntentHow, IntentWho, IntentGeneral,
	}

	for _, intent := range intents {
		weights := IntentWeights(intent)
		for _, edge := range expectedEdges {
			if _, ok := weights[edge]; !ok {
				t.Errorf("IntentWeights(%v) missing edge type %q", intent, edge)
			}
		}
	}
}

func TestIntentWeightsBoost(t *testing.T) {
	// Why queries should strongly boost causal edges.
	whyWeights := IntentWeights(IntentWhy)
	if whyWeights["caused_by"] <= whyWeights["touches"] {
		t.Error("Why intent should boost caused_by over touches")
	}

	// When queries should strongly boost temporal edges.
	whenWeights := IntentWeights(IntentWhen)
	if whenWeights["learned_in"] <= whenWeights["caused_by"] {
		t.Error("When intent should boost learned_in over caused_by")
	}

	// Find queries should boost touches for entity discovery.
	findWeights := IntentWeights(IntentFind)
	if findWeights["touches"] <= findWeights["caused_by"] {
		t.Error("Find intent should boost touches over caused_by")
	}

	// General should be balanced.
	genWeights := IntentWeights(IntentGeneral)
	for _, v := range genWeights {
		if v != 1.0 {
			t.Errorf("General intent should have all weights = 1.0, got %f", v)
		}
	}
}
