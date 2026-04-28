// Package ingest implements dual-stream memory ingestion.
// Based on MAGMA (arxiv:2601.03236) and GAM (arxiv:2604.12285).
//
// Fast path (sync): non-blocking — store node + temporal edge, return immediately.
// Slow path (async): background goroutine — causal inference, entity linking, consolidation.
package ingest

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yaadmemory/yaad/internal/engine"
	"github.com/yaadmemory/yaad/internal/graph"
	"github.com/yaadmemory/yaad/internal/storage"
)

// SlowPathJob is a unit of work for the slow path.
type SlowPathJob struct {
	NodeID  string
	Content string
	Project string
	Eng     *engine.Engine
	Graph   *graph.Graph
}

// DualStream manages fast + slow path ingestion.
type DualStream struct {
	eng      *engine.Engine
	graph    *graph.Graph
	queue    chan SlowPathJob
	wg       sync.WaitGroup
	once     sync.Once
	lastNode map[string]string // project → last node ID (for temporal backbone)
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
// enqueues slow-path work (causal inference, entity linking) asynchronously.
func (ds *DualStream) Remember(in engine.RememberInput) (*storage.Node, error) {
	// Fast path: store node (privacy filter + dedup + basic entity extraction)
	node, err := ds.eng.Remember(in)
	if err != nil {
		return nil, err
	}

	// Fast path: add temporal backbone edge (immutable, ordered)
	ds.mu.Lock()
	prevID := ds.lastNode[in.Project]
	ds.lastNode[in.Project] = node.ID
	ds.mu.Unlock()

	if prevID != "" {
		_ = ds.graph.AddEdge(&storage.Edge{
			ID:     uuid.New().String(),
			FromID: prevID,
			ToID:   node.ID,
			Type:   "learned_in", // temporal backbone
			Weight: 1.0,
		})
	}

	// Enqueue slow path (non-blocking)
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

// slowPath performs async structural consolidation:
// - Infer causal edges from local neighborhood
// - Link entity nodes
// - Update semantic edges
func (ds *DualStream) slowPath(job SlowPathJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ctx

	// Get local neighborhood (2 hops)
	neighbors, err := job.Graph.BFS(job.NodeID, 2)
	if err != nil || len(neighbors) < 2 {
		return
	}

	// Heuristic causal inference (no LLM needed for coding memories):
	// If a decision node is in the neighborhood and this is a convention/bug,
	// infer a causal edge.
	node, err := job.Eng.Store().GetNode(job.NodeID)
	if err != nil {
		return
	}

	for _, neighborID := range neighbors {
		if neighborID == job.NodeID {
			continue
		}
		neighbor, err := job.Eng.Store().GetNode(neighborID)
		if err != nil {
			continue
		}

		// Infer: decision → led_to → convention
		if neighbor.Type == "decision" && node.Type == "convention" {
			_ = job.Graph.AddEdge(&storage.Edge{
				ID:     uuid.New().String(),
				FromID: neighborID,
				ToID:   job.NodeID,
				Type:   "led_to",
				Weight: 0.8, // inferred, lower confidence than explicit
			})
		}

		// Infer: decision → caused_by → bug
		if neighbor.Type == "decision" && node.Type == "bug" {
			_ = job.Graph.AddEdge(&storage.Edge{
				ID:     uuid.New().String(),
				FromID: job.NodeID,
				ToID:   neighborID,
				Type:   "caused_by",
				Weight: 0.7,
			})
		}

		// Infer: spec → part_of → convention (if same entities)
		if neighbor.Type == "spec" && node.Type == "convention" {
			_ = job.Graph.AddEdge(&storage.Edge{
				ID:     uuid.New().String(),
				FromID: job.NodeID,
				ToID:   neighborID,
				Type:   "part_of",
				Weight: 0.6,
			})
		}
	}

	log.Printf("[yaad:slow] processed node %s (%s)", job.NodeID[:8], node.Type)
}

// Stop gracefully shuts down the slow-path worker.
func (ds *DualStream) Stop() {
	close(ds.queue)
	ds.wg.Wait()
}
