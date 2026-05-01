package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/GrayCodeAI/yaad/hooks"
	"github.com/GrayCodeAI/yaad/skill"
	"github.com/GrayCodeAI/yaad/utils"
	intentpkg "github.com/GrayCodeAI/yaad/intent"
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
