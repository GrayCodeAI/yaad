package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/compact"
	"github.com/GrayCodeAI/yaad/conflict"
	"github.com/GrayCodeAI/yaad/dedup"
	"github.com/GrayCodeAI/yaad/graph"
	"github.com/GrayCodeAI/yaad/intent"
	"github.com/GrayCodeAI/yaad/mental"
	"github.com/GrayCodeAI/yaad/privacy"
	"github.com/GrayCodeAI/yaad/profile"
	"github.com/GrayCodeAI/yaad/storage"
	"github.com/GrayCodeAI/yaad/temporal"
)

// Validation and default constants.
const (
	maxContentLength    = 10000 // characters
	defaultRecallDepth  = 2
	defaultRecallLimit  = 10
	confidenceBoost     = 0.2
	minConfidence       = 0.1
	halfLifeDays        = 30.0
	busyTimeoutMs       = 5000
	compactThreshold    = 50000
	defaultDecayBoost   = 0.2
)

var validNodeTypes = map[string]bool{
	"convention": true,
	"decision":   true,
	"bug":        true,
	"spec":       true,
	"task":       true,
	"preference": true,
	"skill":      true,
	"file":       true,
	"entity":     true,
	"session":    true,
}

// IsValidNodeType reports whether typ is a recognized node type.
func IsValidNodeType(typ string) bool {
	return validNodeTypes[typ]
}

// Metrics tracks basic engine operation counters.
type Metrics struct {
	Remembers   int64
	Recalls     int64
	Errors      int64
	NodesStored int64
}

// Engine is the core memory engine wrapping graph + storage.
type Engine struct {
	store       storage.Storage
	graph       graph.Graph
	dedup       *dedup.Window
	temporal    *temporal.Backbone
	conflict    *conflict.Resolver
	access      *AccessTracker
	summarizer  compact.Summarizer
	metrics     Metrics
	DecayConfig DecayConfig
	mu          sync.Mutex // serializes writes (Remember, Forget, Feedback, etc.)
}

// New creates a memory engine.
func New(store storage.Storage, g graph.Graph) *Engine {
	return &Engine{
		store:       store,
		graph:       g,
		dedup:       dedup.New(5 * time.Minute),
		temporal:    temporal.New(store),
		conflict:    conflict.New(store),
		access:      NewAccessTracker(store, 30*time.Second),
		summarizer:  compact.DefaultSummarizer{},
		DecayConfig: DefaultDecayConfig,
	}
}

// WithSummarizer sets a custom summarizer for compaction (e.g., LLM-backed).
func (e *Engine) WithSummarizer(s compact.Summarizer) *Engine {
	e.summarizer = s
	return e
}

// Close shuts down the engine and its background workers.
func (e *Engine) Close() {
	if e.access != nil {
		e.access.Stop()
		e.access.Flush(context.Background())
	}
}

// Graph returns the underlying graph engine.
func (e *Engine) Graph() graph.Graph { return e.graph }

// Store returns the underlying store.
func (e *Engine) Store() storage.Storage { return e.store }

