# Yaad — Hermes Agent Plugin

Gives [Hermes](https://github.com/NousResearch/hermes-agent) persistent graph-native memory via Yaad.

## Install

```bash
# 1. Add to ~/.hermes/config.yaml
cat >> ~/.hermes/config.yaml << 'EOF'
mcp_servers:
  yaad:
    command: yaad
    args: ["mcp"]
EOF

# 2. Initialize Yaad in your project
cd your-project
yaad init

# 3. Start Yaad
yaad mcp
```

## What It Does

- **Session start**: Hermes calls `yaad_context` → gets hot-tier memory injected
- **During session**: Hermes calls `yaad_recall` to search memories, `yaad_remember` to store
- **Session end**: Call `yaad hook session-end` to compress session into summary

## Advanced: Auto-capture hooks

Add to `~/.hermes/config.yaml`:

```yaml
hooks:
  session_start:
    - command: yaad hook session-start
  session_end:
    - command: yaad hook session-end
```

## MCP Tools Available

All 13 Yaad MCP tools are available in Hermes:
`yaad_recall`, `yaad_remember`, `yaad_context`, `yaad_forget`, `yaad_link`,
`yaad_unlink`, `yaad_subgraph`, `yaad_impact`, `yaad_status`, `yaad_task_update`,
`yaad_sessions`, `yaad_stale`, `yaad_intent`
