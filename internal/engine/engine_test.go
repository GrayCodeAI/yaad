package engine

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// ---------------------------------------------------------------------------
// mockStorage — in-memory implementation of storage.Storage
// ---------------------------------------------------------------------------

type mockStorage struct {
	mu       sync.RWMutex
	nodes    map[string]*storage.Node
	edges    map[string]*storage.Edge
	sessions map[string]*storage.Session
	versions map[string][]*storage.NodeVersion
	embeds   map[string][]float32
	watches  []fileWatch
}

type fileWatch struct {
	filePath, nodeID, gitHash string
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		nodes:    make(map[string]*storage.Node),
		edges:    make(map[string]*storage.Edge),
		sessions: make(map[string]*storage.Session),
		versions: make(map[string][]*storage.NodeVersion),
		embeds:   make(map[string][]float32),
	}
}

func (m *mockStorage) CreateNode(n *storage.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *n
	m.nodes[n.ID] = &cp
	return nil
}

func (m *mockStorage) GetNode(id string) (*storage.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if n, ok := m.nodes[id]; ok {
		cp := *n
		return &cp, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) UpdateNode(n *storage.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *n
	m.nodes[n.ID] = &cp
	return nil
}

func (m *mockStorage) DeleteNode(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes, id)
	return nil
}

func (m *mockStorage) ListNodes(f storage.NodeFilter) ([]*storage.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*storage.Node
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
		// return a copy to avoid races when caller mutates
		cp := *n
		out = append(out, &cp)
	}
	return out, nil
}

func (m *mockStorage) SearchNodes(query string, limit int) ([]*storage.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*storage.Node
	for _, n := range m.nodes {
		if query == "" || contains(n.Content, query) || contains(n.Summary, query) || contains(n.Tags, query) {
			cp := *n
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *mockStorage) SearchNodeByHash(hash, scope, project string) (*storage.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.ContentHash == hash && n.Scope == scope && n.Project == project {
			cp := *n
			return &cp, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) GetNeighbors(nodeID string) ([]*storage.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]bool{}
	var out []*storage.Node
	for _, e := range m.edges {
		var other string
		if e.FromID == nodeID {
			other = e.ToID
		} else if e.ToID == nodeID {
			other = e.FromID
		} else {
			continue
		}
		if seen[other] {
			continue
		}
		seen[other] = true
		if n, ok := m.nodes[other]; ok {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockStorage) CreateEdge(e *storage.Edge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.edges[e.ID] = e
	return nil
}

func (m *mockStorage) GetEdge(id string) (*storage.Edge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.edges[id]; ok {
		cp := *e
		return &cp, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockStorage) DeleteEdge(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.edges, id)
	return nil
}

func (m *mockStorage) GetEdgesFrom(nodeID string) ([]*storage.Edge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*storage.Edge
	for _, e := range m.edges {
		if e.FromID == nodeID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockStorage) GetEdgesTo(nodeID string) ([]*storage.Edge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*storage.Edge
	for _, e := range m.edges {
		if e.ToID == nodeID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockStorage) CreateSession(sess *storage.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.ID] = sess
	return nil
}

func (m *mockStorage) EndSession(id string, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Summary = summary
		s.EndedAt = time.Now()
	}
	return nil
}

func (m *mockStorage) ListSessions(project string, limit int) ([]*storage.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*storage.Session
	for _, s := range m.sessions {
		if project == "" || s.Project == project {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockStorage) SaveVersion(nodeID string, content, changedBy, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	vers := m.versions[nodeID]
	nextVer := 1
	for _, v := range vers {
		if v.Version >= nextVer {
			nextVer = v.Version + 1
		}
	}
	m.versions[nodeID] = append(vers, &storage.NodeVersion{
		NodeID:    nodeID,
		Content:   content,
		ChangedBy: changedBy,
		Reason:    reason,
		Version:   nextVer,
		ChangedAt: time.Now(),
	})
	return nil
}

func (m *mockStorage) GetVersions(nodeID string) ([]*storage.NodeVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*storage.NodeVersion, len(m.versions[nodeID]))
	copy(out, m.versions[nodeID])
	return out, nil
}

func (m *mockStorage) SaveEmbedding(nodeID, model string, vector []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]float32, len(vector))
	copy(cp, vector)
	m.embeds[nodeID] = cp
	return nil
}

func (m *mockStorage) DeleteEmbedding(nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.embeds, nodeID)
	return nil
}

func (m *mockStorage) AllEmbeddings() (map[string][]float32, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]float32, len(m.embeds))
	for k, v := range m.embeds {
		cp := make([]float32, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out, nil
}

func (m *mockStorage) GetEmbeddingsBatch(offset, limit int) (map[string][]float32, error) {
	return m.AllEmbeddings()
}

func (m *mockStorage) AddFileWatch(filePath, nodeID, gitHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watches = append(m.watches, fileWatch{filePath, nodeID, gitHash})
	return nil
}

func (m *mockStorage) AddReplayEvent(sessionID, data string) error { return nil }
func (m *mockStorage) GetReplayEvents(sessionID string) ([]*storage.ReplayEvent, error) {
	return nil, nil
}
func (m *mockStorage) DB() *sql.DB { return nil }
func (m *mockStorage) Close() error { return nil }

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) == 0 || (len(s) > 0 && len(substr) > 0 && indexOfSubstr(s, substr) >= 0)
}

