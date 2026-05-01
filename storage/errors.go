package storage

import "errors"

// Domain-specific errors for the storage layer.
// Callers should use errors.Is() to check for specific error conditions.
var (
	ErrNodeNotFound      = errors.New("node not found")
	ErrEdgeNotFound      = errors.New("edge not found")
	ErrSessionNotFound   = errors.New("session not found")
	ErrCycleDetected     = errors.New("cycle detected")
	ErrDuplicateNode     = errors.New("duplicate node")
	ErrDuplicateEdge     = errors.New("duplicate edge")
	ErrInvalidNodeType   = errors.New("invalid node type")
	ErrInvalidEdgeType   = errors.New("invalid edge type")
	ErrContentTooLong    = errors.New("content exceeds maximum length")
	ErrEmptyContent      = errors.New("content cannot be empty")
	ErrDatabaseLocked    = errors.New("database is locked")
	ErrBatchTooLarge     = errors.New("batch size exceeds database limit")
	ErrVersionNotFound   = errors.New("version not found")
	ErrEmbeddingNotFound = errors.New("embedding not found")
)
