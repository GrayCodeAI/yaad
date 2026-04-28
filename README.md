# Yaad (याद) — Memory for Coding Agents

> **yaad** (Hindi/Urdu): memory, remembrance  
> *"Your coding agent remembers everything — across sessions, across models, across projects."*

Yaad is a **model-agnostic, agent-agnostic** persistent memory system for coding agents. It works with **any** coding agent that supports MCP or HTTP — no lock-in, no cloud dependency, no external databases.

**One binary. Zero dependencies. Works with every agent.**

---

## Works With Every Coding Agent

| Agent | Integration | Setup |
|---|---|---|
| **Claude Code** | MCP + auto-capture hooks | `yaad setup claude-code` |
| **Cursor** | MCP server | `yaad setup cursor` |
| **Gemini CLI** | MCP | `yaad setup gemini-cli` |
| **OpenCode** | MCP | `yaad setup opencode` |
| **Codex CLI** | MCP | `yaad setup codex-cli` |
| **Cline** | MCP server | `yaad setup cline` |
| **Windsurf** | MCP server | `yaad setup cline` (same config) |
| **Goose** | MCP server | Add MCP config manually |
| **Roo Code** | MCP server | Add MCP config manually |
| **Aider** | REST API | `yaad context --format=markdown > .yaad/context.md` |
| **Any agent** | REST API | HTTP calls to `localhost:3456` |

### Universal MCP Config (works with any MCP-compatible agent)

```json
{
  "mcpServers": {
    "yaad": {
      "command": "yaad",
      "args": ["mcp"]
    }
  }
}
```

---

## Quick Start

```bash
# Install
go install -tags sqlite_fts5 github.com/yaadmemory/yaad/cmd/yaad@latest

# Initialize in your project
cd your-project
yaad init

# Generate config for your agent (pick one)
yaad setup claude-code    # → .mcp.json + .claude/hooks.json
yaad setup cursor         # → ~/.cursor/mcp.json
yaad setup gemini-cli     # → prints: gemini mcp add yaad -- yaad mcp
yaad setup opencode       # → opencode.json
yaad setup codex-cli      # → .codex/config.yaml
yaad setup cline          # → prints MCP config block

# Start Yaad (MCP server on stdio + REST on :3456)
yaad mcp          # MCP mode (for agents)
yaad serve        # REST mode (for HTTP clients)
yaad              # Both (default)
```

---

## What Yaad Does

Every coding agent suffers from **session amnesia** — it forgets everything when the session ends. You waste the first 5 minutes of every session re-explaining your stack, conventions, and what you were working on.

Yaad fixes this by giving agents a **persistent, graph-native memory** that:

- **Survives session boundaries** — agent picks up exactly where it left off
- **Works across models** — switch from Claude to Gemini to GPT without losing context
- **Understands code** — 9 coding-specific memory types (conventions, decisions, bugs, specs, tasks, skills, preferences, files, entities)
- **Tracks relationships** — Relaxed DAG: decisions lead to conventions, bugs relate to specs, tasks depend on tasks
- **Detects staleness** — git-aware: flags memories when source files change
- **Stays local** — SQLite only, your data never leaves your machine

---

## Memory Types

| Type | Description | Example |
|---|---|---|
| `convention` | Coding rules, patterns, style | "Use jose not jsonwebtoken for Edge compat" |
| `decision` | Architecture choices + rationale | "Chose NATS over Redis Streams for backpressure" |
| `bug` | Symptom → Cause → Fix | "Token refresh race → use mutex in auth.ts" |
| `spec` | How a subsystem works | "Auth uses RS256 JWT with jose library" |
| `task` | What's done / in-progress / blocked | "✓ auth endpoint, → rate limiting, ○ tests" |
| `skill` | Reusable step sequences | "Deploy: test → build → fly deploy" |
| `preference` | User coding style & habits | "Prefers functional style, uses tabs" |
| `file` | File/module anchor | "src/middleware/auth.ts" |
| `entity` | Auto-extracted entity | "jose", "PostgreSQL", "AuthService" |

---

## How It Works

```
Agent starts session
  → yaad_context returns hot-tier subgraph (~2K tokens)
  → Agent sees: conventions, active tasks, stale warnings, previous session summary

Agent works
  → yaad_recall "auth middleware" → returns auth subgraph (decisions + bugs + specs)
  → yaad_remember "Use RS256 for JWT" → stores node, auto-extracts entities, links edges

Session ends
  → yaad hook session-end → compresses session into summary node
  → Next session picks up from summary
```

---

## Interfaces

### MCP Tools (12 tools)
`yaad_recall`, `yaad_remember`, `yaad_context`, `yaad_forget`, `yaad_link`, `yaad_unlink`, `yaad_subgraph`, `yaad_impact`, `yaad_status`, `yaad_task_update`, `yaad_sessions`, `yaad_stale`

