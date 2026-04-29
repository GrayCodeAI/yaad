package storage

import (
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

func (m *mockStorage) CreateNode(n *Node) error {
	m.nodes[n.ID] = n
	return nil
}

func (m *mockStorage) GetNode(id string) (*Node, error) {
	if n, ok := m.nodes[id]; ok {
		return n, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) UpdateNode(n *Node) error {
	m.nodes[n.ID] = n
	return nil
}

func (m *mockStorage) DeleteNode(id string) error {
	delete(m.nodes, id)
	return nil
}

func (m *mockStorage) ListNodes(f NodeFilter) ([]*Node, error) {
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

func (m *mockStorage) SearchNodes(query string, limit int) ([]*Node, error) {
	return m.ListNodes(NodeFilter{})
}

func (m *mockStorage) SearchNodeByHash(hash, scope, project string) (*Node, error) {
	for _, n := range m.nodes {
		if n.ContentHash == hash && n.Scope == scope && n.Project == project {
			return n, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) GetNeighbors(nodeID string) ([]*Node, error) {
	return nil, nil
}

func (m *mockStorage) CreateEdge(e *Edge) error {
	m.edges[e.ID] = e
	return nil
}

func (m *mockStorage) GetEdge(id string) (*Edge, error) {
	if e, ok := m.edges[id]; ok {
		return e, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) DeleteEdge(id string) error {
	delete(m.edges, id)
	return nil
}

func (m *mockStorage) GetEdgesFrom(nodeID string) ([]*Edge, error) {
	var out []*Edge
	for _, e := range m.edges {
		if e.FromID == nodeID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *mockStorage) GetEdgesTo(nodeID string) ([]*Edge, error) {
	var out []*Edge
	for _, e := range m.edges {
		if e.ToID == nodeID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *mockStorage) CreateSession(sess *Session) error {
	return nil
}

func (m *mockStorage) EndSession(id string, summary string) error {
	return nil
}

func (m *mockStorage) ListSessions(project string, limit int) ([]*Session, error) {
	return nil, nil
}

func (m *mockStorage) SaveVersion(nodeID string, content, changedBy, reason string) error {
	return nil
}

func (m *mockStorage) GetVersions(nodeID string) ([]*NodeVersion, error) {
	return nil, nil
}

func (m *mockStorage) SaveEmbedding(nodeID, model string, vector []float32) error {
	return nil
}

func (m *mockStorage) DeleteEmbedding(nodeID string) error {
	return nil
}

func (m *mockStorage) AllEmbeddings() (map[string][]float32, error) {
	return nil, nil
}

func (m *mockStorage) GetEmbeddingsBatch(offset, limit int) (map[string][]float32, error) {
	return nil, nil
}

func (m *mockStorage) AddReplayEvent(sessionID, data string) error {
	return nil
}

func (m *mockStorage) GetReplayEvents(sessionID string) ([]*ReplayEvent, error) {
	return nil, nil
}

func (m *mockStorage) DB() *sql.DB {
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

// TestMockStorageCompiles verifies the mock implements Storage.
func TestMockStorageCompiles(t *testing.T) {
	var _ Storage = newMockStorage()
}

// TestMockStorageCRUD verifies basic CRUD on the mock.
func TestMockStorageCRUD(t *testing.T) {
	m := newMockStorage()

	node := &Node{ID: "n1", Type: "convention", Content: "test", ContentHash: "h1", Scope: "project"}
	if err := m.CreateNode(node); err != nil {
		t.Fatal(err)
	}

	got, err := m.GetNode("n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "test" {
		t.Errorf("expected content 'test', got %q", got.Content)
	}

	edge := &Edge{ID: "e1", FromID: "n1", ToID: "n1", Type: "relates_to"}
	if err := m.CreateEdge(edge); err != nil {
		t.Fatal(err)
	}

	edges, err := m.GetEdgesFrom("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edges))
	}
}
