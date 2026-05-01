package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "yaad",
	Short: "Graph-native memory system",
	Long:  "Yaad (याद) — model-agnostic, graph-native memory system",
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
	serveCmd.Flags().String("addr", "127.0.0.1:3456", "Listen address")
	serveCmd.Flags().Bool("daemon", false, "Run as background daemon")
	startCmd.Flags().String("addr", "127.0.0.1:3456", "Listen address")

	// Admin commands
	benchCmd.Flags().Bool("extended", false, "Run extended 28-question benchmark")

	rootCmd.AddCommand(
		initCmd, setupCmd,
		rememberCmd, recallCmd, linkCmd, subgraphCmd, impactCmd, statusCmd,
		serveCmd, startCmd, stopCmd, mcpCmd, exportCmd,
		embedCmd, hybridRecallCmd, proactiveCmd,
		decayCmd, gcCmd,
		hookCmd, replayCmd,
		exportJSONCmd, exportMarkdownCmd, exportObsidianCmd, importJSONCmd,
		skillStoreCmd, skillListCmd, skillReplayCmd,
		benchCmd, tuiCmd, intentCmd, doctorCmd,
	)
}
