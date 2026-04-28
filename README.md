<div align="center">

# याद Yaad

### Give your coding agent persistent memory.

One config line. Works with any agent. Zero setup.

[![License: MIT](https://img.shields.io/badge/License-MIT-a78bfa.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Pure_Go-no_CGO-00ADD8?logo=go)](go.mod)
[![Tests](https://img.shields.io/badge/Tests-17%2F17-68d391)](integration_test.go)
[![CI](https://img.shields.io/github/actions/workflow/status/GrayCodeAI/yaad/ci.yml?label=ci&logo=github)](https://github.com/GrayCodeAI/yaad/actions)
[![Discord](https://img.shields.io/badge/Discord-GrayCodeAI-5865F2?logo=discord&logoColor=white)](https://discord.gg/UqMbQJRE5)

</div>

---

### The Problem

Every coding agent forgets everything when the session ends.

**Session 1**: You explain your stack, conventions, architecture. Agent writes great code.  
**Session 2**: Agent has forgotten everything. You start over.  
**Session 50**: You've wasted hours re-teaching the same context.

### The Fix

Add one line to your agent's MCP config:

```json
{ "mcpServers": { "yaad": { "command": "yaad", "args": ["mcp"] } } }
```

Now your agent remembers everything — across sessions, across models, across projects.

---

## 30-Second Setup

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/GrayCodeAI/yaad/main/install.sh | sh

# Add to your project
cd your-project && yaad init

# Connect your agent
yaad setup hawk          # or any of 23 supported agents
```

**That's it.** Your agent now has persistent, graph-native memory.

---

## What Happens Next

Your agent starts a session → Yaad injects context from previous sessions:

```markdown
## Project Memory (Yaad)

### Conventions (always follow)
- Use `jose` library, not `jsonwebtoken` (Edge compatibility)
- Named exports only, no default exports
- Run `pnpm test --coverage` before committing

### Active Tasks
- ✓ JWT token issuance endpoint
- → Rate limiting on /auth/token (in progress)

### ⚠ Stale Warnings
- Auth subgraph outdated: src/middleware/auth.ts modified 2h ago

### Previous Session
- Implemented rate limiting skeleton, hit NATS backpressure issue
```

Your agent works → stores decisions, bugs, conventions automatically.  
Session ends → Yaad compresses and links everything in a memory graph.  
Next session → picks up exactly where you left off. **Zero re-explaining.**

---

## Supported Agents

Yaad works with **any** coding agent that supports MCP or HTTP.

<div align="center">

| | Agent | Setup |
|:---:|:---|:---|
| 🦅 | **Hawk** (GrayCodeAI) | `yaad setup hawk` |
| 🤖 | **Claude Code** | `yaad setup claude-code` |
| 🧠 | **Codex CLI** | `yaad setup codex-cli` |
| 💎 | **Gemini CLI** | `yaad setup gemini-cli` |
| 🖱️ | **Cursor** | `yaad setup cursor` |
| 📦 | **OpenCode** | `yaad setup opencode` |
| 🔧 | **Cline** | `yaad setup cline` |
| 🌊 | **Windsurf** | `yaad setup windsurf` |
| ⚡ | **Amp** | `yaad setup amp` |
| 🔢 | **Kilo** | `yaad setup kilo` |
| 🪿 | **Goose** | `yaad setup goose` |
| 🏛️ | **Hermes** | `yaad setup hermes` |
| 🌸 | **Qwen Code** | `yaad setup qwen-code` |
| 🌊 | **Mistral Vibe** | `yaad setup mistral-vibe` |
| ☁️ | **Kiro** (AWS) | `yaad setup kiro` |
| 🐙 | **Copilot CLI** | `yaad setup copilot-cli` |
| 🦘 | **Roo Code** | `yaad setup roo-code` |
| 🍬 | **Crush** | `yaad setup crush` |
| 🚀 | **Warp** | `yaad setup warp` |
| 🔍 | **Augment** | `yaad setup augment` |
| ✏️ | **Zed** | `yaad setup zed` |
| 🤝 | **Aider** | `yaad setup aider` |
| 🔌 | **Any other** | Universal MCP config ↑ |

</div>

---

## How It Works

Yaad is a **memory layer** — it doesn't call LLMs. Your agent handles the LLM. Yaad handles memory.

```
Your Agent                          Yaad
   │                                  │
   ├─ starts session ──────────────▶  │ returns hot-tier context (~2K tokens)
   │                                  │
   ├─ needs context ───────────────▶  │ graph-aware search (BM25 + vector + graph + temporal)
   │  "auth middleware"               │ returns: decisions + conventions + bugs + specs
   │                                  │
   ├─ learns something ────────────▶  │ stores node, extracts entities, links edges
   │  "Use RS256 for JWT"            │ auto-detects: file refs, libraries, functions
   │                                  │
   ├─ ends session ────────────────▶  │ compresses → summary node → links to graph
   │                                  │
   └─ next session ────────────────▶  │ picks up from summary. zero re-explaining.
```

### Under the Hood

**Relaxed DAG** — memories are nodes, relationships are edges:

```
[decision: "Use RS256"] ──led_to──▶ [convention: "Always RS256"]
        │                                     │
        │ led_to                              │ touches
        ▼                                     ▼
[spec: "Auth subsystem"] ◀──part_of── [file: src/middleware/auth.ts]
        │
        │ relates_to
        ▼
[bug: "Token refresh race"] ──supersedes──▶ [bug: "Token expiry (FIXED)"]
```

**Intent-aware retrieval** — "why" queries traverse causal edges, "when" queries traverse temporal edges:

```
"why did we choose NATS?"  → Intent: Why  → boost caused_by, led_to edges
"when did we fix auth?"    → Intent: When → boost temporal backbone
"what is the auth spec?"   → Intent: What → boost spec, part_of edges
```

**4-path search** — BM25 + vector + graph (intent-aware) + temporal recency, fused with RRF.

---

## Memory Types

| Type | What it stores | Example |
|---|---|---|
| 🟢 `convention` | Coding rules & patterns | *"Use jose not jsonwebtoken"* |
| 🔵 `decision` | Architecture choices + why | *"Chose NATS for backpressure"* |
| 🔴 `bug` | Symptom → Cause → Fix | *"Token race → use mutex"* |
| 🟡 `spec` | How a subsystem works | *"Auth: RS256 JWT with jose"* |
| 🟣 `task` | Done / in-progress / blocked | *"✓ auth, → rate limiting"* |
| 🩷 `skill` | Reusable step sequences | *"Deploy: test → build → fly"* |
| 🩵 `preference` | User coding style | *"Functional style, tabs"* |
| ⚪ `file` | File/module anchor | *"src/middleware/auth.ts"* |
| 🔘 `entity` | Auto-extracted entity | *"jose", "PostgreSQL"* |

---

## Key Features

<details>
<summary><b>🧠 Graph-Native Memory (Relaxed DAG)</b></summary>

Not a flat list of memories. A directed graph with 8 edge types:
- **Causal** (acyclic): `led_to`, `supersedes`, `caused_by`, `learned_in`, `part_of`
- **Relational** (cycles OK): `relates_to`, `depends_on`, `touches`

Enables: subgraph extraction, impact analysis, causal chain traversal.
</details>

<details>
<summary><b>🔍 Intent-Aware 4-Path Search</b></summary>

Based on MAGMA (arxiv:2601.03236, 0.700 on LoCoMo):
1. **BM25** (FTS5) — keyword matching
2. **Vector** (optional) — semantic similarity
3. **Graph** (intent-aware BFS) — edge weights boosted by query intent
4. **Temporal** — recency-aware for "when" queries

Fused with Reciprocal Rank Fusion (RRF).
</details>

<details>
<summary><b>⚡ Dual-Stream Ingestion</b></summary>

Based on MAGMA + GAM research:
- **Fast path** (sync): store node + temporal edge, return in <1ms
- **Slow path** (async goroutine): infer causal edges, link entities

Agent is never blocked waiting for memory processing.
</details>

<details>
<summary><b>📊 Git-Aware Staleness</b></summary>

When source files change, Yaad walks the graph backwards to flag entire subgraphs:

*"⚠ Auth subgraph may be stale: src/auth.ts modified 2h ago. Affected: [decision: RS256], [convention: jose], [bug: token refresh]"*
</details>

<details>
<summary><b>💥 Impact Analysis</b></summary>

*"What memories break if I change schema.sql?"* → reverse graph traversal → *"3 decisions + 2 specs + 1 convention affected"*
</details>

<details>
<summary><b>🤝 Team Sync (Git Chunks)</b></summary>

Share memories via git — no server, no merge conflicts:
```bash
yaad sync                    # export chunk + import teammates' chunks
git add .yaad/manifest.json .yaad/chunks/
git push
```
</details>

<details>
<summary><b>🔌 4 Protocol Interfaces</b></summary>

- **MCP** (stdio) — 13 tools, works with any MCP agent
- **REST/HTTPS** — 30+ endpoints on `:3456`
- **gRPC** — port `:3457`, streaming support
- **SSE** — real-time memory events at `/yaad/events`
</details>

<details>
<summary><b>🛡️ Privacy & Security</b></summary>

- API keys, tokens, secrets auto-stripped on ingest
- HTTPS with auto self-signed cert generation
- All data stays local (SQLite, your machine)
- No LLM API calls — Yaad never sends your code anywhere
</details>

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              ANY CODING AGENT                                   │
│  Hawk · Claude Code · Cursor · Gemini CLI · OpenCode · Any     │
└──────┬───────────────┬──────────────────────────┬───────────────┘
       │ MCP (stdio)   │ REST/HTTPS (:3456)       │ gRPC (:3457)
       ▼               ▼                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                       YAAD                                      │
│  Memory Engine · Graph Engine · 4-Path Search · Dual-Stream     │
├─────────────────────────────────────────────────────────────────┤
│  SQLite (pure Go, no CGO) · FTS5 · Embeddings (optional)       │
└─────────────────────────────────────────────────────────────────┘
```

**18MB binary. Zero dependencies. Pure Go. No C compiler. No Docker. No cloud.**

---

## Troubleshooting

```bash
yaad doctor    # diagnose setup issues (DB, server, MCP config, git)
```

---

## Documentation

| Doc | What |
|---|---|
| [PLAN.md](PLAN.md) | Full design document (1,100+ lines) |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Technical architecture (10 components) |
| [COMPARISON.md](COMPARISON.md) | vs Mem0, Letta, Engram, agentmemory |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [CHANGELOG.md](CHANGELOG.md) | Release notes |
| [openapi.yaml](openapi.yaml) | OpenAPI 3.1 spec (30+ endpoints) |

---

## Community

- **Discord**: [GrayCodeAI](https://discord.gg/UqMbQJRE5)
- **Issues**: [GitHub Issues](https://github.com/GrayCodeAI/yaad/issues)
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

---

<div align="center">

**MIT** © 2026 [GrayCodeAI](https://github.com/GrayCodeAI)

*yaad (याद) — Hindi/Urdu for memory, remembrance*

</div>
