<div align="center">

# याद · Yaad

**Model-agnostic, graph-native memory for coding agents**

*"yaad" (Hindi/Urdu) — memory, remembrance*

[![License: MIT](https://img.shields.io/badge/License-MIT-a78bfa.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](go.mod)
[![Pure Go](https://img.shields.io/badge/CGO-disabled-68d391)](Makefile)
[![Tests](https://img.shields.io/badge/Tests-17%2F17-68d391)](integration_test.go)[![R@5](https://img.shields.io/badge/R%405-83%25-f6ad55)](internal/bench/bench.go)
[![CI](https://img.shields.io/github/actions/workflow/status/GrayCodeAI/yaad/ci.yml?label=ci&logo=github)](https://github.com/GrayCodeAI/yaad/actions)

<br/>

> Every coding agent suffers from **session amnesia** — it forgets everything when the session ends.  
> Yaad gives your agent a persistent, graph-native memory that survives sessions, models, and projects.

**One binary · Zero dependencies · Works with every agent**

</div>

---

## ✨ Works With Every Coding Agent

<div align="center">

**GrayCodeAI**

| Agent | Integration | Setup |
|:---:|:---:|:---:|
| 🦅 **Hawk** | MCP | `yaad setup hawk` |

**Big-Lab Native**

| Agent | Integration | Setup |
|:---:|:---:|:---:|
| 🤖 **Claude Code** | MCP + hooks | `yaad setup claude-code` |
| 🧠 **Codex CLI** | MCP | `yaad setup codex-cli` |
| 💎 **Gemini CLI** | MCP | `yaad setup gemini-cli` |
| 🐙 **Copilot CLI** | MCP | `yaad setup copilot-cli` |
| 🌸 **Qwen Code** | MCP | `yaad setup qwen-code` |
| 🌊 **Mistral Vibe** | MCP | `yaad setup mistral-vibe` |
| ☁️ **Kiro** (AWS) | MCP | `yaad setup kiro` |

**IDE / Startup**

| Agent | Integration | Setup |
|:---:|:---:|:---:|
| 🖱️ **Cursor** | MCP | `yaad setup cursor` |
| 🌊 **Windsurf** | MCP | `yaad setup windsurf` |
| ⚡ **Amp** (Sourcegraph) | MCP | `yaad setup amp` |
| 🤖 **Droid** (Factory) | MCP | `yaad setup droid` |
| 🚀 **Warp** | MCP | `yaad setup warp` |
| 🔍 **Augment** | MCP | `yaad setup augment` |

**Open Source / Community**

| Agent | Integration | Setup |
|:---:|:---:|:---:|
| 📦 **OpenCode** | MCP + TS plugin | `yaad setup opencode` |
| 🔧 **Cline** | MCP | `yaad setup cline` |
| 🪿 **Goose** (Block) | MCP | `yaad setup goose` |
| 🦘 **Roo Code** | MCP | `yaad setup roo-code` |
| 🔢 **Kilo** | MCP | `yaad setup kilo` |
| 🍬 **Crush** (Charmbracelet) | MCP | `yaad setup crush` |
| 🏛️ **Hermes** (NousResearch) | MCP | `yaad setup hermes` |
| 🤝 **Aider** | REST API | `yaad setup aider` |
| 🔌 **Any agent** | MCP / REST / gRPC | Universal config ↓ |

</div>

```json
{ "mcpServers": { "yaad": { "command": "yaad", "args": ["mcp"] } } }
```

---

## 🚀 Quick Start

```bash
# Install (pure Go, no C compiler needed)
go install github.com/GrayCodeAI/yaad/cmd/yaad@latest

# Initialize in your project
cd your-project
yaad init

# Generate config for your agent
yaad setup claude-code    # or cursor, gemini-cli, opencode, cline...

# Start Yaad
yaad mcp                  # MCP server (stdio) — for agents
yaad serve                # REST server (:3456) — for HTTP clients
yaad tui                  # Interactive terminal UI
```

---

## 🧠 What Gets Remembered

Yaad uses a **Relaxed DAG** (Labeled Property Graph) — memories are nodes, relationships are edges.

### Node Types

| Type | Color | Description | Example |
|---|:---:|---|---|
| `convention` | 🟢 | Coding rules & patterns | *"Use jose not jsonwebtoken"* |
| `decision` | 🔵 | Architecture choices + rationale | *"Chose NATS over Redis Streams"* |
| `bug` | 🔴 | Symptom → Cause → Fix | *"Token refresh race → use mutex"* |
| `spec` | 🟡 | How a subsystem works | *"Auth uses RS256 JWT with jose"* |
| `task` | 🟣 | Done / in-progress / blocked | *"✓ auth, → rate limiting, ○ tests"* |
| `skill` | 🩷 | Reusable step sequences | *"Deploy: test → build → fly deploy"* |
| `preference` | 🩵 | User coding style & habits | *"Prefers functional style, tabs"* |
| `file` | ⚪ | File/module anchor | *"src/middleware/auth.ts"* |
| `entity` | 🔘 | Auto-extracted entity | *"jose", "PostgreSQL", "AuthService"* |

### Edge Types

```
decision ──led_to──▶ convention ──touches──▶ file
    │                                          │
    └──led_to──▶ spec ◀──part_of── convention
                  │
                  └──relates_to──▶ bug ──supersedes──▶ old_bug
                                    │
                                    └──learned_in──▶ session
```

**Causal edges** (`led_to`, `supersedes`, `caused_by`, `learned_in`, `part_of`) — strictly acyclic  
**Relational edges** (`relates_to`, `depends_on`, `touches`) — cycles allowed (real code has circular deps)

---

## 🔍 How It Works

```
Agent starts session
  → yaad_context returns hot-tier subgraph (~2K tokens)
  → Agent sees: conventions, active tasks, stale warnings, previous session

Agent works
  → yaad_recall "auth middleware"
    → BM25 seed nodes → graph expansion (BFS) → RRF fusion → ranked subgraph
  → yaad_remember "Use RS256 for JWT"
    → privacy filter → dedup → create node → auto-extract entities → link edges

Session ends
  → yaad hook session-end → compress → summary node → next session picks up
```

---

## 📊 Benchmark

> Evaluated on a realistic coding project (12 memories, 12 questions: single-hop, multi-hop, temporal, preference)
> 4-path retrieval: BM25 + vector + graph (intent-aware) + temporal recency, fused with RRF

<div align="center">

| Metric | Score |
|:---:|:---:|
| **R@1** | 25.0% |
| **R@3** | 83.3% |
| **R@5** | 83.3% |
| **R@10** | 83.3% |
| **MRR** | 0.528 |
| **Avg tokens/query** | 76 |
| **Latency** | ~80ms |

</div>

```bash
yaad bench   # run on your own data
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              ANY CODING AGENT                                   │
│  Claude Code │ Cursor │ Gemini CLI │ OpenCode │ Cline │ Any     │
└──────┬───────────────┬──────────────────────────┬───────────────┘
       │ MCP (stdio)   │ REST/HTTPS (:3456)       │ gRPC (:3457)
       ▼               ▼                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                       YAAD SERVER                               │
│  MCP · REST/HTTPS · gRPC · SSE · CLI · Git Watcher · TUI       │
├─────────────────────────────────────────────────────────────────┤
│  Memory Engine: Remember · Recall · Context · Decay · Compress  │
├─────────────────────────────────────────────────────────────────┤
│  Graph Engine: Relaxed DAG · BFS · Impact Analysis · PageRank   │
├─────────────────────────────────────────────────────────────────┤
│  Storage: SQLite (pure Go) + FTS5 · Embeddings (optional)       │
└─────────────────────────────────────────────────────────────────┘

~/.yaad/yaad.db           → Global (preferences, cross-project)
<project>/.yaad/yaad.db   → Project (conventions, decisions, bugs)
```

---

## 🛠️ Interfaces

<details>
<summary><b>MCP Tools (12 tools)</b></summary>

| Tool | Description |
|---|---|
| `yaad_recall` | Graph-aware search: BM25 + graph expansion + vector |
| `yaad_remember` | Store a node with auto-entity-extraction and edge linking |
| `yaad_context` | Get session-start hot-tier subgraph (~2K tokens) |
| `yaad_forget` | Archive a node |
| `yaad_link` | Create edge between nodes |
| `yaad_unlink` | Remove edge |
| `yaad_subgraph` | BFS subgraph around a node |
| `yaad_impact` | "What memories break if I change this file?" |
| `yaad_status` | Health, node/edge counts |
| `yaad_task_update` | Update task node state |
| `yaad_sessions` | List recent sessions |
| `yaad_stale` | Show stale subgraphs (git-aware) |

</details>

<details>
<summary><b>REST API (25+ endpoints)</b></summary>

```
POST   /yaad/remember          POST   /yaad/recall
GET    /yaad/context           GET    /yaad/health
GET    /yaad/graph/stats       POST   /yaad/hybrid-recall
GET    /yaad/proactive         POST   /yaad/feedback
POST   /yaad/decay             POST   /yaad/gc
POST   /yaad/bridge/import     POST   /yaad/bridge/export
GET    /yaad/events            (SSE)
GET    /yaad/replay/{id}       GET    /yaad/ui  (dashboard)
POST   /yaad/export/json       POST   /yaad/export/markdown
POST   /yaad/skill/store       GET    /yaad/skill/list
POST   /yaad/team/share        POST   /yaad/bench
```

</details>

<details>
<summary><b>gRPC (port 3457)</b></summary>

12 unary RPCs + `WatchMemories` streaming. Auto-generated SDKs for Python, TypeScript, Rust, Java.

See [`internal/proto/yaad.proto`](internal/proto/yaad.proto)

</details>

---

## 💻 CLI Commands

```bash
# Core
yaad init                    yaad remember -t convention "..."
yaad recall "auth JWT"       yaad status
yaad link <id1> <id2> led_to yaad subgraph <id>
yaad impact src/auth.ts      yaad tui

# Agent setup
yaad setup hawk              # ← GrayCodeAI's own CLI
yaad setup claude-code       yaad setup cursor
yaad setup gemini-cli        yaad setup opencode
yaad setup codex-cli         yaad setup cline

# Hooks (auto-called by agents)
yaad hook session-start      yaad hook post-tool-use
yaad hook session-end

# Search
yaad hybrid-recall "auth"    yaad proactive

# Memory management
yaad decay                   yaad gc
yaad embed <node_id>

# Import / Export
yaad export-json             yaad export-md
yaad export-obsidian <dir>   yaad import-json <file>

# Bridge
yaad bridge-import           yaad bridge-export

# Skills
yaad skill-store "deploy" "Deploy app" "test" "build" "deploy"
yaad skill-list              yaad skill-replay deploy

# Team sync (git chunks, no merge conflicts)
yaad sync                    yaad sync --status
yaad sync --import

# Sessions
yaad replay <session_id>     yaad bench
```

---

## 🤝 Team Sync

Share memories with your team via git — no server needed, no merge conflicts:

```bash
# Export your memories as a chunk
yaad sync
git add .yaad/manifest.json .yaad/chunks/
git commit -m "sync: add auth memories"
git push

# Teammate imports
git pull
yaad sync --import
```

```
.yaad/
  manifest.json          ← commit this
  chunks/
    a3f8c1d2.jsonl.gz    ← commit these (append-only, never modified)
```

---

## ⚙️ Configuration

```toml
# <project>/.yaad/config.toml

[server]
port = 3456
grpc_port = 3457
tls = false          # true for HTTPS (auto-generates self-signed cert)

[memory]
hot_token_budget = 800
warm_token_budget = 800

[embeddings]
enabled = false      # true for semantic search
provider = "local"   # local | openai | voyage

[decay]
half_life_days = 30
min_confidence = 0.1

[git]
watch = true         # flag stale memories when files change
```

---

## 📁 Project Structure

```
yaad/
├── cmd/yaad/              CLI entrypoint (31 commands)
├── internal/
│   ├── storage/           SQLite + FTS5 (pure Go, no CGO)
│   ├── graph/             Relaxed DAG: BFS, cycle detection, impact analysis
│   ├── engine/            Remember, Recall, Context, Decay, Search, Skills
│   ├── server/            MCP, REST/HTTPS, gRPC, SSE, Web dashboard
│   ├── tui/               Bubbletea terminal UI
│   ├── hooks/             Auto-capture: SessionStart, PostToolUse, SessionEnd
│   ├── bridge/            CLAUDE.md / .cursorrules / AGENTS.md sync
│   ├── sync/              Git chunk sync for team sharing
│   ├── embeddings/        OpenAI / Voyage / local stub providers
│   ├── team/              Namespaced team memory
│   ├── skill/             Procedural memory with step sequences
│   └── exportimport/      JSON / Markdown / Obsidian export
├── plugin/
│   ├── opencode/yaad.ts   TypeScript plugin (auto-start, context injection)
│   └── claude-code/       Bash hooks for Claude Code
├── PLAN.md                Full design document
├── ARCHITECTURE.md        Technical architecture
└── COMPARISON.md          vs Mem0, Letta, Engram, agentmemory, and others
```

---

## 📄 License

[MIT](LICENSE) © 2026 GrayCodeAI
