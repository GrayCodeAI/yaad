// Package profile implements auto-maintained user/project profiles.
// Inspired by SuperMemory's profile system: static facts + dynamic recent context.
//
// A profile is automatically built from the memory graph:
//   - Static: long-term facts (conventions, decisions, preferences) — stable over weeks
//   - Dynamic: recent activity (active tasks, recent bugs, last session) — changes daily
//
// One call, ~50ms. Inject into system prompt and the agent instantly knows the project.
package profile

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Profile is an auto-maintained project/user profile.
type Profile struct {
	Project string   `json:"project"`
	Static  []string `json:"static"`  // long-term facts (conventions, decisions, preferences)
	Dynamic []string `json:"dynamic"` // recent activity (tasks, bugs, last session)
	Stack   []string `json:"stack"`   // detected tech stack
	Summary string   `json:"summary"` // one-line project summary
}

// Build generates a profile from the memory graph. No LLM needed.
func Build(store storage.Storage, project string) (*Profile, error) {
	p := &Profile{Project: project}

	// Static: high-confidence conventions, decisions, preferences
	for _, typ := range []string{"convention", "decision", "preference"} {
		nodes, _ := store.ListNodes(storage.NodeFilter{
			Type: typ, Project: project, MinConfidence: 0.5,
		})
		for _, n := range nodes {
			p.Static = append(p.Static, n.Content)
		}
	}

	// Dynamic: recent tasks, bugs, sessions (last 7 days)
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, typ := range []string{"task", "bug", "session"} {
		nodes, _ := store.ListNodes(storage.NodeFilter{
			Type: typ, Project: project, MinConfidence: 0.1,
		})
		for _, n := range nodes {
			if n.CreatedAt.After(cutoff) || n.UpdatedAt.After(cutoff) {
				p.Dynamic = append(p.Dynamic, fmt.Sprintf("[%s] %s", n.Type, n.Content))
			}
		}
	}

	// Sort dynamic by recency (most recent first)
	// Already in insertion order which is roughly chronological

	// Stack: extract from entity nodes
	entities, _ := store.ListNodes(storage.NodeFilter{
		Type: "entity", Project: project,
	})
	for _, n := range entities {
		if isTech(n.Content) {
			p.Stack = append(p.Stack, n.Content)
		}
	}

	// Deduplicate stack
	p.Stack = dedup(p.Stack)

	// Summary
	parts := []string{}
	if len(p.Stack) > 0 {
		parts = append(parts, "Stack: "+strings.Join(p.Stack[:min(len(p.Stack), 5)], ", "))
	}
	parts = append(parts, fmt.Sprintf("%d facts", len(p.Static)))
	if len(p.Dynamic) > 0 {
		parts = append(parts, fmt.Sprintf("%d recent items", len(p.Dynamic)))
	}
	p.Summary = strings.Join(parts, " · ")

	return p, nil
}

// Format returns the profile as markdown for agent injection.
func (p *Profile) Format() string {
	var sb strings.Builder
	sb.WriteString("## User Profile\n\n")

	if p.Summary != "" {
		sb.WriteString("**" + p.Summary + "**\n\n")
	}

	if len(p.Static) > 0 {
		sb.WriteString("### What I Know (stable)\n")
		for _, s := range p.Static[:min(len(p.Static), 10)] {
			sb.WriteString("- " + s + "\n")
		}
		sb.WriteString("\n")
	}

	if len(p.Dynamic) > 0 {
		sb.WriteString("### What's Happening (recent)\n")
		for _, d := range p.Dynamic[:min(len(p.Dynamic), 5)] {
			sb.WriteString("- " + d + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// Merge combines two profiles (e.g., project + global).
func Merge(a, b *Profile) *Profile {
	return &Profile{
		Project: a.Project,
		Static:  dedup(append(a.Static, b.Static...)),
		Dynamic: append(a.Dynamic, b.Dynamic...),
		Stack:   dedup(append(a.Stack, b.Stack...)),
		Summary: a.Summary,
	}
}

func isTech(name string) bool {
	techs := map[string]bool{
		"typescript": true, "javascript": true, "python": true, "go": true, "rust": true,
		"react": true, "vue": true, "next": true, "node": true, "deno": true, "bun": true,
		"postgresql": true, "mysql": true, "sqlite": true, "redis": true, "nats": true,
		"docker": true, "kubernetes": true, "aws": true, "gcp": true, "azure": true,
		"jose": true, "express": true, "fastify": true, "gin": true, "fiber": true,
		"tailwind": true, "prisma": true, "drizzle": true, "trpc": true, "graphql": true,
	}
	return techs[strings.ToLower(name)]
}

func dedup(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		lower := strings.ToLower(item)
		if !seen[lower] {
			seen[lower] = true
			out = append(out, item)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ensure sort is used
var _ = sort.Strings
