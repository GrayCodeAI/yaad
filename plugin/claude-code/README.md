# Yaad — Claude Code Plugin

Gives Claude Code persistent graph-native memory via Yaad.

## Install

```bash
# 1. Copy hooks
mkdir -p ~/.claude/yaad
cp scripts/session-start.sh ~/.claude/yaad/
cp scripts/session-end.sh ~/.claude/yaad/
chmod +x ~/.claude/yaad/*.sh

# 2. Add to your project's .mcp.json
cat > .mcp.json << 'EOF'
{
  "mcpServers": {
    "yaad": { "command": "yaad", "args": ["mcp"] }
  }
}
EOF

# 3. Add to your project's .claude/hooks.json
mkdir -p .claude
cat > .claude/hooks.json << 'EOF'
{
  "hooks": {
    "SessionStart": [{ "command": "~/.claude/yaad/session-start.sh" }],
    "SessionEnd":   [{ "command": "~/.claude/yaad/session-end.sh" }]
  }
}
EOF
```

## What It Does

- **SessionStart**: Starts Yaad server if not running, imports team sync chunks, injects hot-tier context into session
- **SessionEnd**: Stores session summary as a memory node
- **MCP**: Exposes 12 Yaad tools (`yaad_recall`, `yaad_remember`, etc.) to Claude Code
