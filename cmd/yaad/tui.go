package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/tui"
)

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
