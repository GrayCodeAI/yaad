package engine

import (
	"strings"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

const (
	charsPerToken    = 4  // approximation: 1 token ≈ 4 characters
	nodeSizeOverhead = 50 // bytes of overhead per node (ID, type, metadata)
)

// TrimToTokenBudget trims a node list to fit within a token budget.
// Approximation: 1 token ≈ 4 characters.
func TrimToTokenBudget(nodes []*storage.Node, budget int) []*storage.Node {
	chars := budget * charsPerToken
	used := 0
	var out []*storage.Node
	for _, n := range nodes {
		size := len(n.Content) + len(n.Summary) + len(n.Tags) + nodeSizeOverhead
		if used+size > chars {
			break
		}
		out = append(out, n)
		used += size
	}
	return out
}

// FormatContext formats nodes as a markdown context block for injection.
// Uses tiered injection (Cursor-inspired):
//   - Pinned: full content always shown
//   - Hot tier (tier 1): full content
//   - Warm tier (tier 2): summary/gloss only (full content available via recall)
//   - Cold tier: omitted from context
func FormatContext(nodes []*storage.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Project Memory (Yaad)\n\n")

	// Separate pinned from unpinned
	var pinned, hot, warm []*storage.Node
	for _, n := range nodes {
		if n.Pinned {
			pinned = append(pinned, n)
		} else if n.Tier <= 1 {
			hot = append(hot, n)
		} else {
			warm = append(warm, n)
		}
	}

	if len(pinned) > 0 {
		sb.WriteString("### Core (always active)\n")
		for _, n := range pinned {
			sb.WriteString("- " + n.Content + "\n")
		}
		sb.WriteString("\n")
	}

	// Hot tier: full content, grouped by type
	byType := map[string][]*storage.Node{}
	for _, n := range hot {
		byType[n.Type] = append(byType[n.Type], n)
	}

	sections := []struct{ key, header string }{
		{"convention", "### Conventions (always follow)"},
		{"task", "### Active Tasks"},
		{"decision", "### Recent Decisions"},
		{"bug", "### Known Bug Patterns"},
		{"preference", "### Preferences"},
	}
	for _, s := range sections {
		ns := byType[s.key]
		if len(ns) == 0 {
			continue
		}
		sb.WriteString(s.header + "\n")
		for _, n := range ns {
			sb.WriteString("- " + n.Content + "\n")
		}
		sb.WriteString("\n")
	}

	// Warm tier: gloss only (summary or truncated content)
	if len(warm) > 0 {
		sb.WriteString("### Available (use recall for details)\n")
		for _, n := range warm {
			gloss := n.Summary
			if gloss == "" {
				gloss = truncateContent(n.Content, 80)
			}
			sb.WriteString("- [" + n.Type + "] " + gloss + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func truncateContent(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
