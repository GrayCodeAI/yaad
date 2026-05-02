package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/yaad/storage"
)

// TestQueryEmptyDatabase verifies Query on an empty store returns a no-results
// answer with zero confidence, not an error.
func TestQueryEmptyDatabase(t *testing.T) {
	eng := newTestEngine()

	result, err := eng.Query(context.Background(), "What conventions do we follow?", "empty-proj")
	if err != nil {
		t.Fatalf("Query on empty DB failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil QueryResult")
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0 for empty DB, got %f", result.Confidence)
	}
	if len(result.Sources) != 0 {
		t.Errorf("expected 0 sources for empty DB, got %d", len(result.Sources))
	}
	if !strings.Contains(result.Answer, "No relevant memories found") {
		t.Errorf("expected 'no relevant memories' message, got: %s", result.Answer)
	}
}

// TestQueryEmptyQuestion verifies that an empty question returns an error.
func TestQueryEmptyQuestion(t *testing.T) {
	eng := newTestEngine()

	_, err := eng.Query(context.Background(), "", "proj")
	if err == nil {
		t.Error("expected error for empty question")
	}

	_, err = eng.Query(context.Background(), "   ", "proj")
	if err == nil {
		t.Error("expected error for whitespace-only question")
	}
}

// TestQueryBasicRetrieval verifies that Query retrieves relevant memories
// and formats them into a coherent answer.
func TestQueryBasicRetrieval(t *testing.T) {
	eng := newTestEngine()

	_, err := eng.Remember(context.Background(), RememberInput{
		Type:    "convention",
		Content: "Always use context.Context as the first parameter in Go functions",
		Summary: "context.Context first param convention",
		Project: "myproj",
	})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	_, err = eng.Remember(context.Background(), RememberInput{
		Type:    "decision",
		Content: "We decided to propagate context through the entire call chain",
		Summary: "Propagate context through call chain",
		Project: "myproj",
	})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	// Unrelated memory in a different project
	_, err = eng.Remember(context.Background(), RememberInput{
		Type:    "convention",
		Content: "Use tabs for indentation in Python",
		Project: "other-proj",
	})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	result, err := eng.Query(context.Background(), "What do we know about context.Context?", "myproj")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil QueryResult")
	}
	if len(result.Sources) == 0 {
		t.Fatal("expected at least one source node")
	}
	if result.Answer == "" {
		t.Error("expected non-empty answer")
	}
	if result.Confidence <= 0 {
		t.Errorf("expected positive confidence, got %f", result.Confidence)
	}

	// Verify project scoping: no nodes from other-proj should appear
	for _, s := range result.Sources {
		if s.Project != "myproj" {
			t.Errorf("expected only myproj nodes in sources, got project=%s", s.Project)
		}
	}
}

// TestQueryConfidenceCalculation verifies that the confidence score is the
// average of the source nodes' confidence values.
func TestQueryConfidenceCalculation(t *testing.T) {
	eng := newTestEngine()

	n1, _ := eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "testing convention alpha", Project: "p1",
	})
	n2, _ := eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "testing convention beta", Project: "p1",
	})

	// Manually set different confidence values
	node1, _ := eng.store.GetNode(context.Background(), n1.ID)
	node1.Confidence = 0.8
	_ = eng.store.UpdateNode(context.Background(), node1)

	node2, _ := eng.store.GetNode(context.Background(), n2.ID)
	node2.Confidence = 0.6
	_ = eng.store.UpdateNode(context.Background(), node2)

	result, err := eng.Query(context.Background(), "What are the testing conventions?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Sources) == 0 {
		t.Fatal("expected sources")
	}

	// Calculate expected confidence
	totalConf := 0.0
	for _, s := range result.Sources {
		totalConf += s.Confidence
	}
	expectedConf := totalConf / float64(len(result.Sources))

	diff := result.Confidence - expectedConf
	if diff > 0.01 || diff < -0.01 {
		t.Errorf("expected confidence ~%f, got %f", expectedConf, result.Confidence)
	}
}

