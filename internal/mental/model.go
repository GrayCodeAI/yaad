// Package mental implements auto-generated project mental models.
// A mental model is a high-level summary of the project that evolves
// as memories grow. Based on Hindsight's mental models concept.
//
// Example mental model:
//   "This project is a TypeScript API using jose for JWT auth (RS256),
//    NATS for event bus, PostgreSQL with pgbouncer. Key conventions:
//    named exports only, pnpm test before commit. Active work: rate limiting."
package mental

import (
	"fmt"
	"strings"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Model is an auto-generated project summary.
type Model struct {
	Project      string   `json:"project"`
	Summary      string   `json:"summary"`
	Stack        []string `json:"stack"`
	Conventions  []string `json:"conventions"`
	ActiveTasks  []string `json:"active_tasks"`
	KnownBugs    []string `json:"known_bugs"`
	KeyDecisions []string `json:"key_decisions"`
}

// Generate creates a mental model from the current memory graph.
// No LLM needed — built from high-confidence nodes.
func Generate(store storage.Storage, project string) (*Model, error) {
	m := &Model{Project: project}

	// Conventions (hot tier, high confidence)
	conventions, _ := store.ListNodes(storage.NodeFilter{
		Type: "convention", Project: project, Tier: 1, MinConfidence: 0.5,
	})
	for _, n := range conventions {
		m.Conventions = append(m.Conventions, n.Content)
	}

	// Key decisions
	decisions, _ := store.ListNodes(storage.NodeFilter{
		Type: "decision", Project: project, MinConfidence: 0.5,
	})
	for _, n := range decisions {
		m.KeyDecisions = append(m.KeyDecisions, n.Content)
	}

	// Active tasks
	tasks, _ := store.ListNodes(storage.NodeFilter{
		Type: "task", Project: project, MinConfidence: 0.3,
	})
	for _, n := range tasks {
		m.ActiveTasks = append(m.ActiveTasks, n.Content)
	}

	// Known bugs
	bugs, _ := store.ListNodes(storage.NodeFilter{
		Type: "bug", Project: project, MinConfidence: 0.5,
	})
	for _, n := range bugs {
		m.KnownBugs = append(m.KnownBugs, n.Content)
	}

	// Extract stack from entities
	entities, _ := store.ListNodes(storage.NodeFilter{
		Type: "entity", Project: project,
	})
	for _, n := range entities {
		if isStackTech(n.Content) {
			m.Stack = append(m.Stack, n.Content)
		}
	}

	// Build summary
	m.Summary = buildSummary(m)
	return m, nil
}

// Format returns the mental model as markdown for agent injection.
func (m *Model) Format() string {
	var sb strings.Builder
	sb.WriteString("## Project Mental Model\n\n")
	if m.Summary != "" {
		sb.WriteString(m.Summary + "\n\n")
	}
	if len(m.Stack) > 0 {
		sb.WriteString("**Stack**: " + strings.Join(m.Stack, ", ") + "\n\n")
	}
	if len(m.Conventions) > 0 {
		sb.WriteString("**Conventions**:\n")
		for _, c := range m.Conventions[:min(len(m.Conventions), 5)] {
			sb.WriteString("- " + c + "\n")
		}
		sb.WriteString("\n")
	}
	if len(m.ActiveTasks) > 0 {
		sb.WriteString("**Active Tasks**:\n")
		for _, t := range m.ActiveTasks[:min(len(m.ActiveTasks), 5)] {
			sb.WriteString("- " + t + "\n")
		}
	}
	return sb.String()
}

func buildSummary(m *Model) string {
	parts := []string{}
	if len(m.Stack) > 0 {
		parts = append(parts, "Stack: "+strings.Join(m.Stack[:min(len(m.Stack), 5)], ", "))
	}
	if len(m.Conventions) > 0 {
		parts = append(parts, fmt.Sprintf("%d conventions", len(m.Conventions)))
	}
	if len(m.KeyDecisions) > 0 {
		parts = append(parts, fmt.Sprintf("%d key decisions", len(m.KeyDecisions)))
	}
	if len(m.ActiveTasks) > 0 {
		parts = append(parts, fmt.Sprintf("%d active tasks", len(m.ActiveTasks)))
	}
	if len(parts) == 0 {
		return "No memories yet."
	}
	return strings.Join(parts, ". ") + "."
}

func isStackTech(name string) bool {
	techs := map[string]bool{
		"typescript": true, "javascript": true, "python": true, "go": true, "rust": true,
		"react": true, "vue": true, "next": true, "node": true, "deno": true, "bun": true,
		"postgresql": true, "mysql": true, "sqlite": true, "redis": true, "nats": true,
		"docker": true, "kubernetes": true, "aws": true, "gcp": true, "azure": true,
		"jose": true, "express": true, "fastify": true, "gin": true, "fiber": true,
	}
	return techs[strings.ToLower(name)]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
