package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/daemon"
	"github.com/GrayCodeAI/yaad/internal/embeddings"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/server"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/utils"
	"github.com/GrayCodeAI/yaad/internal/version"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start REST API server",
	Run: func(cmd *cobra.Command, args []string) {
		daemonize, _ := cmd.Flags().GetBool("daemon")
		addr, _ := cmd.Flags().GetString("addr")
		projectDir, _ := os.Getwd()

		if daemonize {
			// Re-exec ourselves without --daemon in background
			if err := daemon.EnsureRunning(projectDir, addr); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("yaad daemon running on %s (pid %d)\n", addr, daemon.ReadPID(projectDir))
			return
		}

		eng := openEngine()
		defer eng.Store().Close()

		// Write PID file so other processes can find us
		if err := daemon.WritePID(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "yaad: warning: could not write PID file: %v\n", err)
		}
		defer daemon.RemovePID(projectDir)

		fmt.Printf("yaad v%s — REST API on %s (pid %d)\n", version.String(), addr, os.Getpid())
		rest := server.NewRESTServer(eng, addr).WithProjectDir(projectDir)

		// Graceful shutdown on SIGTERM/SIGINT
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-sigCh
			fmt.Fprintf(os.Stderr, "\nyaad: shutting down...\n")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rest.Shutdown(ctx)
		}()

		if err := rest.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Ensure yaad daemon is running (health check → auto-start)",
	Run: func(cmd *cobra.Command, args []string) {
		addr, _ := cmd.Flags().GetString("addr")
		projectDir, _ := os.Getwd()

		if daemon.HealthCheck(addr) == nil {
			pid := daemon.ReadPID(projectDir)
			fmt.Printf("yaad already running on %s (pid %d)\n", addr, pid)
			return
		}

		fmt.Printf("Starting yaad daemon on %s...\n", addr)
		if err := daemon.EnsureRunning(projectDir, addr); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("yaad daemon ready (pid %d)\n", daemon.ReadPID(projectDir))
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the yaad daemon",
	Run: func(cmd *cobra.Command, args []string) {
		projectDir, _ := os.Getwd()
		if !daemon.IsRunning(projectDir) {
			fmt.Println("yaad daemon is not running")
			return
		}
		if err := daemon.Stop(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("yaad daemon stopped")
	},
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server on stdio (used by Hawk)",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		mcp := server.NewMCPServer(eng, "all")
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
		nodes, err := eng.Store().ListNodes(context.Background(), storage.NodeFilter{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
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
