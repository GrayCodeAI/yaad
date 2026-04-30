package storage

import (
	"context"
	"database/sql"
	"testing"
)

// mockStorage is a minimal in-memory implementation of the Storage interface
// for testing compilation and basic usage.
type mockStorage struct {
	nodes map[string]*Node
	edges map[string]*Edge
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		nodes: make(map[string]*Node),
		edges: make(map[string]*Edge),
	}
}

func (m *mockStorage) CreateNode(ctx context.Context, n *Node) error {
	m.nodes[n.ID] = n
	return nil
}

func (m *mockStorage) GetNode(ctx context.Context, id string) (*Node, error) {
	if n, ok := m.nodes[id]; ok {
		return n, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) GetNodesBatch(ctx context.Context, ids []string) ([]*Node, error) {
	var out []*Node
	for _, id := range ids {
		if n, ok := m.nodes[id]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *mockStorage) UpdateNode(ctx context.Context, n *Node) error {
	m.nodes[n.ID] = n
	return nil
}

func (m *mockStorage) DeleteNode(ctx context.Context, id string) error {
	delete(m.nodes, id)
	return nil
}

func (m *mockStorage) ListNodes(ctx context.Context, f NodeFilter) ([]*Node, error) {
	var out []*Node
	for _, n := range m.nodes {
		if f.Type != "" && n.Type != f.Type {
			continue
		}
		if f.Scope != "" && n.Scope != f.Scope {
			continue
		}
		if f.Project != "" && n.Project != f.Project {
			continue
		}
		if f.Tier > 0 && n.Tier != f.Tier {
			continue
		}
		if f.MinConfidence > 0 && n.Confidence < f.MinConfidence {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (m *mockStorage) SearchNodes(ctx context.Context, query string, limit int) ([]*Node, error) {
	return m.ListNodes(ctx, NodeFilter{})
}

func (m *mockStorage) SearchNodeByHash(ctx context.Context, hash, scope, project string) (*Node, error) {
	for _, n := range m.nodes {
		if n.ContentHash == hash && n.Scope == scope && n.Project == project {
			return n, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) GetNeighbors(ctx context.Context, nodeID string) ([]*Node, error) {
	return nil, nil
}

func (m *mockStorage) CreateEdge(ctx context.Context, e *Edge) error {
	m.edges[e.ID] = e
	return nil
}

func (m *mockStorage) GetEdge(ctx context.Context, id string) (*Edge, error) {
	if e, ok := m.edges[id]; ok {
		return e, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) DeleteEdge(ctx context.Context, id string) error {
	delete(m.edges, id)
	return nil
}

func (m *mockStorage) GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error) {
	var out []*Edge
	for _, e := range m.edges {
		if e.FromID == nodeID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *mockStorage) GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error) {
	var out []*Edge
	for _, e := range m.edges {
		if e.ToID == nodeID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *mockStorage) GetEdgesBetween(ctx context.Context, nodeIDs []string) ([]*Edge, error) {
	idSet := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		idSet[id] = true
	}
	var out []*Edge
	for _, e := range m.edges {
		if idSet[e.FromID] && idSet[e.ToID] {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *mockStorage) CountEdges(ctx context.Context, nodeID string) (inbound int, outbound int, err error) {
	for _, e := range m.edges {
		if e.FromID == nodeID {
			outbound++
		}
		if e.ToID == nodeID {
			inbound++
		}
	}
	return inbound, outbound, nil
}

func (m *mockStorage) CountAllEdges(ctx context.Context) (int, error) {
	return len(m.edges), nil
}

func (m *mockStorage) CheckCycle(ctx context.Context, fromID, toID string) (bool, error) {
	// Simple cycle check for mock: walk backwards from fromID
	seen := map[string]bool{}
	var walk func(id string) bool
	walk = func(id string) bool {
		if id == toID {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, e := range m.edges {
			if e.ToID == id && IsAcyclic(e.Type) {
				if walk(e.FromID) {
					return true
				}
			}
		}
		return false
	}
	return walk(fromID), nil
}

func (m *mockStorage) CreateSession(ctx context.Context, sess *Session) error {
	return nil
}

func (m *mockStorage) EndSession(ctx context.Context, id string, summary string) error {
	return nil
}

func (m *mockStorage) ListSessions(ctx context.Context, project string, limit int) ([]*Session, error) {
	return nil, nil
}

func (m *mockStorage) SaveVersion(ctx context.Context, nodeID string, content, changedBy, reason string) error {
	return nil
}

func (m *mockStorage) GetVersions(ctx context.Context, nodeID string) ([]*NodeVersion, error) {
	return nil, nil
}

func (m *mockStorage) SaveEmbedding(ctx context.Context, nodeID, model string, vector []float32) error {
	return nil
}

func (m *mockStorage) DeleteEmbedding(ctx context.Context, nodeID string) error {
	return nil
}

func (m *mockStorage) AllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	return nil, nil
}

func (m *mockStorage) GetEmbeddingsBatch(ctx context.Context, offset, limit int) (map[string][]float32, error) {
	return nil, nil
}

func (m *mockStorage) AddFileWatch(ctx context.Context, filePath, nodeID, gitHash string) error {
	return nil
}

func (m *mockStorage) AddReplayEvent(ctx context.Context, sessionID, data string) error {
	return nil
}

func (m *mockStorage) GetReplayEvents(ctx context.Context, sessionID string) ([]*ReplayEvent, error) {
	return nil, nil
}

func (m *mockStorage) LogAccess(ctx context.Context, nodeID string) error { return nil }
func (m *mockStorage) FlushAccessLog(ctx context.Context) (int, error) { return 0, nil }

func (m *mockStorage) WithTx(ctx context.Context, fn func(Storage) error) error {
	return fn(m)
}

func (m *mockStorage) Close() error {
	return nil
}

// IsAcyclic returns true if the edge type enforces DAG constraint.
func IsAcyclic(edgeType string) bool {
	return edgeType == "caused_by" || edgeType == "led_to" || edgeType == "supersedes" ||
		edgeType == "learned_in" || edgeType == "part_of"
}

// TestMockStorageCompiles verifies the mock implements Storage.
func TestMockStorageCompiles(t *testing.T) {
	var _ Storage = newMockStorage()
}

// TestMockStorageCRUD verifies basic CRUD on the mock.
func TestMockStorageCRUD(t *testing.T) {
	m := newMockStorage()
	ctx := context.Background()

	node := &Node{ID: "n1", Type: "convention", Content: "test", ContentHash: "h1", Scope: "project"}
	if err := m.CreateNode(ctx, node); err != nil {
		t.Fatal(err)
	}

	got, err := m.GetNode(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "test" {
		t.Errorf("expected content 'test', got %q", got.Content)
	}

	edge := &Edge{ID: "e1", FromID: "n1", ToID: "n1", Type: "relates_to"}
	if err := m.CreateEdge(ctx, edge); err != nil {
		t.Fatal(err)
	}

	edges, err := m.GetEdgesFrom(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edges))
	}
}
