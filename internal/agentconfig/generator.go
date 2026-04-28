// Package agentconfig generates configuration files for coding agents.
package agentconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Agent represents a supported coding agent.
type Agent string

const (
	// Big-lab native
	AgentClaudeCode  Agent = "claude-code"
	AgentCodexCLI    Agent = "codex-cli"
	AgentGeminiCLI   Agent = "gemini-cli"
	AgentCopilotCLI  Agent = "copilot-cli"
	AgentQwenCode    Agent = "qwen-code"
	AgentMistralVibe Agent = "mistral-vibe"
	AgentKiro        Agent = "kiro"

	// IDE / startup
	AgentCursor    Agent = "cursor"
	AgentWindsurf  Agent = "windsurf"
	AgentAmp       Agent = "amp"
	AgentDroid     Agent = "droid"
	AgentWarp      Agent = "warp"
	AgentAugment   Agent = "augment"

	// Open source / community
	AgentOpenCode  Agent = "opencode"
	AgentCline     Agent = "cline"
	AgentGoose     Agent = "goose"
	AgentRooCode   Agent = "roo-code"
	AgentKilo      Agent = "kilo"
	AgentCrush     Agent = "crush"
	AgentHermes    Agent = "hermes"
	AgentAider     Agent = "aider"
)

// mcpConfig is the universal MCP server config block.
var mcpConfig = map[string]any{
	"mcpServers": map[string]any{
		"yaad": map[string]any{
			"command": "yaad",
			"args":    []string{"mcp"},
		},
	},
}

// Generate writes the appropriate config files for the given agent.
func Generate(agent Agent, projectDir string) error {
	switch agent {
	case AgentClaudeCode:
		return generateClaudeCode(projectDir)
	case AgentCursor:
		return generateCursor(projectDir)
	case AgentGeminiCLI:
		return generateGeminiCLI(projectDir)
	case AgentOpenCode:
		return generateOpenCode(projectDir)
	case AgentCodexCLI:
		return generateCodexCLI(projectDir)
	case AgentCline, AgentWindsurf, AgentGoose, AgentRooCode, AgentKilo, AgentCrush,
		AgentHermes, AgentAmp, AgentDroid, AgentWarp, AgentAugment,
		AgentCopilotCLI, AgentQwenCode, AgentMistralVibe, AgentKiro:
		return generateGenericMCP(projectDir, string(agent))
	case AgentAider:
		fmt.Println("Aider uses REST API. Add to your workflow:")
		fmt.Println("  yaad context --format=markdown > .yaad/context.md")
		fmt.Println("  aider --read .yaad/context.md")
		return nil
	default:
		// Unknown agent — print universal MCP config
		fmt.Printf("Unknown agent %q — using universal MCP config:\n", agent)
		return generateGenericMCP(projectDir, string(agent))
	}
}

func generateClaudeCode(dir string) error {
	// .mcp.json
	if err := writeJSON(filepath.Join(dir, ".mcp.json"), mcpConfig); err != nil {
		return err
	}

	// .claude/hooks.json — auto-capture hooks
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(claudeDir, 0755)
	hooks := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []map[string]any{
				{"command": "yaad hook session-start"},
			},
			"PostToolUse": []map[string]any{
				{"command": "yaad hook post-tool-use"},
			},
			"SessionEnd": []map[string]any{
				{"command": "yaad hook session-end"},
			},
		},
	}
	return writeJSON(filepath.Join(claudeDir, "hooks.json"), hooks)
}

func generateCursor(dir string) error {
	home, _ := os.UserHomeDir()
	cursorDir := filepath.Join(home, ".cursor")
	os.MkdirAll(cursorDir, 0755)
	return writeJSON(filepath.Join(cursorDir, "mcp.json"), mcpConfig)
}

func generateGeminiCLI(_ string) error {
	fmt.Println("Run: gemini mcp add yaad -- yaad mcp")
	return nil
}

func generateOpenCode(dir string) error {
	cfg := map[string]any{
		"mcp": map[string]any{
			"yaad": map[string]any{
				"type":    "local",
				"command": []string{"yaad", "mcp"},
				"enabled": true,
			},
		},
	}
	return writeJSON(filepath.Join(dir, "opencode.json"), cfg)
}

func generateCodexCLI(dir string) error {
	codexDir := filepath.Join(dir, ".codex")
	os.MkdirAll(codexDir, 0755)
	yaml := "mcp_servers:\n  yaad:\n    command: yaad\n    args: [\"mcp\"]\n"
	return os.WriteFile(filepath.Join(codexDir, "config.yaml"), []byte(yaml), 0644)
}

func generateGenericMCP(dir, agent string) error {
	fmt.Printf("Add to %s MCP settings:\n", agent)
	b, _ := json.MarshalIndent(mcpConfig, "", "  ")
	fmt.Println(string(b))
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