// TestQueryFiltersBelowThreshold verifies that nodes with confidence below
// the threshold (0.3) are excluded from query results.
func TestQueryFiltersBelowThreshold(t *testing.T) {
	eng := newTestEngine()

	n1, _ := eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "high confidence memory about deployment", Project: "p1",
	})
	n2, _ := eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "low confidence memory about deployment", Project: "p1",
	})

	// Set n2 to very low confidence (below 0.3 threshold)
	lowNode, _ := eng.store.GetNode(context.Background(), n2.ID)
	lowNode.Confidence = 0.1
	_ = eng.store.UpdateNode(context.Background(), lowNode)

	result, err := eng.Query(context.Background(), "What do we know about deployment?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// n1 should be in sources, n2 should not (confidence too low)
	foundHigh, foundLow := false, false
	for _, s := range result.Sources {
		if s.ID == n1.ID {
			foundHigh = true
		}
		if s.ID == n2.ID {
			foundLow = true
		}
	}
	if !foundHigh {
		t.Error("expected high-confidence node in results")
	}
	if foundLow {
		t.Error("expected low-confidence node (0.1) to be filtered out")
	}
}

// TestQueryWhatIntent verifies the "What" intent produces grouped output.
func TestQueryWhatIntent(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "Use gofmt for all Go code", Project: "p1",
	})
	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "decision", Content: "We decided to use gofmt as the standard formatter", Project: "p1",
	})

	result, err := eng.Query(context.Background(), "What is our gofmt policy?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if !strings.Contains(result.Answer, "what was found") {
		t.Errorf("expected 'what was found' in What-intent answer, got: %s", result.Answer)
	}
	if len(result.Sources) == 0 {
		t.Error("expected sources for What query")
	}
}

// TestQueryWhyIntent verifies the "Why" intent shows causal reasoning.
func TestQueryWhyIntent(t *testing.T) {
	eng := newTestEngine()

	n1, _ := eng.Remember(context.Background(), RememberInput{
		Type: "decision", Content: "We chose GraphQL over REST for the API", Project: "p1",
	})
	_, _ = eng.Remember(context.Background(), RememberInput{
		Type:    "convention",
		Content: "All GraphQL resolvers must validate input because we chose GraphQL",
		Project: "p1",
		Edges:   []EdgeInput{{ToID: n1.ID, Type: "caused_by"}},
	})

	result, err := eng.Query(context.Background(), "Why did we choose GraphQL?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if !strings.Contains(result.Answer, "reasons found") {
		t.Errorf("expected 'reasons found' in Why-intent answer, got: %s", result.Answer)
	}
	if len(result.Sources) == 0 {
		t.Error("expected sources for Why query")
	}
}

// TestQueryWhenIntent verifies the "When" intent shows a timeline.
func TestQueryWhenIntent(t *testing.T) {
	eng := newTestEngine()

	n1, _ := eng.Remember(context.Background(), RememberInput{
		Type: "decision", Content: "Initial decision to use microservices", Project: "p1",
	})
	// Set an explicit time to guarantee ordering
	node1, _ := eng.store.GetNode(context.Background(), n1.ID)
	node1.CreatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = eng.store.UpdateNode(context.Background(), node1)

	n2, _ := eng.Remember(context.Background(), RememberInput{
		Type: "decision", Content: "Later decision to use microservices with gRPC", Project: "p1",
	})
	node2, _ := eng.store.GetNode(context.Background(), n2.ID)
	node2.CreatedAt = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	_ = eng.store.UpdateNode(context.Background(), node2)

	result, err := eng.Query(context.Background(), "When did we decide on microservices?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if !strings.Contains(result.Answer, "Timeline") {
		t.Errorf("expected 'Timeline' in When-intent answer, got: %s", result.Answer)
	}
	if len(result.Sources) == 0 {
		t.Error("expected sources for When query")
	}
}