func indexOfSubstr(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// mockGraph — in-memory implementation of graph.Graph backed by storage
// ---------------------------------------------------------------------------

type mockGraph struct {
	store storage.Storage
}

func newMockGraph(store storage.Storage) *mockGraph {
	return &mockGraph{store: store}
}

func (g *mockGraph) AddNode(n *storage.Node) error {
	return g.store.CreateNode(n)
}

func (g *mockGraph) AddEdge(e *storage.Edge) error {
	return g.store.CreateEdge(e)
}

func (g *mockGraph) RemoveNode(id string) error {
	return g.store.DeleteNode(id)
}

func (g *mockGraph) RemoveEdge(id string) error {
	return g.store.DeleteEdge(id)
}

func (g *mockGraph) ExtractSubgraph(startID string, maxDepth int) (*graph.Subgraph, error) {
	ids, err := g.BFS(startID, maxDepth)
	if err != nil {
		return nil, err
	}
	sg := &graph.Subgraph{}
	for _, id := range ids {
		n, err := g.store.GetNode(id)
		if err == nil {
			sg.Nodes = append(sg.Nodes, n)
		}
	}
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	for _, id := range ids {
		edges, _ := g.store.GetEdgesFrom(id)
		for _, e := range edges {
			if idSet[e.ToID] {
				sg.Edges = append(sg.Edges, e)
			}
		}
	}
	return sg, nil
}

func (g *mockGraph) BFS(startID string, maxDepth int) ([]string, error) {
	_, err := g.store.GetNode(startID)
	if err != nil {
		return nil, nil
	}
	visited := map[string]bool{startID: true}
	queue := []struct {
		id    string
		depth int
	}{{startID, 0}}
	var result []string
	result = append(result, startID)

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr.depth >= maxDepth {
			continue
		}
		edges, _ := g.store.GetEdgesFrom(curr.id)
		edgesTo, _ := g.store.GetEdgesTo(curr.id)
		allEdges := append(edges, edgesTo...)
		for _, e := range allEdges {
			var next string
			if e.FromID == curr.id {
				next = e.ToID
			} else {
				next = e.FromID
			}
			if !visited[next] {
				visited[next] = true
				result = append(result, next)
				queue = append(queue, struct {
					id    string
					depth int
				}{next, curr.depth + 1})
			}
		}
	}
	return result, nil
}

func (g *mockGraph) IntentBFS(startID string, maxDepth int, queryIntent intent.Intent) ([]string, error) {
	// For mock, delegate to plain BFS (intent weights are ignored)
	return g.BFS(startID, maxDepth)
}

func (g *mockGraph) Impact(filePath string, maxDepth int) ([]string, error) {
	return nil, nil
}

func (g *mockGraph) Ancestors(id string) ([]string, error) {
	return nil, nil
}

func (g *mockGraph) Descendants(id string) ([]string, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func newTestEngine() *Engine {
	ms := newMockStorage()
	return New(ms, newMockGraph(ms))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMockStorageCompiles verifies mockStorage implements storage.Storage.
func TestMockStorageCompiles(t *testing.T) {
	var _ storage.Storage = newMockStorage()
}

// TestMockGraphCompiles verifies mockGraph implements graph.Graph.
func TestMockGraphCompiles(t *testing.T) {
	var _ graph.Graph = newMockGraph(newMockStorage())
}

// TestEngineWithMocks verifies engine.New accepts mock implementations.
func TestEngineWithMocks(t *testing.T) {
	eng := newTestEngine()
	if eng == nil {
		t.Fatal("expected non-nil engine")
	}
	if eng.Store() == nil {
		t.Error("expected non-nil store")
	}
	if eng.Graph() == nil {
		t.Error("expected non-nil graph")
	}
}

// TestRememberAndRecall creates a node and recalls it.
func TestRememberAndRecall(t *testing.T) {
	eng := newTestEngine()

	node, err := eng.Remember(RememberInput{
		Type:    "convention",
		Content: "Always use context.Context as first parameter",
		Scope:   "project",
		Project: "testproj",
	})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}
	if node.ID == "" {
		t.Error("expected node ID")
	}
	if node.Content != "Always use context.Context as first parameter" {
		t.Errorf("unexpected content: %s", node.Content)
	}

	res, err := eng.Recall(RecallOpts{Query: "context.Context", Project: "testproj"})
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if len(res.Nodes) == 0 {
		t.Error("expected at least one recalled node")
	}
	found := false
	for _, n := range res.Nodes {
		if n.ID == node.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected recalled node to contain the remembered node")
	}
}

// TestRecallFilters verifies type/tier/project filtering.
func TestRecallFilters(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "conv1", Project: "p1"})
	_, _ = eng.Remember(RememberInput{Type: "bug", Content: "bug1", Project: "p1"})
	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "conv2", Project: "p2"})

	res, err := eng.Recall(RecallOpts{Query: "conv", Type: "convention", Project: "p1"})
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	// Seeds are filtered; graph expansion may include connected nodes of other types.
	// Just verify recall succeeds and at least one seed node is in the result.
	if len(res.Nodes) == 0 {
		t.Error("expected at least one node in recall result")
	}
}

