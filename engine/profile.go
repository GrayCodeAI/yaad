package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GrayCodeAI/yaad/storage"
)

// UserProfile holds user preferences and observed patterns for a project.
type UserProfile struct {
	Preferences map[string]string // e.g., "indent": "tabs", "test_framework": "testify"
	Patterns    []string          // observed patterns: "always runs tests after edit"
	UpdatedAt   time.Time
}

// GetUserProfile returns the user profile for a project, built from preference nodes.
func (e *Engine) GetUserProfile(ctx context.Context, project string) (*UserProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodes(ctx, storage.NodeFilter{
		Type: "preference", Project: project,
	})
	if err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}
	p := &UserProfile{Preferences: make(map[string]string)}
	for _, n := range nodes {
		key, val := parsePreference(n.Tags, n.Content)
		if key != "" {
			p.Preferences[key] = val
			if n.UpdatedAt.After(p.UpdatedAt) {
				p.UpdatedAt = n.UpdatedAt
			}
		}
		if strings.HasPrefix(key, "pattern:") {
			p.Patterns = append(p.Patterns, val)
		}
	}
	return p, nil
}

// UpdateUserPreference stores or updates a single preference as a yaad node.
func (e *Engine) UpdateUserPreference(ctx context.Context, project, key, value string) error {
	if key == "" {
		return fmt.Errorf("preference key cannot be empty")
	}
	_, err := e.Remember(ctx, RememberInput{
		Type:     "preference",
		Content:  value,
		Summary:  key + ": " + value,
		Scope:    "project",
		Project:  project,
		TopicKey: key,
		Tags:     "topic:" + key,
	})
	return err
}

// parsePreference extracts the key from a topic tag and returns (key, content).
func parsePreference(tags, content string) (string, string) {
	for _, t := range strings.Split(tags, ",") {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, "topic:") {
			return strings.TrimPrefix(t, "topic:"), content
		}
	}
	return "", content
}
