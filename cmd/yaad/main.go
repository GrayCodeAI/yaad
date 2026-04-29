package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev" // set by -ldflags="-X main.version=v0.1.0" at build time

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "yaad",
	Short: "Memory for coding agents",
	Long:  "Yaad (याद) — model-agnostic, graph-native memory for coding agents",
	Run: func(cmd *cobra.Command, args []string) {
		// Default: start REST server
		serveCmd.Run(cmd, args)
	},
}

func init() {
	// Core commands
	rememberCmd.Flags().StringP("type", "t", "decision", "Node type")
	rememberCmd.Flags().String("tags", "", "Comma-separated tags")
	recallCmd.Flags().IntP("depth", "d", 2, "Graph expansion depth")
	recallCmd.Flags().IntP("limit", "l", 10, "Results per page")
	recallCmd.Flags().IntP("page", "p", 1, "Page number")
	subgraphCmd.Flags().IntP("depth", "d", 2, "BFS depth")

	// Server commands
	serveCmd.Flags().String("addr", ":3456", "Listen address")
	mcpCmd.Flags().String("tools", "all", "Tool profile: agent (8 core) or all (15 tools)")

	// Admin commands
	benchCmd.Flags().Bool("extended", false, "Run extended 28-question benchmark")

	// Sync commands
	syncCmd.Flags().Bool("status", false, "Show sync status only")
	syncCmd.Flags().Bool("import", false, "Import only (don't export)")

	rootCmd.AddCommand(
		initCmd,
		rememberCmd, recallCmd, linkCmd, subgraphCmd, impactCmd, statusCmd,
		serveCmd, mcpCmd, exportCmd,
		embedCmd, hybridRecallCmd, proactiveCmd,
		decayCmd, gcCmd,
		hookCmd, replayCmd,
		exportJSONCmd, exportMarkdownCmd, exportObsidianCmd, importJSONCmd,
		skillStoreCmd, skillListCmd, skillReplayCmd,
		benchCmd, syncCmd, tuiCmd, intentCmd, doctorCmd, watchCmd,
	)
}
