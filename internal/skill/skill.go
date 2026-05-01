// Package skill implements procedural memory — reusable step sequences.
// A skill is a named sequence of steps that can be stored, retrieved, and replayed.
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Step is a single step in a skill.
type Step struct {
	Order       int    `json:"order"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"` // optional shell command
	Tool        string `json:"tool,omitempty"`    // optional MCP tool name
}

// Skill is a named procedural memory with ordered steps.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
	Tags        string `json:"tags,omitempty"`
}

// Store saves a skill as a node in the memory graph.
func Store(ctx context.Context, eng *engine.Engine, s *Skill, project string) (*storage.Node, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return eng.Remember(ctx, engine.RememberInput{
		Type:    "skill",
		Content: fmt.Sprintf("Skill: %s\n%s", s.Name, string(b)),
		Summary: s.Description,
		Tags:    addTag(s.Tags, "skill:"+s.Name),
		Scope:   "project",
		Project: project,
	})
}

// Load retrieves a skill by name from the memory graph.
func Load(ctx context.Context, store storage.Storage, name, project string) (*Skill, error) {
	nodes, err := store.SearchNodes(ctx, "Skill: "+name, 5)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.Type == "skill" && n.Project == project {
			return parseSkill(n.Content)
		}
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

// ListSkills returns all skill nodes for a project.
func ListSkills(ctx context.Context, store storage.Storage, project string) ([]*Skill, error) {
	nodes, err := store.ListNodes(ctx, storage.NodeFilter{Type: "skill", Project: project})
	if err != nil {
		return nil, err
	}
	var skills []*Skill
	for _, n := range nodes {
		if s, err := parseSkill(n.Content); err == nil {
			skills = append(skills, s)
		}
	}
	return skills, nil
}

// Replay returns the steps of a skill as a formatted string for agent injection.
func Replay(s *Skill) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Skill: %s\n%s\n\n", s.Name, s.Description))
	for _, step := range s.Steps {
		sb.WriteString(fmt.Sprintf("%d. %s", step.Order, step.Description))
		if step.Command != "" {
			sb.WriteString(fmt.Sprintf("\n   ```\n   %s\n   ```", step.Command))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func parseSkill(content string) (*Skill, error) {
	// Content format: "Skill: <name>\n<json>"
	idx := strings.Index(content, "\n")
	if idx < 0 {
		return nil, fmt.Errorf("invalid skill content")
	}
	var s Skill
	if err := json.Unmarshal([]byte(content[idx+1:]), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func addTag(tags, tag string) string {
	if tags == "" {
		return tag
	}
	return tags + "," + tag
}
