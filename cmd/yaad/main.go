package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/agentconfig"
	"github.com/GrayCodeAI/yaad/internal/bench"
	"github.com/GrayCodeAI/yaad/internal/bridge"
	"github.com/GrayCodeAI/yaad/internal/embeddings"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/exportimport"
	"github.com/GrayCodeAI/yaad/internal/hooks"
	"github.com/GrayCodeAI/yaad/internal/server"
	"github.com/GrayCodeAI/yaad/internal/skill"
	"github.com/GrayCodeAI/yaad/internal/storage"
	yaadsync "github.com/GrayCodeAI/yaad/internal/sync"
	"github.com/GrayCodeAI/yaad/internal/tui"
	intentpkg "github.com/GrayCodeAI/yaad/internal/intent"
)

var version = "dev" // set by -ldflags="-X main.version=v0.1.0" at build time

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// --- helpers ---

func dbPath() string {
	dir, _ := os.Getwd()
	return filepath.Join(dir, ".yaad", "yaad.db")
}

func openEngine() *engine.Engine {
	os.MkdirAll(filepath.Dir(dbPath()), 0755)
	store, err := storage.NewStore(dbPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return engine.New(store)
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// --- commands ---

var rootCmd = &cobra.Command{
	Use:   "yaad",
	Short: "Memory for coding agents",
	Long:  "Yaad (याद) — model-agnostic, graph-native memory for coding agents",
	Run: func(cmd *cobra.Command, args []string) {
		// Default: start REST server
		eng := openEngine()
		defer eng.Store().Close()
		addr := ":3456"
		fmt.Printf("yaad v%s — starting server on %s\n", version, addr)
		rest := server.NewRESTServer(eng, addr)
		if err := rest.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize .yaad/ in current project",
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		yaadDir := filepath.Join(dir, ".yaad")
		os.MkdirAll(yaadDir, 0755)
		// Create DB to initialize schema
		store, err := storage.NewStore(filepath.Join(yaadDir, "yaad.db"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		store.Close()
		fmt.Printf("✓ Initialized .yaad/ in %s\n", dir)
	},
}

var rememberCmd = &cobra.Command{
	Use:   "remember [content]",
	Short: "Store a memory node",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		typ, _ := cmd.Flags().GetString("type")
		tags, _ := cmd.Flags().GetString("tags")
		node, err := eng.Remember(engine.RememberInput{
			Content: strings.Join(args, " "),
			Type:    typ,
			Tags:    tags,
			Scope:   "project",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Remembered [%s] %s (id: %s)\n", node.Type, truncate(node.Content, 60), node.ID[:8])
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
		result, err := eng.Recall(engine.RecallOpts{
			Query: strings.Join(args, " "),
			Depth: depth,
			Limit: limit * page, // fetch enough for pagination
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		// Apply pagination
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
			fmt.Printf("[%s] %s (confidence: %.2f, id: %s)\n", n.Type, truncate(n.Content, 70), n.Confidence, n.ID[:8])
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
		edge := &storage.Edge{
			ID:     fmt.Sprintf("%s-%s-%s", args[0][:8], args[1][:8], args[2]),
			FromID: args[0],
			ToID:   args[1],
			Type:   args[2],
			Weight: 1.0,
		}
		if err := eng.Graph().AddEdge(edge); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Linked %s →[%s]→ %s\n", args[0][:8], args[2], args[1][:8])
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
		sg, err := eng.Graph().ExtractSubgraph(args[0], depth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Subgraph: %d nodes, %d edges\n", len(sg.Nodes), len(sg.Edges))
		for _, n := range sg.Nodes {
			fmt.Printf("  [%s] %s\n", n.Type, truncate(n.Content, 60))
		}
		for _, e := range sg.Edges {
			fmt.Printf("  %s →[%s]→ %s\n", e.FromID[:8], e.Type, e.ToID[:8])
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
		ids, err := eng.Graph().Impact(args[0], 3)
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
			if n, err := eng.Store().GetNode(id); err == nil {
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
		st, err := eng.Status("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("yaad v%s\n", version)
		fmt.Printf("  Nodes:    %d\n", st.Nodes)
		fmt.Printf("  Edges:    %d\n", st.Edges)
		fmt.Printf("  Sessions: %d\n", st.Sessions)
		fmt.Printf("  DB:       %s\n", dbPath())
	},
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server on stdio",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		mcp := server.NewMCPServer(eng)
		if err := mcp.ServeStdio(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start REST API server",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		addr, _ := cmd.Flags().GetString("addr")
		fmt.Printf("yaad v%s — REST API on %s\n", version, addr)
		rest := server.NewRESTServer(eng, addr)
		if err := rest.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export graph as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		nodes, _ := eng.Store().ListNodes(storage.NodeFilter{})
		type export struct {
			Nodes []*storage.Node `json:"nodes"`
		}
		printJSON(export{Nodes: nodes})
	},
}

func init() {
	rememberCmd.Flags().StringP("type", "t", "decision", "Node type")
	rememberCmd.Flags().String("tags", "", "Comma-separated tags")
	recallCmd.Flags().IntP("depth", "d", 2, "Graph expansion depth")
	recallCmd.Flags().IntP("limit", "l", 10, "Results per page")
	recallCmd.Flags().IntP("page", "p", 1, "Page number")
	subgraphCmd.Flags().IntP("depth", "d", 2, "BFS depth")
	serveCmd.Flags().String("addr", ":3456", "Listen address")
	benchCmd.Flags().Bool("extended", false, "Run extended 28-question benchmark")
	syncCmd.Flags().Bool("status", false, "Show sync status only")
	syncCmd.Flags().Bool("import", false, "Import only (don't export)")

	rootCmd.AddCommand(initCmd, rememberCmd, recallCmd, linkCmd, subgraphCmd,
		impactCmd, statusCmd, mcpCmd, serveCmd, exportCmd,
		embedCmd, hybridRecallCmd, proactiveCmd, decayCmd, gcCmd, bridgeImportCmd, bridgeExportCmd,
		hookCmd, setupCmd, replayCmd,
		exportJSONCmd, exportMarkdownCmd, exportObsidianCmd, importJSONCmd,
		skillStoreCmd, skillListCmd, skillReplayCmd, benchCmd,
		syncCmd, tuiCmd, intentCmd, doctorCmd, watchCmd)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

var embedCmd = &cobra.Command{
	Use:   "embed [node_id]",
	Short: "Generate and store embedding for a node",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		provider := embeddings.NewLocal()
		node, err := eng.Store().GetNode(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		vec, err := provider.Embed(context.Background(), node.Content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if err := eng.Store().SaveEmbedding(node.ID, provider.Name(), vec); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Embedded node %s (%d dims, provider: %s)\n", args[0][:8], len(vec), provider.Name())
	},
}

var hybridRecallCmd = &cobra.Command{
	Use:   "hybrid-recall [query]",
	Short: "Hybrid search: BM25 + vector + graph with RRF fusion",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		hs := engine.NewHybridSearch(eng.Store(), eng.Graph(), embeddings.NewLocal())
		scored, err := hs.Search(context.Background(), strings.Join(args, " "), engine.RecallOpts{
			Depth: depth, Limit: limit,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		reranked := engine.Rerank(scored, eng.Store())
		for _, sn := range reranked {
			fmt.Printf("[%.3f] [%s] %s\n", sn.Score, sn.Node.Type, truncate(sn.Node.Content, 70))
		}
	},
}

var proactiveCmd = &cobra.Command{
	Use:   "proactive",
	Short: "Show proactively predicted context for next session",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		hs := engine.NewHybridSearch(eng.Store(), eng.Graph(), nil)
		pc := engine.NewProactiveContext(eng, hs)
		nodes, err := pc.Predict(context.Background(), "", 2000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(engine.FormatContext(nodes))
	},
}

var decayCmd = &cobra.Command{
	Use:   "decay",
	Short: "Apply confidence decay to all nodes",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		if err := engine.RunDecay(eng.Store(), engine.DefaultDecayConfig); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Decay applied")
	},
}

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage collect low-confidence nodes",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		n, err := engine.GarbageCollect(eng.Store(), engine.DefaultDecayConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Removed %d nodes\n", n)
	},
}

var bridgeImportCmd = &cobra.Command{
	Use:   "bridge-import [dir]",
	Short: "Import CLAUDE.md / .cursorrules into Yaad",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		if len(args) > 0 {
			dir = args[0]
		}
		eng := openEngine()
		defer eng.Store().Close()
		n, err := bridge.Import(eng, dir, dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Imported %d memories from agent files\n", n)
	},
}

var bridgeExportCmd = &cobra.Command{
	Use:   "bridge-export [dir]",
	Short: "Export hot-tier conventions to CLAUDE.md",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		if len(args) > 0 {
			dir = args[0]
		}
		eng := openEngine()
		defer eng.Store().Close()
		if err := bridge.Export(eng.Store(), dir, dir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Exported conventions to %s/CLAUDE.md\n", dir)
	},
}

// hookCmd dispatches to hook sub-commands: session-start, post-tool-use, session-end
var hookCmd = &cobra.Command{
	Use:   "hook [event]",
	Short: "Auto-capture hook (called by coding agents)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		dir, _ := os.Getwd()
		runner := hooks.New(eng, dir)
		in, _ := hooks.ReadInput(os.Stdin)
		var err error
		switch args[0] {
		case "session-start":
			err = runner.SessionStart(in)
		case "post-tool-use":
			err = runner.PostToolUse(in)
			if err == nil {
				_ = runner.StoreToolEvent(in, eng.Store())
			}
		case "session-end":
			err = runner.SessionEnd(in)
		default:
			fmt.Fprintf(os.Stderr, "unknown hook: %s\n", args[0])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "hook error: %v\n", err)
			os.Exit(1)
		}
	},
}

// setupCmd generates agent config files
var setupCmd = &cobra.Command{
	Use:   "setup [agent]",
	Short: "Generate config for a coding agent",
	Long: `Generate MCP/REST config for your coding agent.

Supported agents:
  GrayCodeAI: hawk
  Big-lab:    claude-code, codex-cli, gemini-cli, copilot-cli, qwen-code, mistral-vibe, kiro
  IDE/Startup: cursor, windsurf, amp, droid, warp, augment
  Open source: opencode, cline, goose, roo-code, kilo, crush, hermes, aider

Any other agent: pass its name and get the universal MCP config.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		agent := agentconfig.Agent(args[0])
		if err := agentconfig.Generate(agent, dir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Generated config for %s\n", agent)
	},
}

// replayCmd shows session replay events
var replayCmd = &cobra.Command{
	Use:   "replay [session_id]",
	Short: "Show session replay timeline",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		events, err := eng.Store().GetReplayEvents(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(events) == 0 {
			fmt.Println("No events found for session:", args[0])
			return
		}
		fmt.Printf("Session %s: %d events\n", args[0][:8], len(events))
		for _, e := range events {
			fmt.Printf("  [%s] %s\n", e.CreatedAt.Format("15:04:05"), truncate(e.Data, 80))
		}
	},
}

var exportJSONCmd = &cobra.Command{
	Use:   "export-json",
	Short: "Export graph as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		data, err := exportimport.ExportJSON(eng.Store(), "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
	},
}

var exportMarkdownCmd = &cobra.Command{
	Use:   "export-md",
	Short: "Export memories as Markdown",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		md, err := exportimport.ExportMarkdown(eng.Store(), "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(md)
	},
}

var exportObsidianCmd = &cobra.Command{
	Use:   "export-obsidian [vault_dir]",
	Short: "Export memories as Obsidian vault",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		n, err := exportimport.ExportObsidian(eng.Store(), "", args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Exported %d notes to %s\n", n, args[0])
	},
}

var importJSONCmd = &cobra.Command{
	Use:   "import-json [file]",
	Short: "Import graph from JSON file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		eng := openEngine()
		defer eng.Store().Close()
		nodes, edges, err := exportimport.ImportJSON(eng.Store(), data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Imported %d nodes, %d edges\n", nodes, edges)
	},
}

var skillStoreCmd = &cobra.Command{
	Use:   "skill-store [name] [description] [step1] [step2] ...",
	Short: "Store a skill (procedural memory)",
	Args:  cobra.MinimumNArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		dir, _ := os.Getwd()
		steps := make([]skill.Step, len(args)-2)
		for i, s := range args[2:] {
			steps[i] = skill.Step{Order: i + 1, Description: s}
		}
		sk := &skill.Skill{Name: args[0], Description: args[1], Steps: steps}
		node, err := skill.Store(eng, sk, dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Stored skill %q (id: %s)\n", sk.Name, node.ID[:8])
	},
}

var skillListCmd = &cobra.Command{
	Use:   "skill-list",
	Short: "List all stored skills",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		dir, _ := os.Getwd()
		skills, err := skill.ListSkills(eng.Store(), dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(skills) == 0 {
			fmt.Println("No skills stored.")
			return
		}
		for _, s := range skills {
			fmt.Printf("  %s — %s (%d steps)\n", s.Name, s.Description, len(s.Steps))
		}
	},
}

var skillReplayCmd = &cobra.Command{
	Use:   "skill-replay [name]",
	Short: "Show skill steps for replay",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		dir, _ := os.Getwd()
		sk, err := skill.Load(eng.Store(), args[0], dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(skill.Replay(sk))
	},
}

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Run retrieval benchmark (LongMemEval-style)",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		extended, _ := cmd.Flags().GetBool("extended")
		qas := bench.DefaultQAs()
		if extended {
			qas = bench.CodingBenchQAs()
		}
		result := bench.Run(eng, qas, 2, 10)
		fmt.Println(result.String())
	},
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open interactive terminal UI",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		if err := tui.Run(eng); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch for team sync changes and auto-import",
	Long:  "Watches .yaad/manifest.json for changes and auto-runs 'yaad sync --import' when teammates push new chunks.",
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		manifest := filepath.Join(dir, ".yaad", "manifest.json")
		fmt.Printf("Watching %s for team sync changes...\n", manifest)
		fmt.Println("Press Ctrl+C to stop.")

		var lastMod time.Time
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			info, err := os.Stat(manifest)
			if err != nil {
				continue
			}
			if info.ModTime().After(lastMod) && !lastMod.IsZero() {
				fmt.Printf("[%s] manifest.json changed — importing...\n", time.Now().Format("15:04:05"))
				eng := openEngine()
				syncer := yaadsync.New(eng.Store(), dir)
				n, e, err := syncer.Import()
				eng.Store().Close()
				if err != nil {
					fmt.Fprintf(os.Stderr, "import error: %v\n", err)
				} else if n > 0 || e > 0 {
					fmt.Printf("  ✓ Imported %d nodes, %d edges\n", n, e)
				} else {
					fmt.Println("  (no new chunks)")
				}
			}
			lastMod = info.ModTime()
		}
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose Yaad setup issues",
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		ok := true
		check := func(label string, pass bool, fix string) {
			if pass {
				fmt.Printf("  ✅ %s\n", label)
			} else {
				fmt.Printf("  ❌ %s\n     Fix: %s\n", label, fix)
				ok = false
			}
		}

		fmt.Printf("yaad doctor — %s\n\n", dir)

		// 1. .yaad/ directory
		_, err := os.Stat(filepath.Join(dir, ".yaad"))
		check(".yaad/ directory exists", err == nil, "run: yaad init")

		// 2. DB file
		dbPath := filepath.Join(dir, ".yaad", "yaad.db")
		_, err = os.Stat(dbPath)
		check("yaad.db exists", err == nil, "run: yaad init")

		// 3. DB readable
		if err == nil {
			store, err2 := storage.NewStore(dbPath)
			if err2 == nil {
				store.Close()
				check("database readable", true, "")
			} else {
				check("database readable", false, "delete .yaad/yaad.db and run: yaad init")
			}
		}

		// 4. REST server reachable
		resp, err := http.Get("http://localhost:3456/yaad/health")
		serverRunning := err == nil && resp.StatusCode == 200
		if resp != nil {
			resp.Body.Close()
		}
		check("REST server running (:3456)", serverRunning, "run: yaad serve  (in another terminal)")

		// 5. MCP config
		mcpFiles := []string{".mcp.json", "opencode.json", ".codex/config.yaml"}
		hasMCP := false
		for _, f := range mcpFiles {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				hasMCP = true
				break
			}
		}
		check("agent MCP config found", hasMCP, "run: yaad setup <agent>  (e.g. yaad setup hawk)")

		// 6. Git repo
		_, err = os.Stat(filepath.Join(dir, ".git"))
		check("git repository (for staleness detection)", err == nil, "run: git init")

		fmt.Println()
		if ok {
			fmt.Println("✅ All checks passed. Yaad is ready!")
		} else {
			fmt.Println("⚠️  Some checks failed. Fix the issues above and re-run: yaad doctor")
		}
	},
}

var intentCmd = &cobra.Command{
	Use:   "intent [query]",
	Short: "Classify query intent (Why/When/Who/How/What) for intent-aware retrieval",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := strings.Join(args, " ")
		i := intentpkg.Classify(query)
		w := intentpkg.Weights(i)
		fmt.Printf("Query:  %s\n", query)
		fmt.Printf("Intent: %s\n", i.String())
		fmt.Printf("Edge weights:\n")
		fmt.Printf("  caused_by:  %.1f  led_to:    %.1f\n", w.CausedBy, w.LedTo)
		fmt.Printf("  learned_in: %.1f  touches:   %.1f\n", w.LearnedIn, w.Touches)
		fmt.Printf("  part_of:    %.1f  relates_to: %.1f\n", w.PartOf, w.RelatesTo)
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync memories via git chunks (.yaad/chunks/*.jsonl.gz)",
	Long: `Export new memories as a chunk and import chunks from teammates.
Chunks are append-only gzipped JSONL files — no merge conflicts.
Commit .yaad/manifest.json and .yaad/chunks/ to share with your team.`,
	Run: func(cmd *cobra.Command, args []string) {
		dir, _ := os.Getwd()
		eng := openEngine()
		defer eng.Store().Close()

		statusOnly, _ := cmd.Flags().GetBool("status")
		importOnly, _ := cmd.Flags().GetBool("import")

		syncer := yaadsync.New(eng.Store(), dir)

		if statusOnly {
			st, err := syncer.Status()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Sync status:\n  Total chunks:    %d\n  Imported chunks: %d\n  Pending chunks:  %d\n",
				st.TotalChunks, st.ImportedChunks, st.PendingChunks)
			return
		}

		// Import first (pull from teammates)
		n, e, err := syncer.Import()
		if err != nil {
			fmt.Fprintf(os.Stderr, "import error: %v\n", err)
		} else if n > 0 || e > 0 {
			fmt.Printf("✓ Imported %d nodes, %d edges from chunks\n", n, e)
		}

		if importOnly {
			return
		}

		// Export (push new memories)
		hash, err := syncer.Export(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "export error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Exported chunk %s → .yaad/chunks/%s.jsonl.gz\n", hash, hash)
		fmt.Printf("  Commit .yaad/manifest.json and .yaad/chunks/ to share with your team\n")
	},
}
