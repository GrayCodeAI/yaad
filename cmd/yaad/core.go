package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/engine"
	"github.com/GrayCodeAI/yaad/graph"
	"github.com/GrayCodeAI/yaad/storage"
	"github.com/GrayCodeAI/yaad/utils"
	"github.com/GrayCodeAI/yaad/internal/version"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize .yaad/ in current project",
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		yaadDir := filepath.Join(dir, ".yaad")
		os.MkdirAll(yaadDir, 0755)
		store, err := storage.NewStore(filepath.Join(yaadDir, "yaad.db"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		store.Close()

		// Write default config if it doesn't exist
		configPath := filepath.Join(yaadDir, "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			os.WriteFile(configPath, []byte(defaultConfigTOML), 0644)
		}

		// Append .yaad/ to .gitignore if not already present
		ensureGitignore(dir)

		fmt.Printf("✓ Initialized .yaad/ in %s\n", dir)
		fmt.Println("  Next: run 'yaad setup' to configure Hawk")
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure Hawk to use Yaad as its memory layer",
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()

		// Write .mcp.json for Hawk
		mcpPath := filepath.Join(dir, ".mcp.json")
		if _, err := os.Stat(mcpPath); err == nil {
			fmt.Println("  .mcp.json already exists (skipped)")
		} else {
			content := fmt.Sprintf(`{
  "mcpServers": {
    "yaad": {
      "command": "yaad",
      "args": ["mcp"],
      "cwd": %q
    }
  }
}
`, dir)
			if err := os.WriteFile(mcpPath, []byte(content), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓ Created .mcp.json (Hawk MCP config)")
		}

		// Write hooks config for Hawk auto-capture
		hooksDir := filepath.Join(dir, ".hawk")
		os.MkdirAll(hooksDir, 0755)
		hooksPath := filepath.Join(hooksDir, "hooks.json")
		if _, err := os.Stat(hooksPath); err == nil {
			fmt.Println("  .hawk/hooks.json already exists (skipped)")
		} else {
			hooksContent := `{
  "hooks": {
    "session-start": "yaad hook session-start",
    "post-tool-use": "yaad hook post-tool-use",
    "session-end": "yaad hook session-end"
  }
}
`
			if err := os.WriteFile(hooksPath, []byte(hooksContent), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓ Created .hawk/hooks.json (auto-capture hooks)")
		}

		// Write system prompt for Hawk
		systemPath := filepath.Join(hooksDir, "system.md")
		if _, err := os.Stat(systemPath); err == nil {
			fmt.Println("  .hawk/system.md already exists (skipped)")
		} else {
			if err := os.WriteFile(systemPath, []byte(hawkSystemPrompt), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓ Created .hawk/system.md (Hawk memory instructions)")
		}

		fmt.Println("\nYaad is ready for Hawk. Memory will auto-capture during sessions.")
	},
}

const hawkSystemPrompt = `# Yaad Memory System

You have access to a persistent memory graph via yaad MCP tools. Use it to maintain continuity across sessions.

## When to recall

- **Session start**: Call yaad_session_recap to see what happened last session.
- **Before implementation**: Call yaad_recall with relevant keywords to check for existing conventions, past decisions, or known bugs.
- **When asked "why"**: Call yaad_recall to find the decision history.
- **Before changing patterns**: Check yaad_mental_model to understand current project conventions.

## When to remember

- **After making a decision**: yaad_remember with type "decision" — capture the WHY, not just the what.
- **When establishing a pattern**: yaad_remember with type "convention" — future sessions need to follow it.
- **When finding a bug**: yaad_remember with type "bug" — include root cause and fix.
- **When learning user preferences**: yaad_remember with type "preference".
- **Multi-step procedures**: yaad_skill_store to save repeatable workflows.

## When to link

- **Cause/effect**: yaad_link with type "caused_by" or "led_to" when one decision leads to another.
- **Contradiction**: yaad_link with type "supersedes" when a new convention replaces an old one.
- **Dependency**: yaad_link with type "depends_on" for task dependencies.

## When to pin

- **Critical facts**: yaad_pin nodes that must ALWAYS appear in context (API keys location, deploy process, core architecture decisions).

## When to forget

- **Outdated info**: yaad_feedback with action "discard" for stale memories.
- **Corrections**: yaad_feedback with action "edit" to fix wrong memories.

## What NOT to remember

- Ephemeral file contents (they change constantly)
- Obvious things derivable from code (function signatures, imports)
- Conversation-specific context that won't matter next session

## Tips

- Keep memory content concise (1-2 sentences). The WHY is more valuable than the WHAT.
- Use yaad_proactive at session start for predicted relevant context.
- Use yaad_stale periodically to find memories that may be outdated.
- Use yaad_compact when the graph grows large.
`

func ensureGitignore(dir string) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	content, _ := os.ReadFile(gitignorePath)
	if strings.Contains(string(content), ".yaad/") {
		return
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		f.WriteString("\n")
	}
	f.WriteString("\n# Yaad memory (local-only)\n.yaad/\n")
}

const defaultConfigTOML = `# Yaad configuration
# See: https://github.com/GrayCodeAI/yaad

[server]
port = 3456
host = "127.0.0.1"

[memory]
hot_token_budget = 800
warm_token_budget = 800
max_memories = 10000

[search]
bm25_weight = 0.5
vector_weight = 0.5
default_limit = 10

[decay]
enabled = true
half_life_days = 30
min_confidence = 0.1
boost_on_access = 0.2

[git]
watch = true
auto_stale = true
`

var rememberCmd = &cobra.Command{
	Use:   "remember [content]",
	Short: "Store a memory node",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		typ, _ := cmd.Flags().GetString("type")
		tags, _ := cmd.Flags().GetString("tags")
		node, err := eng.Remember(context.Background(), engine.RememberInput{
			Content: strings.Join(args, " "),
			Type:    typ,
			Tags:    tags,
			Scope:   "project",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Remembered [%s] %s (id: %s)\n", node.Type, truncate(node.Content, 60), utils.ShortID(node.ID))
	},
}

var recallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Search memories (graph-aware)",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		page, _ := cmd.Flags().GetInt("page")
		result, err := eng.Recall(context.Background(), engine.RecallOpts{
			Query: strings.Join(args, " "),
			Depth: depth,
			Limit: limit * page,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		start := (page - 1) * limit
		end := start + limit
		nodes := result.Nodes
		if start >= len(nodes) {
			fmt.Printf("No results on page %d.\n", page)
			return
		}
		if end > len(nodes) {
			end = len(nodes)
		}
		nodes = nodes[start:end]
		if len(nodes) == 0 {
			fmt.Println("No memories found.")
			return
		}
		fmt.Printf("Page %d (%d-%d of %d results)\n", page, start+1, end, len(result.Nodes))
		for _, n := range nodes {
			fmt.Printf("[%s] %s (confidence: %.2f, id: %s)\n", n.Type, truncate(n.Content, 70), n.Confidence, utils.ShortID(n.ID))
		}
		if len(result.Edges) > 0 {
			fmt.Printf("\n%d relationships found\n", len(result.Edges))
		}
	},
}

var linkCmd = &cobra.Command{
	Use:   "link [from_id] [to_id] [type]",
	Short: "Create edge between two nodes",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		edgeType := args[2]
		if !graph.IsValidEdgeType(edgeType) {
			fmt.Fprintf(os.Stderr, "error: invalid edge type: %q\n", edgeType)
			os.Exit(1)
		}
		edge := &storage.Edge{
			ID:     fmt.Sprintf("%s-%s-%s", utils.ShortID(args[0]), utils.ShortID(args[1]), edgeType),
			FromID: args[0],
			ToID:   args[1],
			Type:   edgeType,
			Weight: 1.0,
		}
		if err := eng.Graph().AddEdge(context.Background(), edge); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Linked %s →[%s]→ %s\n", utils.ShortID(args[0]), edgeType, utils.ShortID(args[1]))
	},
}

var subgraphCmd = &cobra.Command{
	Use:   "subgraph [id]",
	Short: "Show subgraph around a node",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		depth, _ := cmd.Flags().GetInt("depth")
		if depth <= 0 || depth > 5 {
			depth = 2
		}
		sg, err := eng.Graph().ExtractSubgraph(context.Background(), args[0], depth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Subgraph: %d nodes, %d edges\n", len(sg.Nodes), len(sg.Edges))
		for _, n := range sg.Nodes {
			fmt.Printf("  [%s] %s\n", n.Type, truncate(n.Content, 60))
		}
		for _, e := range sg.Edges {
			fmt.Printf("  %s →[%s]→ %s\n", utils.ShortID(e.FromID), e.Type, utils.ShortID(e.ToID))
		}
	},
}

var impactCmd = &cobra.Command{
	Use:   "impact [file]",
	Short: "What memories are affected by this file?",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		ids, err := eng.Graph().Impact(context.Background(), args[0], 5)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(ids) == 0 {
			fmt.Println("No memories linked to this file.")
			return
		}
		fmt.Printf("%d memories affected by %s:\n", len(ids), args[0])
		for _, id := range ids {
			if n, err := eng.Store().GetNode(context.Background(), id); err == nil {
				fmt.Printf("  [%s] %s\n", n.Type, truncate(n.Content, 60))
			}
		}
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show graph stats",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		st, err := eng.Status(context.Background(), "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("yaad v%s\n", version.String())
		fmt.Printf("  Nodes:    %d\n", st.Nodes)
		fmt.Printf("  Edges:    %d\n", st.Edges)
		fmt.Printf("  Sessions: %d\n", st.Sessions)
		fmt.Printf("  DB:       %s\n", dbPath())
	},
}
