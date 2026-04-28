package graph

import (
	"database/sql"
	"fmt"

	"github.com/yaadmemory/yaad/internal/intent"
	"github.com/yaadmemory/yaad/internal/storage"
)

// Acyclic edge types — cycle detection enforced on insert.
var acyclicEdges = map[string]bool{
	"caused_by":  true,
	"led_to":     true,
	"supersedes": true,
	"learned_in": true,
	"part_of":    true,
}

// IsAcyclic returns true if the edge type enforces DAG constraint.
func IsAcyclic(edgeType string) bool { return acyclicEdges[edgeType] }

// Graph wraps a Store and provides DAG operations + traversal.
type Graph struct {
	store *storage.Store
	db    *sql.DB
}

// New creates a Graph engine backed by the given Store.
func New(store *storage.Store) *Graph {
	return &Graph{store: store, db: store.DB()}
}

// AddNode delegates to store.
func (g *Graph) AddNode(n *storage.Node) error { return g.store.CreateNode(n) }

// AddEdge creates an edge, enforcing cycle detection for acyclic types.
func (g *Graph) AddEdge(e *storage.Edge) error {
	e.Acyclic = IsAcyclic(e.Type)
	if e.Acyclic {
		if err := g.checkCycle(e.FromID, e.ToID); err != nil {
			return err
		}
	}
	return g.store.CreateEdge(e)
}

// RemoveNode delegates to store.
func (g *Graph) RemoveNode(id string) error { return g.store.DeleteNode(id) }

// RemoveEdge delegates to store.
func (g *Graph) RemoveEdge(id string) error { return g.store.DeleteEdge(id) }

// checkCycle uses recursive CTE to detect if adding from→to would create a cycle
// among acyclic edges. Returns error if cycle detected.
func (g *Graph) checkCycle(fromID, toID string) error {
	query := `
		WITH RECURSIVE ancestors(id) AS (
			SELECT ?
			UNION ALL
			SELECT e.from_id FROM ancestors a
			JOIN edges e ON e.to_id = a.id AND e.acyclic = 1
		)
		SELECT 1 FROM ancestors WHERE id = ? LIMIT 1`
	var exists int
	err := g.db.QueryRow(query, fromID, toID).Scan(&exists)
	if err == nil {
		return fmt.Errorf("cycle detected: adding edge %s → %s would create a cycle", fromID, toID)
	}
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}

// Subgraph holds a set of nodes and edges.
type Subgraph struct {
	Nodes []*storage.Node
	Edges []*storage.Edge
}

// BFS performs breadth-first traversal from startID up to maxDepth, returning visited node IDs.
func (g *Graph) BFS(startID string, maxDepth int) ([]string, error) {
	query := `
		WITH RECURSIVE sg(id, depth) AS (
			SELECT ?, 0
			UNION ALL
			SELECT CASE WHEN e.from_id = sg.id THEN e.to_id ELSE e.from_id END, sg.depth + 1
			FROM sg
			JOIN edges e ON e.from_id = sg.id OR e.to_id = sg.id
			WHERE sg.depth < ?
		)
		SELECT DISTINCT id FROM sg`
	rows, err := g.db.Query(query, startID, maxDepth)
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
func (g *Graph) ExtractSubgraph(startID string, maxDepth int) (*Subgraph, error) {
	ids, err := g.BFS(startID, maxDepth)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &Subgraph{}, nil
	}
	sg := &Subgraph{}
	for _, id := range ids {
		n, err := g.store.GetNode(id)
		if err != nil {
			continue // skip missing nodes
		}
		sg.Nodes = append(sg.Nodes, n)
	}
	// Collect edges between subgraph nodes
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	for _, id := range ids {
		edges, err := g.store.GetEdgesFrom(id)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if idSet[e.ToID] {
				sg.Edges = append(sg.Edges, e)
			}
		}
	}
	return sg, nil
}

// Ancestors walks backwards through acyclic edges from the given node.
func (g *Graph) Ancestors(id string) ([]string, error) {
	query := `
		WITH RECURSIVE anc(id) AS (
			SELECT ?
			UNION ALL
			SELECT e.from_id FROM anc a
			JOIN edges e ON e.to_id = a.id AND e.acyclic = 1
		)
		SELECT DISTINCT id FROM anc WHERE id != ?`
	rows, err := g.db.Query(query, id, id)
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
func (g *Graph) Descendants(id string) ([]string, error) {
	query := `
		WITH RECURSIVE desc(id) AS (
			SELECT ?
			UNION ALL
			SELECT e.to_id FROM desc d
			JOIN edges e ON e.from_id = d.id AND e.acyclic = 1
		)
		SELECT DISTINCT id FROM desc WHERE id != ?`
	rows, err := g.db.Query(query, id, id)
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
func (g *Graph) IntentBFS(startID string, maxDepth int, queryIntent intent.Intent) ([]string, error) {
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
		edges, err := g.store.GetEdgesFrom(curr.id)
		if err != nil {
			continue
		}
		edgesTo, err := g.store.GetEdgesTo(curr.id)
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

		// Sort by score descending (simple insertion sort for small lists)
		for i := 1; i < len(neighbors); i++ {
			for j := i; j > 0 && neighbors[j].score > neighbors[j-1].score; j-- {
				neighbors[j], neighbors[j-1] = neighbors[j-1], neighbors[j]
			}
		}

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
func (g *Graph) Impact(filePath string, maxDepth int) ([]string, error) {
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
	rows, err := g.db.Query(query, filePath, maxDepth)
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