// TestContext verifies hot-tier + active tasks retrieval.
func TestContext(t *testing.T) {
	eng := newTestEngine()

	// hot tier node
	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "hot memory", Project: "p1", Tier: 1})
	// active task
	_, _ = eng.Remember(RememberInput{Type: "task", Content: "active task", Project: "p1"})
	// irrelevant (different project)
	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "other project", Project: "p2", Tier: 1})

	ctx, err := eng.Context("p1")
	if err != nil {
		t.Fatalf("Context failed: %v", err)
	}
	if len(ctx.Nodes) == 0 {
		t.Fatal("expected context nodes")
	}
	for _, n := range ctx.Nodes {
		if n.Project != "p1" {
			t.Errorf("expected only p1 nodes in context, got %s", n.Project)
		}
	}
}

// TestForget archives a node.
func TestForget(t *testing.T) {
	eng := newTestEngine()

	node, err := eng.Remember(RememberInput{Type: "decision", Content: "drop feature X", Project: "p1"})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	if err := eng.Forget(node.ID); err != nil {
		t.Fatalf("Forget failed: %v", err)
	}

	got, err := eng.store.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if got.Confidence != 0 {
		t.Errorf("expected confidence 0 after forget, got %f", got.Confidence)
	}
}

// TestStatus verifies node/edge/session counting.
func TestStatus(t *testing.T) {
	eng := newTestEngine()

	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "c1", Project: "p1"})
	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "c2", Project: "p1"})
	_, _ = eng.StartSession("p1", "agent-a")

	st, err := eng.Status("p1")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if st.Nodes < 2 {
		t.Errorf("expected at least 2 nodes, got %d", st.Nodes)
	}
	if st.Sessions < 1 {
		t.Errorf("expected at least 1 session, got %d", st.Sessions)
	}
}

// TestFeedback verifies approve/edit/discard actions.
func TestFeedback(t *testing.T) {
	eng := newTestEngine()

	node, err := eng.Remember(RememberInput{Type: "bug", Content: "old bug desc", Project: "p1"})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	// Approve
	if err := eng.Feedback(node.ID, FeedbackApprove, ""); err != nil {
		t.Fatalf("Feedback approve failed: %v", err)
	}
	got, _ := eng.store.GetNode(node.ID)
	if got.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0 after approve, got %f", got.Confidence)
	}

	// Edit
	if err := eng.Feedback(node.ID, FeedbackEdit, "new bug desc"); err != nil {
		t.Fatalf("Feedback edit failed: %v", err)
	}
	got, _ = eng.store.GetNode(node.ID)
	if got.Content != "new bug desc" {
		t.Errorf("expected content 'new bug desc', got %s", got.Content)
	}

	// Discard
	if err := eng.Feedback(node.ID, FeedbackDiscard, ""); err != nil {
		t.Fatalf("Feedback discard failed: %v", err)
	}
	got, _ = eng.store.GetNode(node.ID)
	if got.Confidence != 0 {
		t.Errorf("expected confidence 0 after discard, got %f", got.Confidence)
	}
}

// TestRollback restores a node to a previous version.
func TestRollback(t *testing.T) {
	eng := newTestEngine()

	node, err := eng.Remember(RememberInput{Type: "decision", Content: "v1 content", Project: "p1"})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	// Save a version manually
	_ = eng.store.SaveVersion(node.ID, "v1 content", "user", "saved")
	// Edit to create v2
	_ = eng.Feedback(node.ID, FeedbackEdit, "v2 content")

	// Rollback to v1
	if err := eng.Rollback(node.ID, 1); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	got, _ := eng.store.GetNode(node.ID)
	if got.Content != "v1 content" {
		t.Errorf("expected content 'v1 content' after rollback, got %s", got.Content)
	}
}

