package engine

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/GrayCodeAI/yaad/storage"
)

// MemoryStats holds aggregate statistics about the memory graph.
type MemoryStats struct {
	TotalNodes  int
	NodesByType map[string]int
	TotalEdges  int
	LastUpdated time.Time
	TopTopics   []string // top 5 most-connected node subjects
}

// GetMemoryStats returns aggregate statistics about the memory graph.
func (e *Engine) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	db, ok := e.store.(*storage.Store)
	if !ok {
		return nil, fmt.Errorf("GetMemoryStats requires a *storage.Store backend")
	}
	d := db.DB()
	stats := &MemoryStats{NodesByType: make(map[string]int)}

	// Nodes by type
	rows, err := d.QueryContext(ctx, `SELECT type, COUNT(*) FROM nodes GROUP BY type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var cnt int
		if err := rows.Scan(&typ, &cnt); err != nil {
			return nil, err
		}
		stats.NodesByType[typ] = cnt
		stats.TotalNodes += cnt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Total edges
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&stats.TotalEdges); err != nil {
		return nil, err
	}

	// Last updated
	var lastUpdated sql.NullTime
	if err := d.QueryRowContext(ctx, `SELECT MAX(updated_at) FROM nodes`).Scan(&lastUpdated); err != nil {
		return nil, err
	}
	if lastUpdated.Valid {
		stats.LastUpdated = lastUpdated.Time
	}

	// Top 5 most-connected nodes by edge count
	topRows, err := d.QueryContext(ctx, `SELECT n.content, COUNT(*) as cnt
		FROM edges e JOIN nodes n ON n.id = e.from_id OR n.id = e.to_id
		GROUP BY n.id ORDER BY cnt DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer topRows.Close()
	for topRows.Next() {
		var content string
		var cnt int
		if err := topRows.Scan(&content, &cnt); err != nil {
			return nil, err
		}
		stats.TopTopics = append(stats.TopTopics, content)
	}
	if err := topRows.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

// NodeHistoryEntry represents one version of a memory node.
type NodeHistoryEntry struct {
	Version   int    `json:"version"`
	Content   string `json:"content"`
	ChangedBy string `json:"changed_by"`
	Reason    string `json:"reason"`
	ChangedAt string `json:"changed_at"`
}

// GetNodeHistory returns the version history of a memory node.
func (e *Engine) GetNodeHistory(ctx context.Context, nodeID string) ([]NodeHistoryEntry, error) {
	store, ok := e.store.(*storage.Store)
	if !ok {
		return nil, nil
	}
	versions, err := store.GetVersions(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]NodeHistoryEntry, len(versions))
	for i, v := range versions {
		out[i] = NodeHistoryEntry{
			Version:   v.Version,
			Content:   v.Content,
			ChangedBy: v.ChangedBy,
			Reason:    v.Reason,
			ChangedAt: v.ChangedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return out, nil
}
