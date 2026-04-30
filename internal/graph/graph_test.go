package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

func setupGraph(t *testing.T) (Graph, storage.Storage, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	g := New(store, store.DB())
	return g, store, func() { store.Close(); os.RemoveAll(dir) }
}

func TestAddNode(t *testing.T) {
	g, store, cleanup := setupGraph(t)
	defer cleanup()
	ctx := context.Background()

	node := &storage.Node{ID: "n1", Type: "decision", Content: "test", ContentHash: "h1", Scope: "project"}
	if err := g.AddNode(ctx, node); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	got, _ := store.GetNode(ctx, "n1")
	if got == nil {
		t.Error("node not stored")
	}
}

func TestAddEdgeCycleDetection(t *testing.T) {
	g, _, cleanup := setupGraph(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		_ = g.AddNode(ctx, &storage.Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}

	// a -> led_to -> b (OK)
	if err := g.AddEdge(ctx, &storage.Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0}); err != nil {
		t.Fatalf("AddEdge e1: %v", err)
	}

	// b -> led_to -> c (OK)
	if err := g.AddEdge(ctx, &storage.Edge{ID: "e2", FromID: "b", ToID: "c", Type: "led_to", Weight: 1.0}); err != nil {
		t.Fatalf("AddEdge e2: %v", err)
	}

	// c -> led_to -> a would create a cycle — should be rejected
	if err := g.AddEdge(ctx, &storage.Edge{ID: "e3", FromID: "c", ToID: "a", Type: "led_to", Weight: 1.0}); err == nil {
		t.Error("expected cycle detection to reject c -> led_to -> a")
	}

	// c -> relates_to -> a is allowed (cyclic edge type)
	if err := g.AddEdge(ctx, &storage.Edge{ID: "e4", FromID: "c", ToID: "a", Type: "relates_to", Weight: 1.0}); err != nil {
		t.Fatalf("relates_to cycle should be allowed: %v", err)
	}
}

func TestBFSRespectsDirectionality(t *testing.T) {
	g, _, cleanup := setupGraph(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		_ = g.AddNode(ctx, &storage.Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}

	// Directed: a --led_to--> b --relates_to--> c
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0})
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e2", FromID: "b", ToID: "c", Type: "relates_to", Weight: 1.0})

	// BFS from a should reach b (directed forward) and c (bidirectional relates_to)
	ids, err := g.BFS(ctx, "a", 2)
	if err != nil {
		t.Fatalf("BFS: %v", err)
	}
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["b"] {
		t.Error("BFS from a should reach b via led_to")
	}
	if !found["c"] {
		t.Error("BFS from a should reach c via relates_to")
	}

	// BFS from c should reach b (bidirectional relates_to) but NOT a (led_to is one-way)
	ids, _ = g.BFS(ctx, "c", 2)
	found = map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["b"] {
		t.Error("BFS from c should reach b via relates_to")
	}
	if found["a"] {
		t.Error("BFS from c should NOT reach a via led_to (directed edge, wrong direction)")
	}
}

func TestExtractSubgraph(t *testing.T) {
	g, _, cleanup := setupGraph(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c", "d"} {
		_ = g.AddNode(ctx, &storage.Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0})
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e2", FromID: "b", ToID: "c", Type: "led_to", Weight: 1.0})
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e3", FromID: "a", ToID: "d", Type: "relates_to", Weight: 1.0})

	sg, err := g.ExtractSubgraph(ctx, "a", 2)
	if err != nil {
		t.Fatalf("ExtractSubgraph: %v", err)
	}
	if len(sg.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(sg.Nodes))
	}
	if len(sg.Edges) != 3 {
		t.Errorf("expected 3 edges, got %d", len(sg.Edges))
	}
}

func TestAncestorsAndDescendants(t *testing.T) {
	g, _, cleanup := setupGraph(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		_ = g.AddNode(ctx, &storage.Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0})
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e2", FromID: "b", ToID: "c", Type: "led_to", Weight: 1.0})

	ancestors, err := g.Ancestors(ctx, "c")
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	ancSet := map[string]bool{}
	for _, id := range ancestors {
		ancSet[id] = true
	}
	if !ancSet["a"] || !ancSet["b"] {
		t.Errorf("ancestors of c should include a and b, got %v", ancestors)
	}

	descendants, err := g.Descendants(ctx, "a")
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	descSet := map[string]bool{}
	for _, id := range descendants {
		descSet[id] = true
	}
	if !descSet["b"] || !descSet["c"] {
		t.Errorf("descendants of a should include b and c, got %v", descendants)
	}
}

func TestIntentBFS(t *testing.T) {
	g, _, cleanup := setupGraph(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		_ = g.AddNode(ctx, &storage.Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e1", FromID: "a", ToID: "b", Type: "caused_by", Weight: 1.0})
	_ = g.AddEdge(ctx, &storage.Edge{ID: "e2", FromID: "b", ToID: "c", Type: "relates_to", Weight: 0.5})

	ids, err := g.IntentBFS(ctx, "a", 2, intent.IntentWhy)
	if err != nil {
		t.Fatalf("IntentBFS: %v", err)
	}
	if len(ids) == 0 {
		t.Error("IntentBFS returned no nodes")
	}
	foundA := false
	for _, id := range ids {
		if id == "a" {
			foundA = true
		}
	}
	if !foundA {
		t.Error("IntentBFS should include start node")
	}
}

func TestIsAcyclic(t *testing.T) {
	tests := []struct {
		edgeType string
		want     bool
	}{
		{"caused_by", true},
		{"led_to", true},
		{"supersedes", true},
		{"learned_in", true},
		{"part_of", true},
		{"relates_to", false},
		{"depends_on", false},
		{"touches", false},
		{"unknown", false},
	}
	for _, tc := range tests {
		got := IsAcyclic(tc.edgeType)
		if got != tc.want {
			t.Errorf("IsAcyclic(%q) = %v, want %v", tc.edgeType, got, tc.want)
		}
	}
}
