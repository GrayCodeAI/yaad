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
		_ = e.store.SaveVersion(ctx, node.ID, node.Content, "user", "edited via feedback")
		node.Content = newContent
		node.ContentHash = contentHash(newContent, node.Scope, node.Project)
		node.Version++
		node.UpdatedAt = time.Now()
		node.Confidence = 1.0
		return e.store.UpdateNode(ctx, node)

	case FeedbackDiscard:
		_ = e.store.SaveVersion(ctx, node.ID, node.Content, "user", "discarded via feedback")
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
			_ = e.store.SaveVersion(ctx, node.ID, node.Content, "system", fmt.Sprintf("rollback to v%d", version))
			node.Content = v.Content
			node.Version++
			node.UpdatedAt = time.Now()
			return e.store.UpdateNode(ctx, node)
		}
	}
	return fmt.Errorf("version %d not found for node %s", version, id)
}

// PendingNodes returns low-confidence nodes that may need review.
func (e *Engine) PendingNodes(ctx context.Context, project string, threshold float64) ([]*storage.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return nil, err
	}
	var pending []*storage.Node
	for _, n := range nodes {
		if n.Confidence > 0 && n.Confidence < threshold {
			pending = append(pending, n)
		}
	}
	return pending, nil
}