// TestQueryHowIntent verifies the "How" intent lists steps and procedures.
func TestQueryHowIntent(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "spec", Content: "Deploy by running make deploy to push to production", Project: "p1",
	})
	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "Always run tests before deploying with make test", Project: "p1",
	})

	result, err := eng.Query(context.Background(), "How do we deploy to production?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if !strings.Contains(result.Answer, "steps and procedures") {
		t.Errorf("expected 'steps and procedures' in How-intent answer, got: %s", result.Answer)
	}
	if len(result.Sources) == 0 {
		t.Error("expected sources for How query")
	}
}

// TestQueryProjectScoping verifies that Query only returns memories from
// the specified project.
func TestQueryProjectScoping(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "Project Alpha uses React", Project: "alpha",
	})
	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "Project Beta uses Vue", Project: "beta",
	})

	result, err := eng.Query(context.Background(), "What framework do we use?", "alpha")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	for _, s := range result.Sources {
		if s.Project != "alpha" {
			t.Errorf("expected only alpha project nodes, got project=%s", s.Project)
		}
	}
}

// TestQueryCancelledContext verifies that Query respects context cancellation.
func TestQueryCancelledContext(t *testing.T) {
	eng := newTestEngine()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := eng.Query(ctx, "anything", "proj")
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// TestQueryUsesNodeSummary verifies that the answer prefers Summary over Content
// when formatting nodes.
func TestQueryUsesNodeSummary(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(context.Background(), RememberInput{
		Type:    "convention",
		Content: "Very long detailed content about error handling that spans many paragraphs and has lots of detail",
		Summary: "Error handling convention",
		Project: "p1",
	})

	result, err := eng.Query(context.Background(), "What is our error handling convention?", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Sources) == 0 {
		t.Fatal("expected sources")
	}
	// The answer should use the summary, not the full content
	if !strings.Contains(result.Answer, "Error handling convention") {
		t.Errorf("expected answer to use node summary, got: %s", result.Answer)
	}
}

// TestQueryGeneralIntent verifies the general/default intent path works.
func TestQueryGeneralIntent(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(context.Background(), RememberInput{
		Type: "convention", Content: "logging is important for observability", Project: "p1",
	})

	// A query that doesn't match any specific intent keyword
	result, err := eng.Query(context.Background(), "logging", "p1")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Sources) == 0 {
		t.Error("expected sources for general query")
	}
	if result.Answer == "" {
		t.Error("expected non-empty answer for general query")
	}
}

// TestAverageConfidence verifies the helper function directly.
func TestAverageConfidence(t *testing.T) {
	nodes := []*storage.Node{
		{Confidence: 1.0},
		{Confidence: 0.8},
		{Confidence: 0.6},
	}
	got := averageConfidence(nodes)
	expected := 0.8
	if diff := got - expected; diff > 0.001 || diff < -0.001 {
		t.Errorf("expected average confidence %f, got %f", expected, got)
	}

	// Empty slice
	if avg := averageConfidence(nil); avg != 0 {
		t.Errorf("expected 0 for nil slice, got %f", avg)
	}
}

// TestNodeSummary verifies the nodeSummary helper.
func TestNodeSummary(t *testing.T) {
	// Node with summary
	n1 := &storage.Node{Summary: "short summary", Content: "long content here"}
	if s := nodeSummary(n1); s != "short summary" {
		t.Errorf("expected summary, got %q", s)
	}

	// Node without summary — should use content
	n2 := &storage.Node{Content: "content only"}
	if s := nodeSummary(n2); s != "content only" {
		t.Errorf("expected content, got %q", s)
	}

	// Node with very long content — should truncate
	long := strings.Repeat("x", 200)
	n3 := &storage.Node{Content: long}
	s := nodeSummary(n3)
	if len(s) > 121 {
		t.Errorf("expected truncated content (<=121 chars), got %d chars", len(s))
	}
	if !strings.HasSuffix(s, "...") {
		t.Error("expected truncated content to end with '...'")
	}
}
