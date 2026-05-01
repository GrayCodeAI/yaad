package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/bench"
	"github.com/GrayCodeAI/yaad/engine"
	"github.com/GrayCodeAI/yaad/exportimport"
	"github.com/GrayCodeAI/yaad/storage"
)

var decayCmd = &cobra.Command{
	Use:   "decay",
	Short: "Apply confidence decay to all nodes",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		if err := engine.RunDecay(context.Background(), eng.Store(), eng.DecayConfig); err != nil {
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
		n, err := engine.GarbageCollect(context.Background(), eng.Store(), eng.DecayConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Removed %d nodes\n", n)
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
		result := bench.Run(context.Background(), eng, qas, 2, 10)
		fmt.Println(result.String())
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

		_, err := os.Stat(filepath.Join(dir, ".yaad"))
		check(".yaad/ directory exists", err == nil, "run: yaad init")

		dbPath := filepath.Join(dir, ".yaad", "yaad.db")
		_, err = os.Stat(dbPath)
		check("yaad.db exists", err == nil, "run: yaad init")

		if err == nil {
			store, err2 := storage.NewStore(dbPath)
			if err2 == nil {
				store.Close()
				check("database readable", true, "")
			} else {
				check("database readable", false, "delete .yaad/yaad.db and run: yaad init")
			}
		}

		resp, err := http.Get("http://localhost:3456/yaad/health")
		serverRunning := err == nil && resp.StatusCode == 200
		if resp != nil {
			resp.Body.Close()
		}
		check("REST server running (:3456)", serverRunning, "run: yaad serve  (in another terminal)")

		_, mcpErr := os.Stat(filepath.Join(dir, ".mcp.json"))
		check("Hawk MCP config (.mcp.json)", mcpErr == nil, "run: yaad setup")

		_, hooksErr := os.Stat(filepath.Join(dir, ".hawk", "hooks.json"))
		check("Hawk hooks config (.hawk/hooks.json)", hooksErr == nil, "run: yaad setup")

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

var exportJSONCmd = &cobra.Command{
	Use:   "export-json",
	Short: "Export graph as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		data, err := exportimport.ExportJSON(context.Background(), eng.Store(), "")
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
		md, err := exportimport.ExportMarkdown(context.Background(), eng.Store(), "")
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
		n, err := exportimport.ExportObsidian(context.Background(), eng.Store(), "", args[0])
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
		nodes, edges, err := exportimport.ImportJSON(context.Background(), eng.Store(), data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Imported %d nodes, %d edges\n", nodes, edges)
	},
}
