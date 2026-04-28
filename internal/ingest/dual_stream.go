// Package ingest implements dual-stream memory ingestion.
// Based on MAGMA (arxiv:2601.03236) and GAM (arxiv:2604.12285).
//
// Fast path (sync): non-blocking — store node + temporal edge, return immediately.
// Slow path (async): background goroutine — causal inference, entity linking, consolidation.
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
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
	// Optional: LLM API key for causal inference (MAGMA slow path)
	// If empty, falls back to heuristic causal inference
	LLMAPIKey  string
	LLMBaseURL string
	LLMModel   string
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
// - Infer causal edges (LLM if configured, heuristic otherwise)
// - Link entity nodes
func (ds *DualStream) slowPath(job SlowPathJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	node, err := job.Eng.Store().GetNode(job.NodeID)
	if err != nil {
		return
	}

	// Get local neighborhood (2 hops)
	neighbors, err := job.Graph.BFS(job.NodeID, 2)
	if err != nil || len(neighbors) < 2 {
		return
	}

	// LLM-based causal inference (MAGMA slow path)
	// If LLM configured, ask it to infer causal relationships
	if ds.LLMAPIKey != "" {
		ds.llmCausalInference(ctx, job, node, neighbors)
	} else {
		// Heuristic fallback
		ds.heuristicCausalInference(job, node, neighbors)
	}

	log.Printf("[yaad:slow] processed node %s (%s)", job.NodeID[:8], node.Type)
}

// llmCausalInference uses an LLM to infer causal edges (MAGMA-style).
func (ds *DualStream) llmCausalInference(ctx context.Context, job SlowPathJob, node *storage.Node, neighborIDs []string) {
	// Build neighborhood context
	var neighborContents []string
	var neighborMap = map[string]*storage.Node{}
	for _, id := range neighborIDs {
		if id == job.NodeID {
			continue
		}
		n, err := job.Eng.Store().GetNode(id)
		if err == nil {
			neighborContents = append(neighborContents, fmt.Sprintf("[%s] %s", n.Type, n.Content))
			neighborMap[id] = n
		}
	}
	if len(neighborContents) == 0 {
		return
	}

	prompt := fmt.Sprintf(`Given this new memory:
[%s] %s

And these related memories:
%s

Identify causal relationships. For each relationship, output JSON:
{"from_id": "...", "to_id": "...", "type": "led_to|caused_by|part_of"}
Only output relationships you are confident about. Output empty array [] if none.`,
		node.Type, node.Content,
		strings.Join(neighborContents[:min3(len(neighborContents), 5)], "\n"))

	body, _ := json.Marshal(map[string]any{
		"model": ds.LLMModel,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  200,
		"temperature": 0,
	})

	baseURL := ds.LLMBaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		ds.heuristicCausalInference(job, node, neighborIDs)
		return
	}
	req.Header.Set("Authorization", "Bearer "+ds.LLMAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ds.heuristicCausalInference(job, node, neighborIDs)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Choices) == 0 {
		ds.heuristicCausalInference(job, node, neighborIDs)
		return
	}

	// Parse edges from LLM response
	raw := result.Choices[0].Message.Content
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < 0 {
		return
	}
	var edges []struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &edges); err != nil {
		return
	}
	for _, e := range edges {
		// Validate IDs exist in neighborhood
		if _, ok := neighborMap[e.FromID]; !ok && e.FromID != job.NodeID {
			continue
		}
		_ = job.Graph.AddEdge(&storage.Edge{
			ID:     uuid.New().String(),
			FromID: e.FromID,
			ToID:   e.ToID,
			Type:   e.Type,
			Weight: 0.9, // LLM-inferred, high confidence
		})
	}
}

// heuristicCausalInference uses rule-based causal inference (no LLM).
func (ds *DualStream) heuristicCausalInference(job SlowPathJob, node *storage.Node, neighborIDs []string) {
	for _, neighborID := range neighborIDs {
		if neighborID == job.NodeID {
			continue
		}
		neighbor, err := job.Eng.Store().GetNode(neighborID)
		if err != nil {
			continue
		}
		// decision → led_to → convention
		if neighbor.Type == "decision" && node.Type == "convention" {
			_ = job.Graph.AddEdge(&storage.Edge{
				ID: uuid.New().String(), FromID: neighborID, ToID: job.NodeID,
				Type: "led_to", Weight: 0.8,
			})
		}
		// decision → caused_by ← bug
		if neighbor.Type == "decision" && node.Type == "bug" {
			_ = job.Graph.AddEdge(&storage.Edge{
				ID: uuid.New().String(), FromID: job.NodeID, ToID: neighborID,
				Type: "caused_by", Weight: 0.7,
			})
		}
		// convention → part_of → spec
		if neighbor.Type == "spec" && node.Type == "convention" {
			_ = job.Graph.AddEdge(&storage.Edge{
				ID: uuid.New().String(), FromID: job.NodeID, ToID: neighborID,
				Type: "part_of", Weight: 0.6,
			})
		}
	}
}

func min3(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Stop gracefully shuts down the slow-path worker.
func (ds *DualStream) Stop() {
	close(ds.queue)
	ds.wg.Wait()
}
