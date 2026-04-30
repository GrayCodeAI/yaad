package graph

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

const (
	defaultBFSDepth     = 2
	defaultImpactDepth  = 3
	defaultSubgraphDepth = 2
)

// Acyclic edge types — cycle detection enforced on insert.
var validEdgeTypes = map[string]bool{
	"caused_by":  true,
	"led_to":     true,
	"supersedes": true,
	"learned_in": true,
	"part_of":    true,
	"relates_to": true,
	"depends_on": true,
	"touches":    true,
}

var acyclicEdges = map[string]bool{
	"caused_by":  true,
	"led_to":     true,
	"supersedes": true,
	"learned_in": true,
	"part_of":    true,
}

// IsValidEdgeType reports whether t is a recognized edge type.
func IsValidEdgeType(t string) bool { return validEdgeTypes[t] }

// IsAcyclic returns true if the edge type enforces DAG constraint.
func IsAcyclic(edgeType string) bool { return acyclicEdges[edgeType] }

// Graph is the interface for all graph operations used by Engine.
type Graph interface {
	AddNode(ctx context.Context, n *storage.Node) error
	AddEdge(ctx context.Context, e *storage.Edge) error
	RemoveNode(ctx context.Context, id string) error
	RemoveEdge(ctx context.Context, id string) error
	ExtractSubgraph(ctx context.Context, startID string, maxDepth int) (*Subgraph, error)
	BFS(ctx context.Context, startID string, maxDepth int) ([]string, error)
	IntentBFS(ctx context.Context, startID string, maxDepth int, queryIntent intent.Intent) ([]string, error)
	Impact(ctx context.Context, filePath string, maxDepth int) ([]string, error)
	Ancestors(ctx context.Context, id string) ([]string, error)
	Descendants(ctx context.Context, id string) ([]string, error)
}

// Graph wraps a Store and provides DAG operations + traversal.
type graphImpl struct {
	store storage.Storage
	db    *sql.DB
}

// New creates a Graph engine backed by the given Store and raw DB connection.
// The db handle is used for recursive CTE queries (BFS, cycle detection,
// ancestors, descendants, impact) that cannot be expressed through the Storage
// interface without leaking SQL details.
func New(store storage.Storage, db *sql.DB) Graph {
	return &graphImpl{store: store, db: db}
}

// AddNode delegates to store.
func (g *graphImpl) AddNode(ctx context.Context, n *storage.Node) error { return g.store.CreateNode(ctx, n) }

// AddEdge creates an edge, enforcing cycle detection for acyclic types.
func (g *graphImpl) AddEdge(ctx context.Context, e *storage.Edge) error {
	e.Acyclic = IsAcyclic(e.Type)
	if e.Acyclic {
		hasCycle, err := g.store.CheckCycle(ctx, e.FromID, e.ToID)
		if err != nil {
			return err
		}
		if hasCycle {
			return fmt.Errorf("cycle detected: adding edge %s → %s would create a cycle", e.FromID, e.ToID)
		}
	}
	return g.store.CreateEdge(ctx, e)
}

// RemoveNode delegates to store.
func (g *graphImpl) RemoveNode(ctx context.Context, id string) error { return g.store.DeleteNode(ctx, id) }

// RemoveEdge delegates to store.
func (g *graphImpl) RemoveEdge(ctx context.Context, id string) error { return g.store.DeleteEdge(ctx, id) }

// Subgraph holds a set of nodes and edges.
type Subgraph struct {
	Nodes []*storage.Node
	Edges []*storage.Edge
}

