package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func setupStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store, func() { store.Close(); os.RemoveAll(dir) }
}

func TestCreateAndGetNode(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	node := &Node{
		ID:          uuid.New().String(),
		Type:        "convention",
		Content:     "Use jose not jsonwebtoken",
		ContentHash: "abc123",
		Scope:       "project",
		Project:     "testproj",
		Confidence:  1.0,
	}
	if err := s.CreateNode(ctx, node); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	got, err := s.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Content != node.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, node.Content)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := s.GetNode(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
	if !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestDuplicateNode(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	node := &Node{
		ID:          uuid.New().String(),
		Type:        "convention",
		Content:     "dup test",
		ContentHash: "hash1",
		Scope:       "project",
		Project:     "testproj",
	}
	if err := s.CreateNode(ctx, node); err != nil {
		t.Fatal(err)
	}

	// Same hash, scope, project should fail
	node2 := &Node{
		ID:          uuid.New().String(),
		Type:        "decision",
		Content:     "different content",
		ContentHash: "hash1",
		Scope:       "project",
		Project:     "testproj",
	}
	err := s.CreateNode(ctx, node2)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !errors.Is(err, ErrDuplicateNode) {
		t.Errorf("expected ErrDuplicateNode, got: %v", err)
	}
}

func TestCreateAndGetEdge(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	// Need nodes first
	for _, id := range []string{"a", "b"} {
		_ = s.CreateNode(ctx, &Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}

	e := &Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0}
	if err := s.CreateEdge(ctx, e); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	got, err := s.GetEdge(ctx, "e1")
	if err != nil {
		t.Fatalf("GetEdge: %v", err)
	}
	if got.Type != "led_to" {
		t.Errorf("type mismatch: got %q, want led_to", got.Type)
	}
}

func TestGetEdgeNotFound(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := s.GetEdge(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrEdgeNotFound) {
		t.Errorf("expected ErrEdgeNotFound, got: %v", err)
	}
}

func TestDuplicateEdge(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b"} {
		_ = s.CreateNode(ctx, &Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}

	_ = s.CreateEdge(ctx, &Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0})
	err := s.CreateEdge(ctx, &Edge{ID: "e2", FromID: "a", ToID: "b", Type: "led_to", Weight: 0.5})
	if err == nil {
		t.Fatal("expected duplicate edge error")
	}
	if !errors.Is(err, ErrDuplicateEdge) {
		t.Errorf("expected ErrDuplicateEdge, got: %v", err)
	}
}

func TestGetNodesBatchChunks(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create more nodes than maxSQLVariables to test chunking
	var ids []string
	for i := 0; i < 100; i++ {
		id := uuid.New().String()
		ids = append(ids, id)
		_ = s.CreateNode(ctx, &Node{ID: id, Type: "convention", Content: "batch", ContentHash: "h" + id, Scope: "project"})
	}

	nodes, err := s.GetNodesBatch(ctx, ids)
	if err != nil {
		t.Fatalf("GetNodesBatch: %v", err)
	}
	if len(nodes) != 100 {
		t.Errorf("expected 100 nodes, got %d", len(nodes))
	}
}

func TestAccessLog(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	node := &Node{ID: "n1", Type: "convention", Content: "test", ContentHash: "h1", Scope: "project"}
	_ = s.CreateNode(ctx, node)

	// Log multiple accesses
	for i := 0; i < 5; i++ {
		if err := s.LogAccess(ctx, "n1"); err != nil {
			t.Fatalf("LogAccess: %v", err)
		}
	}

	// Flush should update access_count
	n, err := s.FlushAccessLog(ctx)
	if err != nil {
		t.Fatalf("FlushAccessLog: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 node updated, got %d", n)
	}

	got, _ := s.GetNode(ctx, "n1")
	if got.AccessCount != 5 {
		t.Errorf("expected access_count=5, got %d", got.AccessCount)
	}
	if got.AccessedAt.IsZero() {
		t.Error("expected accessed_at to be set")
	}
}

