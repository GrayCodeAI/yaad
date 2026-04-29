// Package yaad provides the public Go SDK for embedding Yaad memory
// into any Go application or coding agent.
//
// Usage:
//
//	import "github.com/GrayCodeAI/yaad/sdk/go/yaad"
//
//	mem, _ := yaad.Open(".yaad/yaad.db")
//	defer mem.Close()
//
//	node, _ := mem.Remember("Use jose for JWT", yaad.Convention)
//	results, _ := mem.Recall("auth middleware")
//	context, _ := mem.Context("")
//	model, _ := mem.MentalModel("")
package yaad

import (
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/mental"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Memory types.
const (
	Convention = "convention"
	Decision   = "decision"
	Bug        = "bug"
	Spec       = "spec"
	Task       = "task"
	Skill      = "skill"
	Preference = "preference"
)

// Node is a memory node in the Yaad graph.
type Node = storage.Node

// Edge is a relationship between two nodes.
type Edge = storage.Edge

// RecallResult holds search results with nodes and edges.
type RecallResult = engine.RecallResult

// MentalModel is an auto-generated project summary.
type MentalModel = mental.Model

// Memory is the main Yaad SDK handle.
type Memory struct {
	eng   *engine.Engine
	store *storage.Store
}

// Open opens a Yaad database at the given path.
// Creates the database and schema if it doesn't exist.
func Open(dbPath string) (*Memory, error) {
	store, err := storage.NewStore(dbPath)
	if err != nil {
		return nil, err
	}
	return &Memory{eng: engine.New(store, graph.New(store)), store: store}, nil
}

// Close closes the database connection.
func (m *Memory) Close() error {
	return m.store.Close()
}

// Remember stores a memory node with auto entity extraction,
// dedup, temporal linking, and conflict resolution.
func (m *Memory) Remember(content string, nodeType string, opts ...RememberOption) (*Node, error) {
	in := engine.RememberInput{
		Content: content,
		Type:    nodeType,
		Scope:   "project",
	}
	for _, opt := range opts {
		opt(&in)
	}
	return m.eng.Remember(in)
}

// RememberOption configures a Remember call.
type RememberOption func(*engine.RememberInput)

// WithTags sets tags on the memory.
func WithTags(tags string) RememberOption {
	return func(in *engine.RememberInput) { in.Tags = tags }
}

// WithProject sets the project scope.
func WithProject(project string) RememberOption {
	return func(in *engine.RememberInput) { in.Project = project }
}

// WithSummary sets a short summary.
func WithSummary(summary string) RememberOption {
	return func(in *engine.RememberInput) { in.Summary = summary }
}

// WithSession sets the session ID.
func WithSession(session string) RememberOption {
	return func(in *engine.RememberInput) { in.Session = session }
}

// WithAgent sets the agent name.
func WithAgent(agent string) RememberOption {
	return func(in *engine.RememberInput) { in.Agent = agent }
}

// Recall performs graph-aware search: BM25 + vector + graph + temporal.
func (m *Memory) Recall(query string, opts ...RecallOption) (*RecallResult, error) {
	ro := engine.RecallOpts{Query: query, Depth: 2, Limit: 10}
	for _, opt := range opts {
		opt(&ro)
	}
	return m.eng.Recall(ro)
}

// RecallOption configures a Recall call.
type RecallOption func(*engine.RecallOpts)

// WithDepth sets the graph expansion depth.
func WithDepth(depth int) RecallOption {
	return func(ro *engine.RecallOpts) { ro.Depth = depth }
}

// WithLimit sets the max results.
func WithLimit(limit int) RecallOption {
	return func(ro *engine.RecallOpts) { ro.Limit = limit }
}

// WithType filters by node type.
func WithType(nodeType string) RecallOption {
	return func(ro *engine.RecallOpts) { ro.Type = nodeType }
}

// Context returns the hot-tier subgraph for session start injection.
func (m *Memory) Context(project string) (*RecallResult, error) {
	return m.eng.Context(project)
}

// Forget archives a memory node.
func (m *Memory) Forget(id string) error {
	return m.eng.Forget(id)
}

// Compact merges low-confidence memories to keep the graph lean.
func (m *Memory) Compact(project string) (int, error) {
	return m.eng.Compact(project)
}

// MentalModel generates an auto-evolving project summary.
func (m *Memory) MentalModel(project string) (*MentalModel, error) {
	return m.eng.MentalModel(project)
}

// Feedback applies user feedback to a memory node.
func (m *Memory) Approve(id string) error {
	return m.eng.Feedback(id, "approve", "")
}

// Edit updates a memory node's content.
func (m *Memory) Edit(id string, newContent string) error {
	return m.eng.Feedback(id, "edit", newContent)
}

// Discard archives a memory node via feedback.
func (m *Memory) Discard(id string) error {
	return m.eng.Feedback(id, "discard", "")
}
