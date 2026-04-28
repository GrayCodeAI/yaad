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
	AgentClaudeCode Agent = "claude-code"
	AgentCursor     Agent = "cursor"
	AgentGeminiCLI  Agent = "gemini-cli"
	AgentOpenCode   Agent = "opencode"
	AgentCodexCLI   Agent = "codex-cli"
	AgentCline      Agent = "cline"
	AgentWindsurf   Agent = "windsurf"
	AgentGoose      Agent = "goose"
	AgentRooCode    Agent = "roo-code"
	AgentAider      Agent = "aider"
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
	case AgentCline, AgentWindsurf, AgentGoose, AgentRooCode:
		return generateGenericMCP(projectDir, string(agent))
	case AgentAider:
		fmt.Println("Aider uses REST API. Add to your workflow:")
		fmt.Println("  yaad context --format=markdown > .yaad/context.md")
		fmt.Println("  aider --read .yaad/context.md")
		return nil
	default:
		return fmt.Errorf("unknown agent: %s", agent)
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
