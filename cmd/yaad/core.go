package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/utils"
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
