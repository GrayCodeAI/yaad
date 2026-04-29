package storage

import "database/sql"

// Storage is the persistence interface used by Engine and other packages.
type Storage interface {
	// Nodes
	CreateNode(n *Node) error
	GetNode(id string) (*Node, error)
	UpdateNode(n *Node) error
	DeleteNode(id string) error
	ListNodes(f NodeFilter) ([]*Node, error)
	SearchNodes(query string, limit int) ([]*Node, error)
	SearchNodeByHash(hash, scope, project string) (*Node, error)
	GetNeighbors(nodeID string) ([]*Node, error)

	// Edges
	CreateEdge(e *Edge) error
	GetEdge(id string) (*Edge, error)
	DeleteEdge(id string) error
	GetEdgesFrom(nodeID string) ([]*Edge, error)
	GetEdgesTo(nodeID string) ([]*Edge, error)

	// Sessions
	CreateSession(sess *Session) error
	EndSession(id string, summary string) error
	ListSessions(project string, limit int) ([]*Session, error)

	// Versions
	SaveVersion(nodeID string, content, changedBy, reason string) error
	GetVersions(nodeID string) ([]*NodeVersion, error)

	// Embeddings
	SaveEmbedding(nodeID, model string, vector []float32) error
	DeleteEmbedding(nodeID string) error
	AllEmbeddings() (map[string][]float32, error)
	GetEmbeddingsBatch(offset, limit int) (map[string][]float32, error)

	// Replay
	AddReplayEvent(sessionID, data string) error
	GetReplayEvents(sessionID string) ([]*ReplayEvent, error)

	// File watch (staleness tracking)
	AddFileWatch(filePath, nodeID, gitHash string) error

	// Raw DB access (for packages that need recursive CTEs)
	DB() *sql.DB

	Close() error
}
