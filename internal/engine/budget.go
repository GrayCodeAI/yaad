package engine

import (
	"strings"

	"github.com/yaadmemory/yaad/internal/storage"
)

// TrimToTokenBudget trims a node list to fit within a token budget.
// Approximation: 1 token ≈ 4 characters.
func TrimToTokenBudget(nodes []*storage.Node, budget int) []*storage.Node {
	chars := budget * 4
	used := 0
	var out []*storage.Node
	for _, n := range nodes {
		size := len(n.Content) + len(n.Summary) + len(n.Tags) + 50 // overhead
		if used+size > chars {
			break
		}
		out = append(out, n)
		used += size
	}
	return out
}

// FormatContext formats nodes as a markdown context block for injection.
func FormatContext(nodes []*storage.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Project Memory (Yaad)\n\n")

	byType := map[string][]*storage.Node{}
	for _, n := range nodes {
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
	return sb.String()
}