// RememberInput is the input for creating a memory node.
type RememberInput struct {
	Type    string // convention|decision|bug|spec|task|preference
	Content string
	Summary string
	Scope   string // global|project
	Project string
	Tier    int
	Tags    string
	Key     string // optional unique key per project (upsert: same key → update, not duplicate)
	Pinned  bool   // pinned nodes always appear in context output
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

// validateRememberInput checks that input meets basic constraints.
func validateRememberInput(in RememberInput) error {
	if len(in.Content) == 0 {
		return fmt.Errorf("content cannot be empty")
	}
	if len(in.Content) > maxContentLength {
		return fmt.Errorf("content exceeds max length of %d characters", maxContentLength)
	}
	if in.Type != "" && !validNodeTypes[in.Type] {
		return fmt.Errorf("invalid node type: %q", in.Type)
	}
	return nil
}

// Remember creates a memory node with privacy filtering, dedup, and entity extraction.
func (e *Engine) Remember(ctx context.Context, in RememberInput) (*storage.Node, error) {
	atomic.AddInt64(&e.metrics.Remembers, 1)
	if err := ctx.Err(); err != nil {
		atomic.AddInt64(&e.metrics.Errors, 1)
		return nil, err
	}
	if err := validateRememberInput(in); err != nil {
		atomic.AddInt64(&e.metrics.Errors, 1)
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

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

	// 3. Content hash for exact dedup
	hash := contentHash(content, in.Scope, in.Project)

	// 4. Keyed upsert: if key is set, find existing node and update it
	if in.Key != "" {
		existing, _ := e.store.GetNodeByKey(ctx, in.Key, in.Project)
		if existing != nil {
			// Save version before overwriting
			_ = e.store.SaveVersion(ctx, existing.ID, existing.Content, in.Agent, "keyed update")
			existing.Content = content
			existing.ContentHash = hash
			existing.Summary = summary
			existing.Type = in.Type
			existing.Tags = in.Tags
			existing.Pinned = in.Pinned
			existing.Confidence = 1.0
			existing.AccessCount++
			existing.AccessedAt = time.Now()
			existing.UpdatedAt = time.Now()
			existing.Version++
			if err := e.store.UpdateNode(ctx, existing); err != nil {
				return nil, fmt.Errorf("keyed upsert failed: %w", err)
			}
			atomic.AddInt64(&e.metrics.NodesStored, 1)
			return existing, nil
		}
	}

	// 5. Rolling window dedup (skip near-duplicates within 5min)
	if e.dedup.IsDuplicate(content) {
		existing, _ := e.store.SearchNodeByHash(ctx, hash, in.Scope, in.Project)
		if existing != nil {
			return existing, nil
		}
	}

	// 6. Check dedup — if exists, boost confidence and return
	existing, _ := e.store.SearchNodeByHash(ctx, hash, in.Scope, in.Project)
	if existing != nil {
		existing.Confidence = min(existing.Confidence+0.2, 1.0)
		existing.AccessCount++
		existing.AccessedAt = time.Now()
		if err := e.store.UpdateNode(ctx, existing); err != nil {
			return nil, fmt.Errorf("dedup boost failed: %w", err)
		}
		return existing, nil
	}

	// 8. Create node
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
		Key:           in.Key,
		Pinned:        in.Pinned,
		Confidence:    1.0,
		SourceSession: in.Session,
		SourceAgent:   in.Agent,
		Version:       1,
	}
	if err := e.graph.AddNode(ctx, node); err != nil {
		return nil, fmt.Errorf("create node failed: %w", err)
	}

	// 9. Extract entities and create anchor nodes + edges (best-effort)
	entities := ExtractEntities(content)
	for _, ent := range entities {
		entNode := e.getOrCreateAnchor(ctx, ent.Name, ent.Type, in.Scope, in.Project)
		if entNode != nil {
			if err := e.graph.AddEdge(ctx, &storage.Edge{
				ID:     uuid.New().String(),
				FromID: node.ID,
				ToID:   entNode.ID,
				Type:   "touches",
				Weight: 1.0,
			}); err != nil {
				// Entity edge is best-effort; don't fail Remember
				continue
			}
		}
	}

	// 10. Create explicit edges (fail if user-requested edges can't be created)
	for _, ei := range in.Edges {
		if err := e.graph.AddEdge(ctx, &storage.Edge{
			ID:     uuid.New().String(),
			FromID: node.ID,
			ToID:   ei.ToID,
			Type:   ei.Type,
			Weight: 1.0,
		}); err != nil {
			return nil, fmt.Errorf("link edge failed: %w", err)
		}
	}

	// 11. Temporal backbone — auto-link to previous node in timeline
	if err := e.temporal.Link(ctx, node.ID, in.Project); err != nil {
		return nil, fmt.Errorf("temporal link failed: %w", err)
	}

	// 12. Conflict resolution — detect and supersede contradictions (best-effort)
	_, _ = e.conflict.CheckAndResolve(ctx, node)

	// 13. Self-linking — find related nodes and create edges (A-MEM inspired, best-effort)
	e.SelfLink(ctx, node)

	atomic.AddInt64(&e.metrics.NodesStored, 1)
	return node, nil
}

// RecallOpts configures a recall search.
type RecallOpts struct {
	Query   string
	Depth   int
	Limit   int
	Budget  int // max tokens in response (0 = no cap)
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
func (e *Engine) Recall(ctx context.Context, opts RecallOpts) (*RecallResult, error) {
	atomic.AddInt64(&e.metrics.Recalls, 1)
	if err := ctx.Err(); err != nil {
		atomic.AddInt64(&e.metrics.Errors, 1)
		return nil, err
	}
	if opts.Depth == 0 {
		opts.Depth = 2
	}
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	// Stage 1: BM25 seed nodes
	seeds, err := e.store.SearchNodes(ctx, opts.Query, opts.Limit)
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
		ids, err := e.graph.IntentBFS(ctx, seed.ID, opts.Depth, queryIntent)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if n, err := e.store.GetNode(ctx, id); err == nil {
				nodeMap[n.ID] = n
			}
		}
		// Also get edges for the subgraph
		sg, err := e.graph.ExtractSubgraph(ctx, seed.ID, opts.Depth)
		if err == nil {
			allEdges = append(allEdges, sg.Edges...)
		}
	}

	// Stage 3: Rank by confidence × recency
	nodes := make([]*storage.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		// Log access via lightweight INSERT (batched flush) instead of UPDATE churn
		e.access.Log(ctx, n.ID)
		nodes = append(nodes, n)
	}
	sortByScore(nodes)

	// Trim to limit
	if len(nodes) > opts.Limit {
		nodes = nodes[:opts.Limit]
	}

	// Enforce token budget if specified
	if opts.Budget > 0 {
		nodes = TrimToTokenBudget(nodes, opts.Budget)
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
// Pinned nodes always appear first (guaranteed 500-token budget), followed by
// hot-tier and active tasks filling the remaining budget.
func (e *Engine) Context(ctx context.Context, project string) (*RecallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	const totalBudget = 4000  // tokens
	const pinnedBudget = 500  // reserved for pinned

	// Load pinned nodes (always-in-context, like Letta's core memory)
	pinTrue := true
	pinnedNodes, err := e.store.ListNodes(ctx, storage.NodeFilter{
		Project: project, Pinned: &pinTrue,
	})
	if err != nil {
		return nil, fmt.Errorf("load pinned: %w", err)
	}
	pinnedNodes = TrimToTokenBudget(pinnedNodes, pinnedBudget)

	// Load hot tier nodes
	hotNodes, err := e.store.ListNodes(ctx, storage.NodeFilter{
		Tier: 1, Project: project, MinConfidence: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("load hot tier: %w", err)
	}

	// Load active tasks
	tasks, err := e.store.ListNodes(ctx, storage.NodeFilter{
		Type: "task", Project: project, MinConfidence: 0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	// Merge hot+tasks, excluding already-pinned nodes
	pinnedSet := map[string]bool{}
	for _, n := range pinnedNodes {
		pinnedSet[n.ID] = true
	}
	nodeMap := map[string]*storage.Node{}
	for _, n := range hotNodes {
		if !pinnedSet[n.ID] {
			nodeMap[n.ID] = n
		}
	}
	for _, n := range tasks {
		if !pinnedSet[n.ID] {
			nodeMap[n.ID] = n
		}
	}

	rest := make([]*storage.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		rest = append(rest, n)
	}
	sortByScore(rest)

	// Trim rest to remaining budget
	remainingBudget := totalBudget - tokenCount(pinnedNodes)
	rest = TrimToTokenBudget(rest, remainingBudget)

	// Pinned first, then rest
	nodes := make([]*storage.Node, 0, len(pinnedNodes)+len(rest))
	nodes = append(nodes, pinnedNodes...)
	nodes = append(nodes, rest...)

	return &RecallResult{Nodes: nodes}, nil
}

func tokenCount(nodes []*storage.Node) int {
	total := 0
	for _, n := range nodes {
		total += (len(n.Content) + len(n.Summary) + len(n.Tags) + 50) / 4
	}
	return total
}

// Forget archives a node by setting confidence to 0.
func (e *Engine) Forget(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	node, err := e.store.GetNode(ctx, id)
	if err != nil {
		return err
	}
	// Save version before archiving
	if err := e.store.SaveVersion(ctx, node.ID, node.Content, "system", "archived"); err != nil {
		return fmt.Errorf("save version failed: %w", err)
	}
	node.Confidence = 0
	return e.store.UpdateNode(ctx, node)
}

// Status returns basic stats.
type Status struct {
	Nodes    int
	Edges    int
	Sessions int
}

// Compact merges low-confidence memories to keep the graph lean.
func (e *Engine) Compact(ctx context.Context, project string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c := compact.New(e.store, 50000).WithSummarizer(e.summarizer)
	return c.Compact(ctx, project)
}

// MentalModel generates an auto-evolving project summary.
func (e *Engine) MentalModel(ctx context.Context, project string) (*mental.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return mental.Generate(ctx, e.store, project)
}

// Profile returns an auto-maintained user/project profile (static facts + dynamic context).
func (e *Engine) Profile(ctx context.Context, project string) (*profile.Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profile.Build(ctx, e.store, project)
}

func (e *Engine) Status(ctx context.Context, project string) (*Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	sessions, err := e.store.ListSessions(ctx, project, 1000)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	// Count total edges with a single query instead of N+1
	edgeCount, _ := e.store.CountAllEdges(ctx)
	return &Status{Nodes: len(nodes), Edges: edgeCount, Sessions: len(sessions)}, nil
}

// GetMetrics returns a copy of the engine's operational metrics.
func (e *Engine) GetMetrics() Metrics {
	return Metrics{
		Remembers:   atomic.LoadInt64(&e.metrics.Remembers),
		Recalls:     atomic.LoadInt64(&e.metrics.Recalls),
		Errors:      atomic.LoadInt64(&e.metrics.Errors),
		NodesStored: atomic.LoadInt64(&e.metrics.NodesStored),
	}
}

// --- helpers ---

func contentHash(content, scope, project string) string {
	h := sha256.Sum256([]byte(content + "\x00" + scope + "\x00" + project))
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

func (e *Engine) getOrCreateAnchor(ctx context.Context, name, typ, scope, project string) *storage.Node {
	hash := contentHash(name, scope, project)
	existing, _ := e.store.SearchNodeByHash(ctx, hash, scope, project)
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
	if err := e.store.CreateNode(ctx, node); err != nil {
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
	now := time.Now()
	sort.Slice(nodes, func(i, j int) bool {
		return score(nodes[i], now) > score(nodes[j], now)
	})
}

func score(n *storage.Node, now time.Time) float64 {
	// Recency: exponential decay based on last access or update
	recency := 1.0
	ref := n.AccessedAt
	if ref.IsZero() {
		ref = n.UpdatedAt
	}
	if !ref.IsZero() {
		days := now.Sub(ref).Hours() / 24
		if days > 0 {
			recency = 1.0 / (1.0 + days/30.0)
		}
	}

	// Tier boost
	tierBoost := 1.0
	switch n.Tier {
	case 1:
		tierBoost = 2.0
	case 2:
		tierBoost = 1.3
	}

	// Pinned nodes get a fixed high boost
	pinnedBoost := 1.0
	if n.Pinned {
		pinnedBoost = 3.0
	}

	return n.Confidence * recency * tierBoost * pinnedBoost * (1.0 + float64(n.AccessCount)*0.05)
}



