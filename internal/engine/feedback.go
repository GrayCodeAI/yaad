package engine

import (
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
func (e *Engine) Feedback(id string, action FeedbackAction, newContent string) error {
	node, err := e.store.GetNode(id)
	if err != nil {
		return fmt.Errorf("node %s not found: %w", id, err)
	}

	switch action {
	case FeedbackApprove:
		// Boost confidence on explicit approval
		node.Confidence = 1.0
		node.AccessedAt = time.Now()
		return e.store.UpdateNode(node)

	case FeedbackEdit:
		if newContent == "" {
			return fmt.Errorf("edit requires new content")
		}
		_ = e.store.SaveVersion(node.ID, node.Content, "user", "edited via feedback")
		node.Content = newContent
		node.ContentHash = contentHash(newContent, node.Scope, node.Project)
		node.Version++
		node.UpdatedAt = time.Now()
		node.Confidence = 1.0
		return e.store.UpdateNode(node)

	case FeedbackDiscard:
		_ = e.store.SaveVersion(node.ID, node.Content, "user", "discarded via feedback")
		node.Confidence = 0
		return e.store.UpdateNode(node)

	default:
		return fmt.Errorf("unknown feedback action: %s", action)
	}
}

// Rollback restores a node to a previous version.
func (e *Engine) Rollback(id string, version int) error {
	versions, err := e.store.GetVersions(id)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if v.Version == version {
			node, err := e.store.GetNode(id)
			if err != nil {
				return err
			}
			_ = e.store.SaveVersion(node.ID, node.Content, "system", fmt.Sprintf("rollback to v%d", version))
			node.Content = v.Content
			node.Version++
			node.UpdatedAt = time.Now()
			return e.store.UpdateNode(node)
		}
	}
	return fmt.Errorf("version %d not found for node %s", version, id)
}

// PendingNodes returns low-confidence nodes that may need review.
func (e *Engine) PendingNodes(project string, threshold float64) ([]*storage.Node, error) {
	nodes, err := e.store.ListNodes(storage.NodeFilter{Project: project})
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
