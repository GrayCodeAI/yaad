// Package hooks implements auto-capture hooks for coding agents.
// Hooks are invoked by the agent at key lifecycle events and automatically
// capture observations into the Yaad memory graph.
package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/privacy"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// HookInput is the JSON payload passed to hooks via stdin.
type HookInput struct {
	// Common
	SessionID string `json:"session_id"`
	Project   string `json:"project"`
	Agent     string `json:"agent"`

	// PostToolUse
	ToolName   string `json:"tool_name"`
	ToolInput  string `json:"tool_input"`
	ToolOutput string `json:"tool_output"`
	ToolError  string `json:"tool_error"`

	// UserPromptSubmit
	Prompt string `json:"prompt"`

	// SessionEnd
	Summary string `json:"summary"`
}

// Runner executes hook logic.
type Runner struct {
	eng     *engine.Engine
	project string
}

// New creates a hook runner.
func New(eng *engine.Engine, project string) *Runner {
	if project == "" {
		project, _ = os.Getwd()
	}
	return &Runner{eng: eng, project: project}
}

// ReadInput reads HookInput from stdin (agents pipe JSON to hooks).
func ReadInput(r io.Reader) (*HookInput, error) {
	var in HookInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		// Not all hooks send JSON — treat as empty input
		return &in, nil
	}
	return &in, nil
}

// SessionStart is called when an agent session begins.
// Outputs hot-tier context to stdout for injection into the session.
func (r *Runner) SessionStart(in *HookInput) error {
	sessionID, err := r.eng.StartSession(r.project, in.Agent)
	if err != nil {
		return err
	}

	// Write session ID to a temp file for other hooks to pick up
	_ = os.WriteFile(sessionFile(r.project), []byte(sessionID), 0644)

	// Get context and print to stdout (Claude Code injects stdout into session)
	result, err := r.eng.Context(r.project)
	if err != nil {
		return err
	}
	fmt.Print(engine.FormatContext(result.Nodes))
	return nil
}

// PostToolUse is called after each tool use. Captures the observation.
func (r *Runner) PostToolUse(in *HookInput) error {
	if in.ToolName == "" {
		return nil
	}

	sessionID := readSessionID(r.project)
	content := buildObservation(in)
	if content == "" {
		return nil
	}

	// Privacy filter
	content = privacy.Filter(content)

	// Classify the observation type
	nodeType := classifyTool(in.ToolName)

	_, err := r.eng.Remember(engine.RememberInput{
		Type:    nodeType,
		Content: content,
		Scope:   "project",
		Project: r.project,
		Session: sessionID,
		Agent:   in.Agent,
	})
	return err
}

// SessionEnd is called when a session ends. Compresses and stores summary.
func (r *Runner) SessionEnd(in *HookInput) error {
	sessionID := readSessionID(r.project)
	if sessionID == "" {
		return nil
	}

	// Store summary as a decision/spec if provided
	if in.Summary != "" {
		_, _ = r.eng.Remember(engine.RememberInput{
			Type:    "session",
			Content: privacy.Filter(in.Summary),
			Scope:   "project",
			Project: r.project,
			Session: sessionID,
			Agent:   in.Agent,
		})
	}

	// Compress session
	_, err := r.eng.CompressSession(sessionID, r.project)

	// Clean up session file
	_ = os.Remove(sessionFile(r.project))
	return err
}

// --- helpers ---

func buildObservation(in *HookInput) string {
	if in.ToolError != "" {
		return fmt.Sprintf("Tool %s failed: %s (input: %s)",
			in.ToolName, truncate(in.ToolError, 200), truncate(in.ToolInput, 100))
	}
	if in.ToolOutput != "" {
		return fmt.Sprintf("Tool %s: input=%s output=%s",
			in.ToolName, truncate(in.ToolInput, 100), truncate(in.ToolOutput, 200))
	}
	if in.ToolInput != "" {
		return fmt.Sprintf("Tool %s: %s", in.ToolName, truncate(in.ToolInput, 200))
	}
	return ""
}

func classifyTool(toolName string) string {
	switch toolName {
	case "Write", "Edit", "MultiEdit":
		return "convention" // file edits often encode conventions
	case "Bash", "Computer":
		return "decision" // commands often reflect decisions
	case "Read", "Glob", "Grep":
		return "spec" // reads often relate to understanding specs
	default:
		return "decision"
	}
}

func sessionFile(project string) string {
	return filepath.Join(project, ".yaad", ".session")
}

func readSessionID(project string) string {
	b, err := os.ReadFile(sessionFile(project))
	if err != nil {
		return ""
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// StoreToolEvent stores a raw tool event for session replay.
func (r *Runner) StoreToolEvent(in *HookInput, store storage.Storage) error {
	sessionID := readSessionID(r.project)
	b, _ := json.Marshal(map[string]any{
		"tool":    in.ToolName,
		"input":   in.ToolInput,
		"output":  in.ToolOutput,
		"error":   in.ToolError,
		"time":    time.Now().Unix(),
		"session": sessionID,
	})
	return store.AddReplayEvent(sessionID, string(b))
}
