package storage

import (
	"context"
)

// Storage is the persistence interface used by Engine and other packages.
// All graph-aware queries (recursive CTEs, cycle detection) are encapsulated
// behind methods so callers never need raw *sql.DB access.
type Storage interface {
	// Nodes
	CreateNode(ctx context.Context, n *Node) error
	GetNode(ctx context.Context, id string) (*Node, error)
	GetNodesBatch(ctx context.Context, ids []string) ([]*Node, error)
	UpdateNode(ctx context.Context, n *Node) error
	DeleteNode(ctx context.Context, id string) error
	ListNodes(ctx context.Context, f NodeFilter) ([]*Node, error)
	SearchNodes(ctx context.Context, query string, limit int) ([]*Node, error)
	SearchNodeByHash(ctx context.Context, hash, scope, project string) (*Node, error)
	GetNeighbors(ctx context.Context, nodeID string) ([]*Node, error)

	// Edges
	CreateEdge(ctx context.Context, e *Edge) error
	GetEdge(ctx context.Context, id string) (*Edge, error)
	DeleteEdge(ctx context.Context, id string) error
	GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error)
	GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error)
	GetEdgesBetween(ctx context.Context, nodeIDs []string) ([]*Edge, error)
	CountEdges(ctx context.Context, nodeID string) (inbound int, outbound int, err error)
	CountAllEdges(ctx context.Context) (int, error)

	// Graph queries (encapsulates recursive CTEs)
	CheckCycle(ctx context.Context, fromID, toID string) (bool, error)

	// Sessions
	CreateSession(ctx context.Context, sess *Session) error
	EndSession(ctx context.Context, id string, summary string) error
	ListSessions(ctx context.Context, project string, limit int) ([]*Session, error)

	// Versions
	SaveVersion(ctx context.Context, nodeID string, content, changedBy, reason string) error
	GetVersions(ctx context.Context, nodeID string) ([]*NodeVersion, error)

	// Embeddings
	SaveEmbedding(ctx context.Context, nodeID, model string, vector []float32) error
	DeleteEmbedding(ctx context.Context, nodeID string) error
	AllEmbeddings(ctx context.Context) (map[string][]float32, error)
	GetEmbeddingsBatch(ctx context.Context, offset, limit int) (map[string][]float32, error)

	// Replay
	AddReplayEvent(ctx context.Context, sessionID, data string) error
	GetReplayEvents(ctx context.Context, sessionID string) ([]*ReplayEvent, error)

	// File watch (staleness tracking)
	AddFileWatch(ctx context.Context, filePath, nodeID, gitHash string) error

	// AccessLog: lightweight access tracking (batched flush)
	LogAccess(ctx context.Context, nodeID string) error
	FlushAccessLog(ctx context.Context) (int, error)

	// Transactions
	WithTx(ctx context.Context, fn func(Storage) error) error

	Close() error
}