func TestGetEdgesBetween(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c", "d"} {
		_ = s.CreateNode(ctx, &Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}
	_ = s.CreateEdge(ctx, &Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0})
	_ = s.CreateEdge(ctx, &Edge{ID: "e2", FromID: "b", ToID: "c", Type: "led_to", Weight: 1.0})
	_ = s.CreateEdge(ctx, &Edge{ID: "e3", FromID: "c", ToID: "d", Type: "relates_to", Weight: 1.0})

	// Edges between a,b,c (should get e1 and e2)
	edges, err := s.GetEdgesBetween(ctx, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("GetEdgesBetween: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestCheckCycle(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		_ = s.CreateNode(ctx, &Node{ID: id, Type: "decision", Content: id, ContentHash: "h" + id, Scope: "project"})
	}
	_ = s.CreateEdge(ctx, &Edge{ID: "e1", FromID: "a", ToID: "b", Type: "led_to", Weight: 1.0, Acyclic: true})
	_ = s.CreateEdge(ctx, &Edge{ID: "e2", FromID: "b", ToID: "c", Type: "led_to", Weight: 1.0, Acyclic: true})

	// a -> b -> c, so c -> a would create cycle
	hasCycle, err := s.CheckCycle(ctx, "c", "a")
	if err != nil {
		t.Fatalf("CheckCycle: %v", err)
	}
	if !hasCycle {
		t.Error("expected cycle detected for c -> a")
	}

	// a -> b is not a cycle
	hasCycle, _ = s.CheckCycle(ctx, "a", "b")
	if hasCycle {
		t.Error("expected no cycle for a -> b")
	}
}

func TestSessions(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	sess := &Session{
		ID:        "s1",
		Project:   "p1",
		Agent:     "test-agent",
		StartedAt: time.Now(),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.EndSession(ctx, "s1", "completed"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	sessions, err := s.ListSessions(ctx, "p1", 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Summary != "completed" {
		t.Errorf("summary mismatch: got %q", sessions[0].Summary)
	}
	if sessions[0].EndedAt.IsZero() {
		t.Error("expected ended_at to be set")
	}
}

func TestVersions(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	node := &Node{ID: "n1", Type: "convention", Content: "v1", ContentHash: "h1", Scope: "project"}
	_ = s.CreateNode(ctx, node)

	_ = s.SaveVersion(ctx, "n1", "v1", "user", "saved")
	_ = s.SaveVersion(ctx, "n1", "v2", "user", "edited")

	versions, err := s.GetVersions(ctx, "n1")
	if err != nil {
		t.Fatalf("GetVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}

func TestWithTx(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	err := s.WithTx(ctx, func(tx Storage) error {
		return tx.CreateNode(ctx, &Node{ID: "tx1", Type: "convention", Content: "tx", ContentHash: "htx", Scope: "project"})
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	node, err := s.GetNode(ctx, "tx1")
	if err != nil {
		t.Fatalf("GetNode after tx: %v", err)
	}
	if node.ID != "tx1" {
		t.Errorf("expected tx1, got %s", node.ID)
	}
}

func TestSearchNodesWithSpecialChars(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	// Node with content that contains FTS5 special chars
	_ = s.CreateNode(ctx, &Node{
		ID: uuid.New().String(), Type: "convention",
		Content: "Use auth* with JWT tokens", ContentHash: "h1", Scope: "project",
	})
	_ = s.CreateNode(ctx, &Node{
		ID: uuid.New().String(), Type: "convention",
		Content: "Use OAuth not basic auth", ContentHash: "h2", Scope: "project",
	})

	// Search with * should not crash or inject
	results, err := s.SearchNodes(ctx, "auth*", 10)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for auth*")
	}

	// Search with quoted content
	results, err = s.SearchNodes(ctx, `OAuth "not"`, 10)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
}

func TestEscapeFTS5(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"auth", `"auth"`},
		{"auth JWT", `"auth" OR "JWT"`},
		{`auth"injection`, `"auth""injection"`},
		{"auth*", `"auth*"`},
		{"auth -JWT", `"auth" OR "-JWT"`},
	}
	for _, tc := range cases {
		got := escapeFTS5(tc.input)
		if got != tc.want {
			t.Errorf("escapeFTS5(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestWithTxRollback(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	err := s.WithTx(ctx, func(tx Storage) error {
		_ = tx.CreateNode(ctx, &Node{ID: "rollback", Type: "convention", Content: "x", ContentHash: "h", Scope: "project"})
		return errors.New("intentional failure")
	})
	if err == nil {
		t.Fatal("expected error from WithTx")
	}

	_, err = s.GetNode(ctx, "rollback")
	if err == nil {
		t.Error("expected node to not exist after rollback")
	}
}
