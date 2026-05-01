// Package exportimport handles JSON round-trip, Markdown, and Obsidian vault export.
package exportimport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GrayCodeAI/yaad/storage"
)

// GraphExport is the full graph export format.
type GraphExport struct {
	Version   string          `json:"version"`
	ExportedAt time.Time      `json:"exported_at"`
	Nodes     []*storage.Node `json:"nodes"`
	Edges     []*storage.Edge `json:"edges"`
}

// ExportJSON exports the full graph as JSON.
func ExportJSON(ctx context.Context, store storage.Storage, project string) ([]byte, error) {
	nodes, err := store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return nil, err
	}
	// Batch-fetch all edges between project nodes (single query instead of N+1)
	nodeIDs := make([]string, len(nodes))
	for i, n := range nodes {
		nodeIDs[i] = n.ID
	}
	edges, _ := store.GetEdgesBetween(ctx, nodeIDs)
	return json.MarshalIndent(GraphExport{
		Version:    "1.0",
		ExportedAt: time.Now(),
		Nodes:      nodes,
		Edges:      edges,
	}, "", "  ")
}

// ImportJSON imports a graph from JSON, skipping duplicates.
func ImportJSON(ctx context.Context, store storage.Storage, data []byte) (int, int, error) {
	var exp GraphExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return 0, 0, err
	}
	nodes, edges := 0, 0
	for _, n := range exp.Nodes {
		if err := store.CreateNode(ctx, n); err == nil {
			nodes++
		}
	}
	for _, e := range exp.Edges {
		if err := store.CreateEdge(ctx, e); err == nil {
			edges++
		}
	}
	return nodes, edges, nil
}

// ExportMarkdown exports nodes as a Markdown document.
func ExportMarkdown(ctx context.Context, store storage.Storage, project string) (string, error) {
	nodes, err := store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("# Yaad Memory Export\n\n")
	sb.WriteString(fmt.Sprintf("Project: `%s`  \nExported: %s\n\n", project, time.Now().Format("2006-01-02")))

	byType := map[string][]*storage.Node{}
	for _, n := range nodes {
		byType[n.Type] = append(byType[n.Type], n)
	}

	sections := []struct{ key, header string }{
		{"convention", "## Conventions"},
		{"decision", "## Architecture Decisions"},
		{"bug", "## Bug Patterns"},
		{"spec", "## Subsystem Specs"},
		{"task", "## Tasks"},
		{"skill", "## Skills"},
		{"preference", "## Preferences"},
	}
	for _, s := range sections {
		ns := byType[s.key]
		if len(ns) == 0 {
			continue
		}
		sb.WriteString(s.header + "\n\n")
		for _, n := range ns {
			sb.WriteString(fmt.Sprintf("- **[%.0f%%]** %s\n", n.Confidence*100, n.Content))
			if n.Tags != "" {
				sb.WriteString(fmt.Sprintf("  *tags: %s*\n", n.Tags))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// ExportObsidian exports the graph as an Obsidian vault (one .md file per node, wikilinks for edges).
// vaultDir must be an absolute path and must not escape above itself via ".." components.
func ExportObsidian(ctx context.Context, store storage.Storage, project, vaultDir string) (int, error) {
	cleaned := filepath.Clean(vaultDir)
	if !filepath.IsAbs(cleaned) {
		return 0, fmt.Errorf("vault_dir must be an absolute path, got: %q", vaultDir)
	}
	if strings.Contains(cleaned, "..") {
		return 0, fmt.Errorf("vault_dir must not contain path traversal components")
	}
	if err := os.MkdirAll(cleaned, 0755); err != nil {
		return 0, fmt.Errorf("create vault dir: %w", err)
	}
	vaultDir = cleaned

	nodes, err := store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return 0, err
	}

	// Build ID→title map
	titles := map[string]string{}
	for _, n := range nodes {
		titles[n.ID] = obsidianTitle(n)
	}

	written := 0
	for _, n := range nodes {
		edges, _ := store.GetEdgesFrom(ctx, n.ID)
		var sb strings.Builder

		// Frontmatter
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("type: %s\n", n.Type))
		sb.WriteString(fmt.Sprintf("confidence: %.2f\n", n.Confidence))
		if n.Tags != "" {
			sb.WriteString(fmt.Sprintf("tags: [%s]\n", n.Tags))
		}
		sb.WriteString("---\n\n")

		// Content
		sb.WriteString(n.Content + "\n\n")

		// Links
		if len(edges) > 0 {
			sb.WriteString("## Links\n\n")
			for _, e := range edges {
				if title, ok := titles[e.ToID]; ok {
					sb.WriteString(fmt.Sprintf("- **%s** → [[%s]]\n", e.Type, title))
				}
			}
		}

		fname := sanitizeFilename(obsidianTitle(n)) + ".md"
		if err := os.WriteFile(filepath.Join(vaultDir, fname), []byte(sb.String()), 0644); err == nil {
			written++
		}
	}
	return written, nil
}

func obsidianTitle(n *storage.Node) string {
	content := n.Content
	if len(content) > 50 {
		content = content[:50]
	}
	return fmt.Sprintf("[%s] %s", n.Type, content)
}

func sanitizeFilename(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			sb.WriteRune('_')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
