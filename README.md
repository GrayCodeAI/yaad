<div align="center">

# याद Yaad

### Give your coding agent persistent memory.

One config line. Works with any MCP agent. Zero setup.

[![License: MIT](https://img.shields.io/badge/License-MIT-a78bfa.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Pure_Go-no_CGO-00ADD8?logo=go)](go.mod)
[![Tests](https://img.shields.io/badge/Tests-passing-68d391)](yaad_test.go)
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
go install github.com/GrayCodeAI/yaad/cmd/yaad@latest

# Add to your project
cd your-project && yaad init

# Connect your agent (generates .mcp.json + hooks)
yaad setup
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
| `convention` | Coding rules & patterns | *"Use jose not jsonwebtoken"* |
| `decision` | Architecture choices + why | *"Chose NATS for backpressure"* |
| `bug` | Symptom → Cause → Fix | *"Token race → use mutex"* |
| `spec` | How a subsystem works | *"Auth: RS256 JWT with jose"* |
| `task` | Done / in-progress / blocked | *"✓ auth, → rate limiting"* |
| `skill` | Reusable step sequences | *"Deploy: test → build → fly"* |
| `preference` | User coding style | *"Functional style, tabs"* |
| `file` | File/module anchor | *"src/middleware/auth.ts"* |
| `entity` | Auto-extracted entity | *"jose", "PostgreSQL"* |

---

## Key Features

<details>
<summary><b>Graph-Native Memory (Relaxed DAG)</b></summary>

Not a flat list of memories. A directed graph with 8 edge types:
- **Causal** (acyclic): `led_to`, `supersedes`, `caused_by`, `learned_in`, `part_of`
- **Relational** (cycles OK): `relates_to`, `depends_on`, `touches`

Enables: subgraph extraction, impact analysis, causal chain traversal.
</details>

<details>
<summary><b>Intent-Aware 4-Path Search</b></summary>

Based on MAGMA (arxiv:2601.03236):
1. **BM25** (FTS5) — keyword matching
2. **Vector** (optional) — semantic similarity
3. **Graph** (intent-aware BFS) — edge weights boosted by query intent
4. **Temporal** — recency-aware for "when" queries

Fused with Reciprocal Rank Fusion (RRF).
</details>

<details>
<summary><b>Dual-Stream Ingestion</b></summary>

Based on MAGMA + GAM research:
- **Fast path** (sync): store node + temporal edge, return in <1ms
- **Slow path** (async goroutine): infer causal edges, link entities

Agent is never blocked waiting for memory processing.
</details>

<details>
<summary><b>Git-Aware Staleness</b></summary>

When source files change, Yaad walks the graph backwards to flag stale subgraphs:

*"Auth subgraph may be stale: src/auth.ts modified 2h ago. Affected: [decision: RS256], [convention: jose], [bug: token refresh]"*
</details>

<details>
<summary><b>Impact Analysis</b></summary>

*"What memories break if I change schema.sql?"* → reverse graph traversal → *"3 decisions + 2 specs + 1 convention affected"*
</details>

<details>
<summary><b>Auto-Decay & Compaction</b></summary>

- Half-life decay: unused memories fade automatically
- Compaction: low-confidence memories merge into summaries
- Pinned memories never decay (core architecture decisions, deploy process)
- Auto-decay runs on every session start — zero maintenance
</details>

<details>
<summary><b>Privacy & Security</b></summary>

- API keys, tokens, secrets auto-stripped on ingest (regex + entropy detection)
- Localhost-only binding (127.0.0.1)
- HTTPS with auto self-signed cert generation
- All data stays local (SQLite, your machine)
- No LLM API calls — Yaad never sends your code anywhere
</details>

---

## MCP Tools (23 tools)

Your agent gets these tools automatically via `yaad mcp`:

| Tool | What it does |
|---|---|
| `yaad_remember` | Store a memory (convention, decision, bug, spec, task, skill, preference) |
| `yaad_recall` | Graph-aware search with intent classification |
| `yaad_hybrid_recall` | 4-path search: BM25 + vector + graph + temporal |
| `yaad_context` | Get hot-tier context for session injection |
| `yaad_link` | Create typed edge between memories |
| `yaad_forget` | Archive a memory (sets confidence to 0) |
| `yaad_feedback` | Approve / edit / discard a memory |
| `yaad_pin` | Pin/unpin a memory (pinned = always in context) |
| `yaad_stale` | Find memories invalidated by git changes |
| `yaad_proactive` | Predict what context the agent needs next |
| `yaad_compact` | Merge low-confidence memories into summaries |
| `yaad_mental_model` | Auto-generated project summary |
| `yaad_skill_store` | Save a reusable step sequence |
| `yaad_skill_get` | Retrieve and replay a skill |
| `yaad_session_recap` | Summary of the previous session |
| `yaad_subgraph` | Extract neighborhood around a memory |
| `yaad_impact` | What memories are affected by a file change? |
| `yaad_status` | Graph stats (nodes, edges, sessions) |
| `yaad_decay` | Manually trigger confidence decay |
| `yaad_gc` | Garbage collect archived memories |
| `yaad_embed` | Generate vector embedding for a node |
| `yaad_export` | Export graph as JSON/Markdown/Obsidian |
| `yaad_import` | Import graph from JSON |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              YOUR CODING AGENT                                   │
│  Hawk · Claude Code · Cursor · Gemini CLI · Any MCP Agent       │
└──────┬───────────────┬──────────────────────────────────────────┘
       │ MCP (stdio)   │ REST/HTTPS (127.0.0.1:3456)
       ▼               ▼
┌─────────────────────────────────────────────────────────────────┐
│                       YAAD                                      │
│  Memory Engine · Graph Engine · 4-Path Search · Dual-Stream     │
├─────────────────────────────────────────────────────────────────┤
│  SQLite (WAL mode) · FTS5 · Embeddings (optional)              │
└─────────────────────────────────────────────────────────────────┘
```

**Single binary. Zero dependencies. Pure Go. No CGO. No Docker. No cloud.**

---

## CLI Commands

```bash
yaad init              # Initialize .yaad/ in current project
yaad setup             # Configure MCP + hooks for your agent
yaad serve             # Start REST API server
yaad mcp               # Start MCP server on stdio (used by agents)

yaad remember "..."    # Store a memory
yaad recall "..."      # Search memories
yaad link A B type     # Create edge between nodes
yaad status            # Show graph stats
yaad doctor            # Diagnose setup issues

yaad decay             # Apply confidence decay
yaad gc                # Garbage collect low-confidence nodes
yaad bench             # Run retrieval benchmark

yaad export-json       # Export as JSON
yaad export-md         # Export as Markdown
yaad export-obsidian   # Export as Obsidian vault
yaad import-json       # Import from JSON
```

---

## Configuration

Generated at `.yaad/config.toml`:

```toml
[server]
port = 3456
host = "127.0.0.1"

[memory]
hot_token_budget = 800
warm_token_budget = 800
max_memories = 10000

[search]
bm25_weight = 0.5
vector_weight = 0.5
default_limit = 10

[decay]
enabled = true
half_life_days = 30
min_confidence = 0.1
boost_on_access = 0.2

[git]
watch = true
auto_stale = true
```

---

## Development

```bash
git clone https://github.com/GrayCodeAI/yaad.git
cd yaad
make build           # Build binary
make test            # Run all tests
make install         # Install to $GOPATH/bin
```

---

## Documentation

| Doc | What |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Technical architecture |
| [COMPARISON.md](COMPARISON.md) | vs Mem0, Letta, Engram, agentmemory |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [CHANGELOG.md](CHANGELOG.md) | Release notes |
| [openapi.yaml](openapi.yaml) | OpenAPI spec |

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
