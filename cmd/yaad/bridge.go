package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/internal/hooks"
	"github.com/GrayCodeAI/yaad/internal/skill"
	"github.com/GrayCodeAI/yaad/internal/utils"
	yaadsync "github.com/GrayCodeAI/yaad/internal/sync"
	intentpkg "github.com/GrayCodeAI/yaad/internal/intent"
)

var hookCmd = &cobra.Command{
	Use:   "hook [event]",
	Short: "Auto-capture hook for lifecycle events",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		dir, _ := os.Getwd()
		runner := hooks.New(eng, dir)
		in, _ := hooks.ReadInput(os.Stdin)
		ctx := context.Background()
		var err error
		switch args[0] {
		case "session-start":
			err = runner.SessionStart(ctx, in)
		case "post-tool-use":
			err = runner.PostToolUse(ctx, in)
			if err == nil {
				_ = runner.StoreToolEvent(ctx, in, eng.Store())
			}
		case "session-end":
			err = runner.SessionEnd(ctx, in)
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

var replayCmd = &cobra.Command{
	Use:   "replay [session_id]",
	Short: "Show session replay timeline",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		events, err := eng.Store().GetReplayEvents(context.Background(), args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(events) == 0 {
			fmt.Println("No events found for session:", args[0])
			return
		}
		fmt.Printf("Session %s: %d events\n", utils.ShortID(args[0]), len(events))
		for _, e := range events {
			fmt.Printf("  [%s] %s\n", e.CreatedAt.Format("15:04:05"), truncate(e.Data, 80))
		}
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

		n, e, err := syncer.Import(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "import error: %v\n", err)
		} else if n > 0 || e > 0 {
			fmt.Printf("✓ Imported %d nodes, %d edges from chunks\n", n, e)
		}

		if importOnly {
			return
		}

		hash, err := syncer.Export(context.Background(), dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "export error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Exported chunk %s → .yaad/chunks/%s.jsonl.gz\n", hash, hash)
		fmt.Printf("  Commit .yaad/manifest.json and .yaad/chunks/ to share with your team\n")
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

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating watcher: %v\n", err)
			os.Exit(1)
		}
		defer watcher.Close()

		if err := watcher.Add(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "error watching manifest: %v\n", err)
			os.Exit(1)
		}

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					fmt.Printf("[%s] manifest.json changed — importing...\n", time.Now().Format("15:04:05"))
					eng := openEngine()
					syncer := yaadsync.New(eng.Store(), dir)
					n, e, err := syncer.Import(context.Background())
					eng.Store().Close()
					if err != nil {
						fmt.Fprintf(os.Stderr, "import error: %v\n", err)
					} else if n > 0 || e > 0 {
						fmt.Printf("  ✓ Imported %d nodes, %d edges\n", n, e)
					} else {
						fmt.Println("  (no new chunks)")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
			}
		}
	},
}

var intentCmd = &cobra.Command{
	Use:   "intent [query]",
	Short: "Classify query intent (Why/When/Who/How/What) for intent-aware retrieval",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := args[0]
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
		node, err := skill.Store(context.Background(), eng, sk, dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Stored skill %q (id: %s)\n", sk.Name, utils.ShortID(node.ID))
	},
}

var skillListCmd = &cobra.Command{
	Use:   "skill-list",
	Short: "List all stored skills",
	Run: func(cmd *cobra.Command, args []string) {
		eng := openEngine()
		defer eng.Store().Close()
		dir, _ := os.Getwd()
		skills, err := skill.ListSkills(context.Background(), eng.Store(), dir)
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
		sk, err := skill.Load(context.Background(), eng.Store(), args[0], dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(skill.Replay(sk))
	},
}