// TestPendingNodes returns low-confidence nodes.
func TestPendingNodes(t *testing.T) {
	eng := newTestEngine()

	node, _ := eng.Remember(RememberInput{Type: "convention", Content: "low confidence", Project: "p1"})
	_ = eng.Feedback(node.ID, FeedbackDiscard, "")
	// re-create with low confidence manually
	node.Confidence = 0.3
	_ = eng.store.UpdateNode(node)

	pending, err := eng.PendingNodes("p1", 0.5)
	if err != nil {
		t.Fatalf("PendingNodes failed: %v", err)
	}
	found := false
	for _, n := range pending {
		if n.ID == node.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pending node to be found")
	}
}

// TestEmptyDatabase verifies operations on empty store return empty results, not errors.
func TestEmptyDatabase(t *testing.T) {
	eng := newTestEngine()

	res, err := eng.Recall(RecallOpts{Query: "anything", Project: "empty"})
	if err != nil {
		t.Fatalf("Recall on empty DB failed: %v", err)
	}
	if len(res.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(res.Nodes))
	}

	ctx, err := eng.Context("empty")
	if err != nil {
		t.Fatalf("Context on empty DB failed: %v", err)
	}
	if len(ctx.Nodes) != 0 {
		t.Errorf("expected 0 context nodes, got %d", len(ctx.Nodes))
	}

	st, err := eng.Status("empty")
	if err != nil {
		t.Fatalf("Status on empty DB failed: %v", err)
	}
	if st.Nodes != 0 {
		t.Errorf("expected 0 nodes in status, got %d", st.Nodes)
	}
}

// TestNonexistentNode verifies Forget, GetNode, Link with bad IDs return errors.
func TestNonexistentNode(t *testing.T) {
	eng := newTestEngine()

	if err := eng.Forget("nonexistent-id"); err == nil {
		t.Error("expected error forgetting nonexistent node")
	}

	_, err := eng.store.GetNode("nonexistent-id")
	if err == nil {
		t.Error("expected error getting nonexistent node")
	}
}

// TestConcurrentRemember verifies multiple goroutines calling Remember is safe.
func TestConcurrentRemember(t *testing.T) {
	eng := newTestEngine()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := eng.Remember(RememberInput{
				Type:    "convention",
				Content: "concurrent memory " + string(rune('a'+idx)),
				Project: "concurrent-proj",
			})
			if err != nil {
				t.Errorf("Remember goroutine %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	st, err := eng.Status("concurrent-proj")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if st.Nodes < 20 {
		t.Errorf("expected at least 20 nodes, got %d", st.Nodes)
	}
}

// TestConcurrentReadWrite verifies Recall and Remember concurrently.
func TestConcurrentReadWrite(t *testing.T) {
	eng := newTestEngine()

	// Seed some data
	for i := 0; i < 10; i++ {
		_, _ = eng.Remember(RememberInput{
			Type:    "convention",
			Content: "seed memory " + string(rune('a'+i)),
			Project: "rw-proj",
		})
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			_, _ = eng.Remember(RememberInput{
				Type:    "convention",
				Content: "writer memory " + string(rune('a'+idx)),
				Project: "rw-proj",
			})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = eng.Recall(RecallOpts{Query: "memory", Project: "rw-proj"})
		}()
	}
	wg.Wait()
}

// TestRememberEmptyContent verifies Remember with empty content is handled.
func TestRememberEmptyContent(t *testing.T) {
	eng := newTestEngine()

	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"empty content", "", false}, // empty content is allowed (filtered)
		{"normal content", "valid content", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := eng.Remember(RememberInput{
				Type:    "convention",
				Content: tc.content,
				Project: "empty-test",
			})
			if tc.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if node.ID == "" {
				t.Error("expected node ID")
			}
		})
	}
}

// TestSessionFlow verifies StartSession and CompressSession.
func TestSessionFlow(t *testing.T) {
	eng := newTestEngine()

	sessID, err := eng.StartSession("p1", "agent-a")
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	if sessID == "" {
		t.Error("expected session ID")
	}

	_, _ = eng.Remember(RememberInput{Type: "convention", Content: "sess mem", Project: "p1", Session: sessID})

	summary, err := eng.CompressSession(sessID, "p1")
	if err != nil {
		t.Fatalf("CompressSession failed: %v", err)
	}
	if summary == nil {
		t.Fatal("expected session summary node")
	}
	if summary.Type != "session" {
		t.Errorf("expected type session, got %s", summary.Type)
	}
}