// BFS performs breadth-first traversal from startID up to maxDepth, returning visited node IDs.
// Acyclic edges (DAG edges: led_to, caused_by, supersedes, learned_in, part_of) are traversed
// only in their natural direction (from_id → to_id). Cyclic edges (relates_to, depends_on,
// touches) are traversed bidirectionally.
func (g *graphImpl) BFS(ctx context.Context, startID string, maxDepth int) ([]string, error) {
	query := `
		WITH RECURSIVE sg(id, depth) AS (
			SELECT ?, 0
			UNION ALL
			SELECT e.to_id, sg.depth + 1
			FROM sg
			JOIN edges e ON e.from_id = sg.id
			WHERE sg.depth < ? AND e.acyclic = 1
			UNION ALL
			SELECT CASE WHEN e.from_id = sg.id THEN e.to_id ELSE e.from_id END, sg.depth + 1
			FROM sg
			JOIN edges e ON (e.from_id = sg.id OR e.to_id = sg.id)
			WHERE sg.depth < ? AND e.acyclic = 0
		)
		SELECT DISTINCT id FROM sg`
	rows, err := g.db.QueryContext(ctx, query, startID, maxDepth, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ExtractSubgraph returns the full subgraph (nodes + edges) around startID up to maxDepth.
func (g *graphImpl) ExtractSubgraph(ctx context.Context, startID string, maxDepth int) (*Subgraph, error) {
	ids, err := g.BFS(ctx, startID, maxDepth)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &Subgraph{}, nil
	}
	sg := &Subgraph{}
	nodes, err := g.store.GetNodesBatch(ctx, ids)
	if err == nil {
		sg.Nodes = nodes
	}
	// Collect edges between subgraph nodes (single batched query, no N+1)
	edges, err := g.store.GetEdgesBetween(ctx, ids)
	if err == nil {
		sg.Edges = edges
	}
	return sg, nil
}

// Ancestors walks backwards through acyclic edges from the given node.
func (g *graphImpl) Ancestors(ctx context.Context, id string) ([]string, error) {
	query := `
		WITH RECURSIVE anc(id) AS (
			SELECT ?
			UNION ALL
			SELECT e.from_id FROM anc a
			JOIN edges e ON e.to_id = a.id AND e.acyclic = 1
		)
		SELECT DISTINCT id FROM anc WHERE id != ?`
	rows, err := g.db.QueryContext(ctx, query, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var aid string
		if err := rows.Scan(&aid); err != nil {
			return nil, err
		}
		ids = append(ids, aid)
	}
	return ids, rows.Err()
}

// Descendants walks forward through acyclic edges from the given node.
func (g *graphImpl) Descendants(ctx context.Context, id string) ([]string, error) {
	query := `
		WITH RECURSIVE desc(id) AS (
			SELECT ?
			UNION ALL
			SELECT e.to_id FROM desc d
			JOIN edges e ON e.from_id = d.id AND e.acyclic = 1
		)
		SELECT DISTINCT id FROM desc WHERE id != ?`
	rows, err := g.db.QueryContext(ctx, query, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var did string
		if err := rows.Scan(&did); err != nil {
			return nil, err
		}
		ids = append(ids, did)
	}
	return ids, rows.Err()
}

// IntentBFS performs intent-aware BFS from startID.
// Edge traversal is weighted by the query intent (MAGMA-style adaptive traversal).
// Edges with higher intent weight are traversed first and given higher scores.
func (g *graphImpl) IntentBFS(ctx context.Context, startID string, maxDepth int, queryIntent intent.Intent) ([]string, error) {
	weights := intent.Weights(queryIntent)

	// Use a priority-aware BFS: score each neighbor by edge weight × semantic fit
	// For simplicity (no embeddings required), we use edge weight as the sole score.
	type candidate struct {
		id    string
		score float64
		depth int
	}

	visited := map[string]bool{startID: true}
	queue := []candidate{{id: startID, score: 1.0, depth: 0}}
	var result []string
	result = append(result, startID)

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth >= maxDepth {
			continue
		}

		// Get all edges from this node
		edges, err := g.store.GetEdgesFrom(ctx, curr.id)
		if err != nil {
			continue
		}
		edgesTo, err := g.store.GetEdgesTo(ctx, curr.id)
		if err != nil {
			continue
		}
		allEdges := append(edges, edgesTo...)

		// Score and sort neighbors by intent weight
		type scored struct {
			id    string
			score float64
		}
		var neighbors []scored
		for _, e := range allEdges {
			neighborID := e.ToID
			if e.ToID == curr.id {
				neighborID = e.FromID
			}
			if visited[neighborID] {
				continue
			}
			edgeBoost := weights.EdgeWeight(e.Type)
			score := curr.score * edgeBoost * e.Weight
			neighbors = append(neighbors, scored{neighborID, score})
		}

		// Sort by score descending
		sort.Slice(neighbors, func(i, j int) bool {
			return neighbors[i].score > neighbors[j].score
		})

		for _, n := range neighbors {
			if !visited[n.id] {
				visited[n.id] = true
				result = append(result, n.id)
				queue = append(queue, candidate{id: n.id, score: n.score, depth: curr.depth + 1})
			}
		}
	}
	return result, nil
}

// Impact returns node IDs affected by a file change, walking backwards through the graph.
func (g *graphImpl) Impact(ctx context.Context, filePath string, maxDepth int) ([]string, error) {
	query := `
		WITH RECURSIVE affected(id, depth) AS (
			SELECT node_id, 0 FROM file_watch WHERE file_path = ?
			UNION ALL
			SELECT e.from_id, a.depth + 1
			FROM affected a
			JOIN edges e ON e.to_id = a.id
			WHERE a.depth < ?
		)
		SELECT DISTINCT id FROM affected`
	rows, err := g.db.QueryContext(ctx, query, filePath, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
