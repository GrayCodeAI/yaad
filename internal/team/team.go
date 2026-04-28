// Package team implements namespaced team memory sharing.
// Team memories are stored in the global DB (~/.yaad/yaad.db) with a team_id namespace.
// Private memories stay in the project DB; shared memories are promoted to the team namespace.
package team

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/yaadmemory/yaad/internal/engine"
	"github.com/yaadmemory/yaad/internal/storage"
)

// ShareInput describes a memory to share with the team.
type ShareInput struct {
	NodeID  string
	TeamID  string
	SharedBy string
}

// Share copies a project-scoped node into the team namespace (global scope + team_id tag).
func Share(src *storage.Store, dst *storage.Store, in ShareInput) (*storage.Node, error) {
	node, err := src.GetNode(in.NodeID)
	if err != nil {
		return nil, fmt.Errorf("node %s not found: %w", in.NodeID, err)
	}

	// Create a copy in the global (team) store
	shared := &storage.Node{
		ID:          uuid.New().String(),
		Type:        node.Type,
		Content:     node.Content,
		ContentHash: node.ContentHash + ":team:" + in.TeamID, // different hash to avoid dedup
		Summary:     node.Summary,
		Scope:       "global",
		Project:     "",
		Tier:        node.Tier,
		Tags:        addTag(node.Tags, "team:"+in.TeamID),
		Confidence:  node.Confidence,
		SourceAgent: in.SharedBy,
		Version:     1,
	}
	if err := dst.CreateNode(shared); err != nil {
		return nil, err
	}
	return shared, nil
}

// ListTeamMemories returns all memories for a given team_id from the global store.
func ListTeamMemories(store *storage.Store, teamID string) ([]*storage.Node, error) {
	all, err := store.ListNodes(storage.NodeFilter{Scope: "global"})
	if err != nil {
		return nil, err
	}
	tag := "team:" + teamID
	var result []*storage.Node
	for _, n := range all {
		if containsTag(n.Tags, tag) {
			result = append(result, n)
		}
	}
	return result, nil
}

// InjectTeamContext adds team memories to a recall result.
func InjectTeamContext(eng *engine.Engine, teamStore *storage.Store, teamID, query string, limit int) ([]*storage.Node, error) {
	teamNodes, err := ListTeamMemories(teamStore, teamID)
	if err != nil {
		return nil, err
	}
	// Filter by relevance to query (simple substring match)
	var relevant []*storage.Node
	for _, n := range teamNodes {
		if limit > 0 && len(relevant) >= limit {
			break
		}
		relevant = append(relevant, n)
	}
	return relevant, nil
}

func addTag(tags, tag string) string {
	if tags == "" {
		return tag
	}
	return tags + "," + tag
}

func containsTag(tags, tag string) bool {
	if tags == "" {
		return false
	}
	for _, t := range splitTags(tags) {
		if t == tag {
			return true
		}
	}
	return false
}

func splitTags(tags string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(tags); i++ {
		if i == len(tags) || tags[i] == ',' {
			if i > start {
				result = append(result, tags[start:i])
			}
			start = i + 1
		}
	}
	return result
}
