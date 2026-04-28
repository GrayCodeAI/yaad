package engine

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/privacy"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Engine is the core memory engine wrapping graph + storage.
type Engine struct {
	store *storage.Store
	graph *graph.Graph
}

// New creates a memory engine.
func New(store *storage.Store) *Engine {
	return &Engine{store: store, graph: graph.New(store)}
}

// Graph returns the underlying graph engine.
func (e *Engine) Graph() *graph.Graph { return e.graph }

// Store returns the underlying store.
func (e *Engine) Store() *storage.Store { return e.store }

// RememberInput is the input for creating a memory node.
type RememberInput struct {
	Type    string // convention|decision|bug|spec|task|preference
	Content string
	Summary string
	Scope   string // global|project
	Project string
	Tier    int
	Tags    string
	Session string
	Agent   string
	// Optional: explicit edges to create
	Edges []EdgeInput
}

// EdgeInput describes an edge to create alongside a node.
type EdgeInput struct {
	ToID string
	Type string
}

// Remember creates a memory node with privacy filtering, dedup, and entity extraction.
func (e *Engine) Remember(in RememberInput) (*storage.Node, error) {
	// 1. Privacy filter
	content := privacy.Filter(in.Content)
	summary := privacy.Filter(in.Summary)

	// 2. Defaults
	if in.Scope == "" {
		in.Scope = "project"
	}
	if in.Tier == 0 {
		in.Tier = defaultTier(in.Type)
	}

	// 3. Content hash for dedup
	hash := contentHash(content, in.Scope, in.Project)

	// 4. Check dedup — if exists, boost confidence
	existing, _ := e.store.SearchNodeByHash(hash, in.Scope, in.Project)
	if existing != nil {
		existing.Confidence = min(existing.Confidence+0.2, 1.0)
		existing.AccessCount++
		existing.AccessedAt = time.Now()
		_ = e.store.UpdateNode(existing)
		return existing, nil
	}

	// 5. Create node
	node := &storage.Node{
		ID:            uuid.New().String(),
		Type:          in.Type,
		Content:       content,
		ContentHash:   hash,
		Summary:       summary,
		Scope:         in.Scope,
		Project:       in.Project,
		Tier:          in.Tier,
		Tags:          in.Tags,
		Confidence:    1.0,
		SourceSession: in.Session,
		SourceAgent:   in.Agent,
		Version:       1,
	}
	if err := e.graph.AddNode(node); err != nil {
		return nil, err
	}

	// 6. Extract entities and create anchor nodes + edges
	entities := ExtractEntities(content)
	for _, ent := range entities {
		entNode := e.getOrCreateAnchor(ent.Name, ent.Type, in.Scope, in.Project)
		if entNode != nil {
			_ = e.graph.AddEdge(&storage.Edge{
				ID:     uuid.New().String(),
				FromID: node.ID,
				ToID:   entNode.ID,
				Type:   "touches",
				Weight: 1.0,
			})
		}
	}

	// 7. Create explicit edges
	for _, ei := range in.Edges {
		_ = e.graph.AddEdge(&storage.Edge{
			ID:     uuid.New().String(),
			FromID: node.ID,
			ToID:   ei.ToID,
			Type:   ei.Type,
			Weight: 1.0,
		})
	}

	return node, nil
}

// RecallOpts configures a recall search.
type RecallOpts struct {
	Query   string
	Depth   int
	Limit   int
	Type    string
	Tier    int
	Project string
}

// RecallResult holds search results.
type RecallResult struct {
	Nodes []*storage.Node
	Edges []*storage.Edge
}

// Recall performs graph-aware hybrid search: BM25 seed → graph expand → rank.
func (e *Engine) Recall(opts RecallOpts) (*RecallResult, error) {
	if opts.Depth == 0 {
		opts.Depth = 2
	}
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	// Stage 1: BM25 seed nodes
	seeds, err := e.store.SearchNodes(opts.Query, opts.Limit)
	if err != nil {
		return nil, err
	}

	// Filter by type/tier/project if specified
	seeds = filterNodes(seeds, opts)

	if len(seeds) == 0 {
		return &RecallResult{}, nil
	}

	// Classify query intent (MAGMA: intent-aware routing)
	queryIntent := intent.Classify(opts.Query)

	// Stage 2: Intent-aware graph expansion
	nodeMap := map[string]*storage.Node{}
	var allEdges []*storage.Edge
	for _, seed := range seeds {
		nodeMap[seed.ID] = seed
		// Use IntentBFS for intent-aware traversal
		ids, err := e.graph.IntentBFS(seed.ID, opts.Depth, queryIntent)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if n, err := e.store.GetNode(id); err == nil {
				nodeMap[n.ID] = n
			}
		}
		// Also get edges for the subgraph
		sg, err := e.graph.ExtractSubgraph(seed.ID, opts.Depth)
		if err == nil {
			allEdges = append(allEdges, sg.Edges...)
		}
	}

	// Stage 3: Rank by confidence × recency
	nodes := make([]*storage.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		// Boost access
		n.AccessCount++
		n.AccessedAt = time.Now()
		_ = e.store.UpdateNode(n)
		nodes = append(nodes, n)
	}
	sortByScore(nodes)

	// Trim to limit
	if len(nodes) > opts.Limit {
		nodes = nodes[:opts.Limit]
	}

	// Dedup edges
	edgeMap := map[string]*storage.Edge{}
	for _, edge := range allEdges {
		edgeMap[edge.ID] = edge
	}
	edges := make([]*storage.Edge, 0, len(edgeMap))
	for _, edge := range edgeMap {
		edges = append(edges, edge)
	}

	return &RecallResult{Nodes: nodes, Edges: edges}, nil
}

