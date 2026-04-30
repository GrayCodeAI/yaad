package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

// FeedbackAction represents what to do with a pending memory.
type FeedbackAction string

const (
	FeedbackApprove FeedbackAction = "approve"
	FeedbackEdit    FeedbackAction = "edit"
	FeedbackDiscard FeedbackAction = "discard"
)

// Feedback applies user feedback to a memory node.
func (e *Engine) Feedback(ctx context.Context, id string, action FeedbackAction, newContent string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	node, err := e.store.GetNode(ctx, id)
	if err != nil {
		return fmt.Errorf("node %s not found: %w", id, err)
	}

	switch action {
	case FeedbackApprove:
		// Boost confidence on explicit approval
		node.Confidence = 1.0
		node.AccessedAt = time.Now()
		return e.store.UpdateNode(ctx, node)

	case FeedbackEdit:
		if newContent == "" {
			return fmt.Errorf("edit requires new content")
		}
		if len(newContent) > maxContentLength {
			return fmt.Errorf("content exceeds max length of %d characters", maxContentLength)
		}
		if err := e.store.SaveVersion(ctx, node.ID, node.Content, "user", "edited via feedback"); err != nil {
			return fmt.Errorf("save version: %w", err)
		}
		node.Content = newContent
		node.ContentHash = contentHash(newContent, node.Scope, node.Project)
		node.Version++
		node.UpdatedAt = time.Now()
		node.Confidence = 1.0
		return e.store.UpdateNode(ctx, node)

	case FeedbackDiscard:
		if err := e.store.SaveVersion(ctx, node.ID, node.Content, "user", "discarded via feedback"); err != nil {
			return fmt.Errorf("save version: %w", err)
		}
		node.Confidence = 0
		return e.store.UpdateNode(ctx, node)

	default:
		return fmt.Errorf("unknown feedback action: %s", action)
	}
}

// Rollback restores a node to a previous version.
func (e *Engine) Rollback(ctx context.Context, id string, version int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	versions, err := e.store.GetVersions(ctx, id)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if v.Version == version {
			node, err := e.store.GetNode(ctx, id)
			if err != nil {
				return err
			}
			if err := e.store.SaveVersion(ctx, node.ID, node.Content, "system", fmt.Sprintf("rollback to v%d", version)); err != nil {
				return fmt.Errorf("save version: %w", err)
			}
			node.Content = v.Content
			node.Version++
			node.UpdatedAt = time.Now()
			return e.store.UpdateNode(ctx, node)
		}
	}
	return fmt.Errorf("version %d not found for node %s", version, id)
}

// PendingNodes returns low-confidence nodes that may need review.
// Limited to 1000 nodes to prevent unbounded memory use on large graphs.
func (e *Engine) PendingNodes(ctx context.Context, project string, threshold float64) ([]*storage.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodes(ctx, storage.NodeFilter{Project: project, MinConfidence: 0.01})
	if err != nil {
		return nil, err
	}
	var pending []*storage.Node
	for _, n := range nodes {
		if n.Confidence > 0 && n.Confidence < threshold {
			pending = append(pending, n)
			if len(pending) >= 1000 {
				break
			}
		}
	}
	return pending, nil
}
