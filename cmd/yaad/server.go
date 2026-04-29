package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/embeddings"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/server"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/utils"
)

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

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server on stdio",
	Long:  `Tool profiles: --tools=agent (8 core tools, saves ~800 tokens) or --tools=all (15 tools, default)`,
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		profile, _ := cmd.Flags().GetString("tools")
		mcp := server.NewMCPServer(eng, profile)
		if err := mcp.ServeStdio(); err != nil {
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
		nodes, _ := eng.Store().ListNodes(context.Background(), storage.NodeFilter{})
		type export struct {
			Nodes []*storage.Node `json:"nodes"`
		}
		printJSON(export{Nodes: nodes})
	},
}

var embedCmd = &cobra.Command{
	Use:   "embed [node_id]",
	Short: "Generate and store embedding for a node",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		provider := embeddings.NewLocal()
		node, err := eng.Store().GetNode(context.Background(), args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		vec, err := provider.Embed(context.Background(), node.Content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if err := eng.Store().SaveEmbedding(context.Background(), node.ID, provider.Name(), vec); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Embedded node %s (%d dims, provider: %s)\n", utils.ShortID(args[0]), len(vec), provider.Name())
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
		scored, err := hs.Search(context.Background(), args[0], engine.RecallOpts{
			Depth: depth, Limit: limit,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		reranked := engine.Rerank(context.Background(), scored, eng.Store())
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
