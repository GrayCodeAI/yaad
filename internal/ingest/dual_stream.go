// Package ingest implements dual-stream memory ingestion.
// Based on MAGMA (arxiv:2601.03236) and GAM (arxiv:2604.12285).
//
// Yaad is a memory layer — it does NOT call LLM APIs directly.
// The coding agent (Hawk, Claude Code, Cursor, etc.) handles the LLM.
// Yaad stores, retrieves, and organizes memories.
//
// Fast path (sync): non-blocking — store node + temporal edge, return immediately.
// Slow path (async): background goroutine — heuristic causal inference, entity linking.
package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/utils"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// SlowPathJob is a unit of work for the slow path.
type SlowPathJob struct {
	NodeID  string
	Content string
	Project string
	Eng     *engine.Engine
	Graph   graph.Graph
}

// DualStream manages fast + slow path ingestion.
type DualStream struct {
	eng      *engine.Engine
	graph    graph.Graph
	queue    chan SlowPathJob
	wg       sync.WaitGroup
	once     sync.Once
	lastNode map[string]string // project → last node ID (temporal backbone)
	mu       sync.Mutex
}

// New creates a DualStream ingestion manager.
func New(eng *engine.Engine) *DualStream {
	ds := &DualStream{
		eng:      eng,
		graph:    eng.Graph(),
		queue:    make(chan SlowPathJob, 256),
		lastNode: map[string]string{},
	}
	ds.startWorker()
	return ds
}

// Remember is the fast path: stores node + temporal edge synchronously, then
// enqueues slow-path work (heuristic causal inference) asynchronously.
func (ds *DualStream) Remember(ctx context.Context, in engine.RememberInput) (*storage.Node, error) {
	// Fast path: store node
	node, err := ds.eng.Remember(ctx, in)
	if err != nil {
		return nil, err
	}

	// Fast path: add temporal backbone edge (immutable, ordered)
	ds.mu.Lock()
	prevID := ds.lastNode[in.Project]
	ds.lastNode[in.Project] = node.ID
	ds.mu.Unlock()

	if prevID != "" {
		_ = ds.graph.AddEdge(ctx, &storage.Edge{
			ID:     uuid.New().String(),
			FromID: prevID,
			ToID:   node.ID,
			Type:   "learned_in",
			Weight: 1.0,
		})
	}

	// Enqueue slow path (non-blocking). Do NOT capture the request context —
	// the background worker must outlive the HTTP handler.
	select {
	case ds.queue <- SlowPathJob{
		NodeID:  node.ID,
		Content: node.Content,
		Project: in.Project,
		Eng:     ds.eng,
		Graph:   ds.graph,
	}:
	default:
		// Queue full — skip slow path for this node (graceful degradation)
	}

	return node, nil
}

// startWorker launches the background slow-path worker.
func (ds *DualStream) startWorker() {
	ds.once.Do(func() {
		ds.wg.Add(1)
		go func() {
			defer ds.wg.Done()
			for job := range ds.queue {
				ds.slowPath(job)
			}
		}()
	})
}

// slowPath performs heuristic causal inference in the background.
// No LLM calls — Yaad is a memory layer, not an LLM client.
// The coding agent handles LLM; Yaad handles memory structure.
func (ds *DualStream) slowPath(job SlowPathJob) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	node, err := job.Eng.Store().GetNode(ctx, job.NodeID)
	if err != nil {
		return
	}

	neighbors, err := job.Graph.BFS(ctx, job.NodeID, 2)
	if err != nil || len(neighbors) < 2 {
		return
	}

	for _, neighborID := range neighbors {
		if neighborID == job.NodeID {
			continue
		}
		neighbor, err := job.Eng.Store().GetNode(ctx, neighborID)
		if err != nil {
			continue
		}
		// decision → led_to → convention
		if neighbor.Type == "decision" && node.Type == "convention" {
			_ = job.Graph.AddEdge(ctx, &storage.Edge{
				ID: uuid.New().String(), FromID: neighborID, ToID: job.NodeID,
				Type: "led_to", Weight: 0.8,
			})
		}
		// decision ← caused_by ← bug
		if neighbor.Type == "decision" && node.Type == "bug" {
			_ = job.Graph.AddEdge(ctx, &storage.Edge{
				ID: uuid.New().String(), FromID: job.NodeID, ToID: neighborID,
				Type: "caused_by", Weight: 0.7,
			})
		}
		// convention → part_of → spec
		if neighbor.Type == "spec" && node.Type == "convention" {
			_ = job.Graph.AddEdge(ctx, &storage.Edge{
				ID: uuid.New().String(), FromID: job.NodeID, ToID: neighborID,
				Type: "part_of", Weight: 0.6,
			})
		}
	}

	slog.Info("slow path processed node", "node_id", utils.ShortID(job.NodeID), "type", node.Type)
}

// Stop gracefully shuts down the slow-path worker.
func (ds *DualStream) Stop() {
	close(ds.queue)
	ds.wg.Wait()
}