// Context returns the hot-tier subgraph for session start injection.
func (e *Engine) Context(project string) (*RecallResult, error) {
	// Load hot tier nodes
	hotNodes, err := e.store.ListNodes(storage.NodeFilter{
		Tier: 1, Project: project, MinConfidence: 0.3,
	})
	if err != nil {
		return nil, err
	}

	// Load active tasks
	tasks, err := e.store.ListNodes(storage.NodeFilter{
		Type: "task", Project: project, MinConfidence: 0.1,
	})
	if err != nil {
		return nil, err
	}

	// Merge, dedup
	nodeMap := map[string]*storage.Node{}
	for _, n := range hotNodes {
		nodeMap[n.ID] = n
	}
	for _, n := range tasks {
		nodeMap[n.ID] = n
	}

	nodes := make([]*storage.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}
	sortByScore(nodes)

	return &RecallResult{Nodes: nodes}, nil
}

// Forget archives a node by setting confidence to 0.
func (e *Engine) Forget(id string) error {
	node, err := e.store.GetNode(id)
	if err != nil {
		return err
	}
	// Save version before archiving
	_ = e.store.SaveVersion(node.ID, node.Content, "system", "archived")
	node.Confidence = 0
	return e.store.UpdateNode(node)
}

// Status returns basic stats.
type Status struct {
	Nodes    int
	Edges    int
	Sessions int
}

func (e *Engine) Status(project string) (*Status, error) {
	nodes, err := e.store.ListNodes(storage.NodeFilter{Project: project})
	if err != nil {
		return nil, err
	}
	sessions, err := e.store.ListSessions(project, 1000)
	if err != nil {
		return nil, err
	}
	// Count edges (approximate via node edges)
	edgeCount := 0
	for _, n := range nodes {
		edges, _ := e.store.GetEdgesFrom(n.ID)
		edgeCount += len(edges)
	}
	return &Status{Nodes: len(nodes), Edges: edgeCount, Sessions: len(sessions)}, nil
}

// --- helpers ---

func contentHash(content, scope, project string) string {
	h := sha256.Sum256([]byte(content + "|" + scope + "|" + project))
	return fmt.Sprintf("%x", h)
}

func defaultTier(nodeType string) int {
	switch nodeType {
	case "convention", "preference", "task":
		return 1
	case "decision", "bug":
		return 2
	default:
		return 3
	}
}

func (e *Engine) getOrCreateAnchor(name, typ, scope, project string) *storage.Node {
	hash := contentHash(name, scope, project)
	existing, _ := e.store.SearchNodeByHash(hash, scope, project)
	if existing != nil {
		return existing
	}
	node := &storage.Node{
		ID:          uuid.New().String(),
		Type:        typ,
		Content:     name,
		ContentHash: hash,
		Scope:       scope,
		Project:     project,
		Tier:        0, // anchor nodes don't have a tier
		Confidence:  1.0,
		Version:     1,
	}
	if err := e.store.CreateNode(node); err != nil {
		return nil
	}
	return node
}

func filterNodes(nodes []*storage.Node, opts RecallOpts) []*storage.Node {
	if opts.Type == "" && opts.Tier == 0 && opts.Project == "" {
		return nodes
	}
	var out []*storage.Node
	for _, n := range nodes {
		if opts.Type != "" && n.Type != opts.Type {
			continue
		}
		if opts.Tier != 0 && n.Tier != opts.Tier {
			continue
		}
		if opts.Project != "" && n.Project != opts.Project {
			continue
		}
		out = append(out, n)
	}
	return out
}

func sortByScore(nodes []*storage.Node) {
	// Simple sort: confidence × recency
	now := time.Now()
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			si := score(nodes[i], now)
			sj := score(nodes[j], now)
			if sj > si {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}
}

func score(n *storage.Node, now time.Time) float64 {
	recency := 1.0
	if !n.AccessedAt.IsZero() {
		days := now.Sub(n.AccessedAt).Hours() / 24
		if days > 0 {
			recency = 1.0 / (1.0 + days/30.0)
		}
	}
	tierBoost := 1.0
	if n.Tier == 1 {
		tierBoost = 2.0
	}
	return n.Confidence * recency * tierBoost * (1.0 + float64(n.AccessCount)*0.1)
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
