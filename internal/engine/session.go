package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// CompressSession creates a session summary node linking all memories from the session.
func (e *Engine) CompressSession(ctx context.Context, sessionID, project string) (*storage.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Get nodes created in this session (filtered at SQL level)
	nodes, err := e.store.ListNodes(ctx, storage.NodeFilter{
		Project:       project,
		SourceSession: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	var sessionNodes []*storage.Node
	for _, n := range nodes {
		if n.Type != "session" {
			sessionNodes = append(sessionNodes, n)
		}
	}

	if len(sessionNodes) == 0 {
		return nil, nil
	}

	// Build summary
	summary := buildSummary(sessionNodes)

	// Create session node
	sessNode := &storage.Node{
		ID:            uuid.New().String(),
		Type:          "session",
		Content:       summary,
		ContentHash:   contentHash(summary, "project", project),
		Scope:         "project",
		Project:       project,
		Tier:          3, // cold
		Confidence:    1.0,
		SourceSession: sessionID,
		Version:       1,
	}
	if err := e.graph.AddNode(ctx, sessNode); err != nil {
		return nil, err
	}

	// Link all session nodes to the session summary (best-effort)
	for _, n := range sessionNodes {
		if err := e.graph.AddEdge(ctx, &storage.Edge{
			ID:     uuid.New().String(),
			FromID: n.ID,
			ToID:   sessNode.ID,
			Type:   "learned_in",
			Weight: 1.0,
		}); err != nil {
			// Log but don't fail — the session node is already created
			continue
		}
	}

	// End the session in the sessions table (best-effort)
	_ = e.store.EndSession(ctx, sessionID, summary)

	return sessNode, nil
}

func buildSummary(nodes []*storage.Node) string {
	counts := map[string]int{}
	var samples []string
	for _, n := range nodes {
		counts[n.Type]++
		if len(samples) < 3 {
			samples = append(samples, fmt.Sprintf("[%s] %s", n.Type, truncateStr(n.Content, 60)))
		}
	}
	parts := []string{fmt.Sprintf("Session: %d memories created", len(nodes))}
	for typ, cnt := range counts {
		parts = append(parts, fmt.Sprintf("%d %s(s)", cnt, typ))
	}
	parts = append(parts, "")
	parts = append(parts, strings.Join(samples, "; "))
	return strings.Join(parts, ", ")
}

func truncateStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// StartSession creates a new session record and returns its ID.
func (e *Engine) StartSession(ctx context.Context, project, agent string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	id := uuid.New().String()
	sess := &storage.Session{
		ID:        id,
		Project:   project,
		Agent:     agent,
		StartedAt: time.Now(),
	}
	if err := e.store.CreateSession(ctx, sess); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}
