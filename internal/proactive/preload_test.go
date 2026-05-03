package proactive

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mockSearcher is a test double that returns predetermined results
// based on whether the query contains certain substrings.
type mockSearcher struct {
	// index maps a keyword to the node IDs it should return.
	index map[string][]string
}

func (m *mockSearcher) Search(_ context.Context, query string, _ string, limit int) ([]string, error) {
	q := strings.ToLower(query)
	var results []string
	for keyword, ids := range m.index {
		if strings.Contains(q, keyword) {
			results = append(results, ids...)
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func newMockSearcher() *mockSearcher {
	return &mockSearcher{
		index: map[string][]string{
			"intent":    {"node-1", "node-2"},
			"search":    {"node-2", "node-3"},
			"intent.go": {"node-1"},
			"auth":      {"node-4", "node-5"},
			"recent":    {"node-6"},
			"session":   {"node-7"},
			"project":   {"node-6", "node-7"},
		},
	}
}

func TestPreloadFileOpen(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerFileOpen,
		Value:     "internal/search/intent.go",
		Project:   "project-x",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) == 0 {
		t.Fatal("expected preloaded nodes for file open trigger")
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
	if !strings.Contains(result.Reason, "file_open") {
		t.Errorf("reason should mention trigger kind, got: %s", result.Reason)
	}
}

func TestPreloadGitCheckout(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerGitCheckout,
		Value:     "feature/auth-refactor",
		Project:   "project-x",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) == 0 {
		t.Fatal("expected preloaded nodes for git checkout trigger")
	}
	// "auth" should match in the branch name decomposition.
	found := false
	for _, id := range result.NodeIDs {
		if id == "node-4" || id == "node-5" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth-related nodes from branch name")
	}
}

func TestPreloadSessionStart(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerSessionStart,
		Value:     "",
		Project:   "project",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) == 0 {
		t.Fatal("expected preloaded nodes for session start trigger")
	}
}

func TestPreloadEmptyTrigger(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerFileOpen,
		Value:     "",
		Project:   "p",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) != 0 {
		t.Errorf("expected no nodes for empty trigger value, got %d", len(result.NodeIDs))
	}
}

func TestPreloadLimit(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerSessionStart,
		Value:     "",
		Project:   "project",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) > 1 {
		t.Errorf("expected at most 1 node, got %d", len(result.NodeIDs))
	}
}

func TestPreloadDirChange(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerDirChange,
		Value:     "internal/search",
		Project:   "p",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) == 0 {
		t.Fatal("expected preloaded nodes for dir change trigger")
	}
}

func TestPreloadQueryPrefix(t *testing.T) {
	searcher := newMockSearcher()
	trigger := PreloadTrigger{
		Kind:      TriggerQuery,
		Value:     "intent classification",
		Project:   "p",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NodeIDs) == 0 {
		t.Fatal("expected preloaded nodes for query prefix trigger")
	}
}

func TestPreloadDeduplication(t *testing.T) {
	searcher := newMockSearcher()
	// "internal/search/intent.go" will fire queries for the full path,
	// the filename "intent.go", and the directory "internal/search".
	// "node-2" appears via both "intent" and "search" keywords, so
	// it should appear once in results and rank higher.
	trigger := PreloadTrigger{
		Kind:      TriggerFileOpen,
		Value:     "internal/search/intent.go",
		Project:   "p",
		Timestamp: time.Now(),
	}

	result, err := Preload(context.Background(), trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check no duplicates.
	seen := make(map[string]bool)
	for _, id := range result.NodeIDs {
		if seen[id] {
			t.Errorf("duplicate node ID in results: %s", id)
		}
		seen[id] = true
	}
}

func TestPreloadCancellation(t *testing.T) {
	searcher := newMockSearcher()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	trigger := PreloadTrigger{
		Kind:    TriggerSessionStart,
		Value:   "",
		Project: "project",
	}

	// Should still return without error (best-effort).
	result, err := Preload(ctx, trigger, searcher, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Results may be empty since context is cancelled, but no panic.
	_ = result
}

func TestTriggerKindString(t *testing.T) {
	tests := []struct {
		kind TriggerKind
		want string
	}{
		{TriggerFileOpen, "file_open"},
		{TriggerGitCheckout, "git_checkout"},
		{TriggerDirChange, "dir_change"},
		{TriggerQuery, "query"},
		{TriggerSessionStart, "session_start"},
		{TriggerKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("TriggerKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
