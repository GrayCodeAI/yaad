package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

const maxAccessBuffer = 10000

// AccessTracker batches node access events to reduce SQLite UPDATE churn.
// Instead of updating nodes.access_count on every recall (which causes write
// contention under concurrent load), it INSERTs lightweight rows into
// access_log and periodically flushes them in a single aggregated UPDATE.
type AccessTracker struct {
	store     storage.Storage
	buffer    []string // node IDs pending flush
	mu        sync.Mutex
	flushTick *time.Ticker
	stopCh    chan struct{}
	stopped   sync.Once
}

// NewAccessTracker creates a tracker that flushes every interval.
func NewAccessTracker(store storage.Storage, interval time.Duration) *AccessTracker {
	at := &AccessTracker{
		store:     store,
		buffer:    make([]string, 0, 128),
		flushTick: time.NewTicker(interval),
		stopCh:    make(chan struct{}),
	}
	go at.loop()
	return at
}

// Log records an access for the given node ID (best-effort, non-blocking).
func (at *AccessTracker) Log(ctx context.Context, nodeID string) {
	// Try SQLite INSERT directly (fast, append-only, minimal contention)
	if err := at.store.LogAccess(ctx, nodeID); err == nil {
		return
	}
	// Fall back to in-memory buffer if DB is temporarily unavailable
	at.mu.Lock()
	if len(at.buffer) < maxAccessBuffer {
		at.buffer = append(at.buffer, nodeID)
	} else {
		slog.Warn("access_tracker: buffer full, dropping access log", "node_id", nodeID)
	}
	at.mu.Unlock()
}

// Flush immediately applies all pending access counts to nodes.
func (at *AccessTracker) Flush(ctx context.Context) {
	// Flush any buffered in-memory items first
	at.mu.Lock()
	buf := make([]string, len(at.buffer))
	copy(buf, at.buffer)
	at.buffer = at.buffer[:0]
	at.mu.Unlock()

	for _, nodeID := range buf {
		_ = at.store.LogAccess(ctx, nodeID)
	}

	n, err := at.store.FlushAccessLog(ctx)
	if err != nil {
		slog.Warn("access_tracker: flush failed", "error", err)
	} else if n > 0 {
		slog.Debug("access_tracker: flushed", "nodes_updated", n)
	}
}

// Stop halts the background flusher. Safe to call multiple times.
func (at *AccessTracker) Stop() {
	at.stopped.Do(func() {
		at.flushTick.Stop()
		close(at.stopCh)
	})
}

func (at *AccessTracker) loop() {
	for {
		select {
		case <-at.flushTick.C:
			at.Flush(context.Background())
		case <-at.stopCh:
			return
		}
	}
}