### REST API (25+ endpoints)
`POST /yaad/remember`, `POST /yaad/recall`, `GET /yaad/context`, `GET /yaad/health`, `GET /yaad/graph/stats`, `POST /yaad/hybrid-recall`, `GET /yaad/proactive`, `POST /yaad/feedback`, `POST /yaad/decay`, `POST /yaad/gc`, `POST /yaad/bridge/import`, `POST /yaad/bridge/export`, `GET /yaad/events` (SSE), `GET /yaad/replay/{id}`, `POST /yaad/export/json`, `POST /yaad/export/markdown`, `POST /yaad/skill/store`, `GET /yaad/skill/list`, `POST /yaad/team/share`, `POST /yaad/bench`, and more.

### gRPC (port 3457)
12 unary RPCs + `WatchMemories` streaming. Auto-generated SDKs for Python, TypeScript, Rust, Java.

### Web Dashboard
`http://localhost:3456/yaad/ui` — D3.js force graph visualization of your memory graph.

---

## CLI Commands (29 commands)

```bash
# Core
yaad init                    # Initialize .yaad/ in project
yaad remember -t convention "Use jose not jsonwebtoken"
yaad recall "auth middleware"
yaad status
yaad link <id1> <id2> led_to
yaad subgraph <id> --depth 2
yaad impact src/auth.ts

# Agent setup
yaad setup claude-code       # Generate Claude Code config
yaad setup cursor            # Generate Cursor config
yaad setup gemini-cli        # Generate Gemini CLI config
yaad setup opencode          # Generate OpenCode config
yaad setup codex-cli         # Generate Codex CLI config
yaad setup cline             # Generate Cline config

# Hooks (called by agents automatically)
yaad hook session-start
yaad hook post-tool-use
yaad hook session-end

# Search
yaad hybrid-recall "auth JWT"  # BM25 + vector + graph with RRF
yaad proactive                 # Predict next session context

# Memory management
yaad decay                   # Apply confidence decay
yaad gc                      # Garbage collect low-confidence nodes
yaad embed <node_id>         # Generate embedding for a node

# Import/Export
yaad export-json             # Export graph as JSON
yaad export-md               # Export as Markdown
yaad export-obsidian <dir>   # Export as Obsidian vault
yaad import-json <file>      # Import from JSON

# Bridge
yaad bridge-import           # Import CLAUDE.md / .cursorrules
yaad bridge-export           # Export conventions to CLAUDE.md

# Skills
yaad skill-store "deploy" "Deploy app" "run tests" "build" "deploy"
yaad skill-list
yaad skill-replay deploy

# Sessions
yaad replay <session_id>     # Show session timeline

# Benchmark
yaad bench                   # Run LongMemEval-style eval

# Servers
yaad mcp                     # Start MCP server (stdio)
yaad serve                   # Start REST server (:3456)
yaad                         # Start both
```

---

## Benchmark

Evaluated on a realistic coding project memory set (13 seeded memories, 5 retrieval questions):

| Metric | Score |
|---|---|
| **R@1** | 60.0% |
| **R@3** | 100.0% |
| **R@5** | 100.0% |
| **R@10** | 100.0% |
| **MRR** | 0.767 |
| **Avg tokens/query** | 81 |
| **Latency** | ~50ms |

Run your own benchmark: `yaad bench`

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              ANY CODING AGENT                                   │
│  Claude Code │ Cursor │ Gemini CLI │ OpenCode │ Cline │ Any     │
└──────┬───────────────┬──────────────────────────┬───────────────┘
       │ MCP (stdio)   │ REST/HTTPS (:3456)       │ gRPC (:3457)
       ▼               ▼                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                       YAAD SERVER                               │
│  MCP │ REST/HTTPS │ gRPC │ SSE │ CLI │ Git Watcher             │
├─────────────────────────────────────────────────────────────────┤
│  Memory Engine: Remember │ Recall │ Context │ Decay │ Compress  │
├─────────────────────────────────────────────────────────────────┤
│  Graph Engine: Relaxed DAG │ BFS │ Impact Analysis │ PageRank   │
├─────────────────────────────────────────────────────────────────┤
│  Storage: SQLite + FTS5 │ Embeddings (optional) │ WAL mode      │
└─────────────────────────────────────────────────────────────────┘

~/.yaad/yaad.db           → Global (preferences, cross-project)
<project>/.yaad/yaad.db   → Project (conventions, decisions, bugs)
```

---

## Configuration

```toml
# <project>/.yaad/config.toml

[server]
port = 3456
grpc_port = 3457
host = "127.0.0.1"
tls = false

[memory]
hot_token_budget = 800
warm_token_budget = 800

[embeddings]
enabled = false          # true for semantic search
provider = "local"       # local | openai | voyage
# api_key_env = "OPENAI_API_KEY"

[decay]
half_life_days = 30
min_confidence = 0.1

[git]
watch = true
```

---

## License

Apache-2.0
