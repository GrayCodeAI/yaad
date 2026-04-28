# Yaad (याद) — Memory for Coding Agents

> **yaad** (Hindi/Urdu): memory, remembrance
> "Your coding agent remembers everything — across sessions, across models, across projects."

## Vision

Yaad is a **model-agnostic, agent-agnostic memory system** purpose-built for coding agents. It gives any coding agent — Claude Code, Cursor, Gemini CLI, OpenCode, Cline, Aider, Codex, Windsurf, or any MCP-compatible client — persistent, intelligent memory that survives session boundaries.

**One binary. Zero dependencies. Works with every agent.**

## Problem

Every coding agent today suffers from **session amnesia**:

- **Session 1**: You explain your stack, architecture, conventions. Agent writes great code.
- **Session 2**: Agent has forgotten everything. You re-explain from scratch.
- **Session 50**: You've wasted hours re-teaching the same context.

Current solutions fail because:

| Solution | Limitation |
|---|---|
| `CLAUDE.md` / `.cursorrules` | 200-line cap, manual, goes stale, single-agent |
| Mem0 (54k ⭐) | General-purpose, not coding-specific, heavy deps (Qdrant/pgvector) |
| Letta (22k ⭐) | Full agent runtime, high lock-in, not a memory layer |
| Engram (2.9k ⭐) | Coding-focused but no tiered memory, no staleness detection |
| agentmemory (2k ⭐) | TypeScript, requires iii-engine runtime |

**None of them** natively understand coding-specific memory types (architecture decisions, bug patterns, conventions, subsystem specs) or detect when memories go stale as code changes.

## Design Principles

1. **Zero friction** — Single binary, `yaad` command, works in 30 seconds
2. **Agent-agnostic** — MCP server + REST API + CLI. Works with any agent
3. **Model-agnostic** — No LLM dependency for core operations. Optional LLM for summarization
4. **Coding-native** — First-class support for code-specific memory types
5. **Local-first** — SQLite + FTS5 + optional local embeddings. Your data stays on your machine
6. **Git-aware** — Detects when code changes make memories stale
7. **Token-efficient** — Tiered loading so agents get the right context without blowing the window
8. **Multi-agent safe** — Leases, scoping, and conflict detection for parallel agents

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        CODING AGENT                             │
│  (Claude Code / Cursor / Gemini CLI / OpenCode / Cline / Any)  │
└──────┬──────────┬──────────────────┬────────────┬───────────────┘
       │ MCP      │ REST/HTTPS       │ gRPC       │ CLI
       │ (stdio)  │ (:3456)          │ (:3457)    │
       ▼          ▼                  ▼            ▼
┌─────────────────────────────────────────────────────────────────┐
│                         YAAD SERVER                             │
│                                                                 │
│  ┌──────────┐ ┌───────────┐ ┌──────────┐ ┌────┐ ┌───────────┐ │
│  │MCP Server│ │REST/HTTPS │ │gRPC      │ │CLI │ │Git Watcher│ │
│  │(stdio)   │ │(:3456)    │ │(:3457)   │ │    │ │(fsnotify) │ │
│  └────┬─────┘ └─────┬─────┘ └────┬─────┘ └─┬──┘ └─────┬─────┘ │
│       └──────────────┴────────────┴─────────┴──────────┘        │
│                          │                                      │
│  ┌───────────────────────▼──────────────────────────────────┐   │
│  │                   MEMORY ENGINE                          │   │
│  │                                                          │   │
│  │  ┌─────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │   │
│  │  │ Ingest  │  │ Retrieve │  │ Compress │  │  Decay   │  │   │
│  │  └─────────┘  └──────────┘  └──────────┘  └──────────┘  │   │
│  │                                                          │   │
│  │  ┌──────────────────────────────────────────────────┐    │   │
│  │  │              HYBRID SEARCH                       │    │   │
│  │  │  BM25 (FTS5) + Vector (optional) + Tag filter    │    │   │
│  │  └──────────────────────────────────────────────────┘    │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          │                                      │
│  ┌───────────────────────▼──────────────────────────────────┐   │
│  │                   GRAPH ENGINE                           │   │
│  │                                                          │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────┐  │   │
│  │  │ DAG Ops  │  │Traversal │  │ Ranking  │  │ Impact  │  │   │
│  │  │+cycle chk│  │+BFS/DFS  │  │+PageRank │  │+file→   │  │   │
│  │  └──────────┘  └──────────┘  └──────────┘  │ graph   │  │   │
│  │                                             └─────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          │                                      │
│  ┌───────────────────────▼──────────────────────────────────┐   │
│  │                   STORAGE (SQLite)                       │   │
│  │                                                          │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐               │   │
│  │  │  Nodes  │  │  Edges  │  │Embeddings│               │   │
│  │  │  (FTS5)  │  │ (graph)  │  │(optional)│               │   │
│  │  └──────────┘  └──────────┘  └──────────┘               │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘

Storage locations:
  ~/.yaad/              → Global memories (user preferences, cross-project)
  <project>/.yaad/      → Project memories (conventions, architecture, bugs)
```

## Memory Model — Relaxed DAG (Labeled Property Graph in SQLite)

Memories are **nodes** in a graph. Relationships are **directed, labeled edges with properties**. This is a Labeled Property Graph (LPG) stored in SQLite — no Neo4j needed.

**Relaxed DAG** means:
- **Causal edges** (`led_to`, `supersedes`, `caused_by`, `learned_in`, `part_of`) → **strictly acyclic** (DAG enforced)
- **Relational edges** (`relates_to`, `depends_on`, `touches`) → **cycles allowed** (real code has circular deps)

This gives us clean causality chains AND real-world flexibility.

### Why DAG, Not Flat Records

```
FLAT (what others do):          DAG (what Yaad does):
┌──────────┐                    
│ memory 1 │                    [decision: use NATS]
│ memory 2 │                         │
│ memory 3 │                    ┌────┴─────┐
│ memory 4 │                    ▼          ▼
│   ...    │              [convention:  [bug: NATS
└──────────┘               use NATS      reconnect
  No relationships.        client]       timeout]
  No context.                  │          │
  Just a pile.                 ▼          ▼
                          [spec: event  [fix: set
                           bus arch]     keepalive]
```

**Benefits of DAG:**
- **Traversal**: "Show me everything related to auth" → walk the graph
- **Impact analysis**: "If I change the DB schema, what memories are affected?"
- **Dependency chains**: Task A blocks Task B blocks Task C
- **Supersession**: New fix → old fix (without deleting history)
- **Context assembly**: Pull a subgraph of related memories, not random matches

### Node Types (Memory Types)

| Type | Tag | Description | Example | Default Tier |
|---|---|---|---|---|
| **Convention** | `convention` | Coding rules, patterns, style | "Use jose not jsonwebtoken for Edge compat" | Hot |
| **Decision** | `decision` | Architecture choices + rationale | "Chose NATS over Redis Streams for backpressure" | Warm |
| **Bug Pattern** | `bug` | Symptom → Cause → Fix | "Desync on crits → local time → use GetSyncedTime()" | Warm |
| **Subsystem Spec** | `spec` | How a subsystem works | Save system 2-tier architecture doc | Cold |
| **Task State** | `task` | What's done / in-progress / blocked | "✓ auth endpoint, / rate limiting, ○ tests" | Hot |
| **Preference** | `preference` | User coding style & habits | "Prefers functional style, uses tabs" | Hot (global) |
| **Session** | `session` | Session summary node | "Implemented JWT auth, fixed token refresh bug" | Cold |
| **File** | `file` | File/module anchor node | "src/middleware/auth.ts" | — (anchor only) |
| **Entity** | `entity` | Auto-extracted entity (lib, function, person, service) | "jose", "PostgreSQL", "AuthService" | — (anchor only) |

### Edge Types (Relationships)

| Edge | Meaning | Example | Cycles? |
|---|---|---|---|
| `caused_by` | A was caused by B | bug ←caused_by← decision | ❌ Acyclic |
| `led_to` | A led to creating B | decision →led_to→ convention | ❌ Acyclic |
| `supersedes` | A replaces B | new_fix →supersedes→ old_fix | ❌ Acyclic |
| `learned_in` | Memory was learned in session | bug →learned_in→ session | ❌ Acyclic |
| `part_of` | A is part of B | convention →part_of→ spec | ❌ Acyclic |
| `relates_to` | A is related to B | spec ←relates_to→ spec | ✅ Allowed |
| `depends_on` | A depends on B | task →depends_on→ task | ✅ Allowed |
| `touches` | Memory relates to file | convention →touches→ file | ✅ Allowed |

Cycle detection runs **only** on acyclic edge types during insert. Relational edges skip the check.

### DAG Example: Real Project

```
[session:2024-01-15]
    │ learned_in
    ▼
[decision: "Use RS256 over HS256"]──led_to──▶[convention: "Always RS256"]
    │                                              │
    │ led_to                                       │ touches
    ▼                                              ▼
[spec: "Auth subsystem"]◀──part_of──[convention: "Use jose library"]
    │                                              │
    │ relates_to                                   │ touches
    ▼                                              ▼
[bug: "Token refresh race"]              [file: src/middleware/auth.ts]
    │
    │ supersedes
    ▼
[bug: "Token expiry off-by-one (FIXED)"]
    │
    │ led_to
    ▼
[task: "Add token refresh integration tests"]
    │ depends_on
    ▼
[task: "Set up test fixtures for auth"]
```

### Three Tiers (Hot / Warm / Cold)

Tiers control **loading strategy**, not storage. Every node lives in the DAG; tiers decide when it enters the context window.

```
TIER 1: HOT — Always injected at session start (<2K tokens)
  ├── Conventions (high-confidence)
  ├── Build / test / lint commands
  ├── Active tasks (not completed)
  ├── User preferences
  └── Computed: nodes with highest PageRank in project subgraph

TIER 2: WARM — Loaded per-task via graph traversal
  ├── Decisions + rationale (neighbors of relevant files)
  ├── Bug patterns (connected to touched files)
  ├── Recent session summaries
  └── Computed: BFS from query-matched nodes, depth ≤ 2

TIER 3: COLD — Retrieved on-demand via search
  ├── Full subsystem specs
  ├── Old session summaries
  ├── Archived/superseded memories
  └── Computed: full-graph search, any depth
```

### Graph-Powered Retrieval

```
Query: "auth middleware"
  │
  ├──1. Text search (BM25/FTS5) → find matching nodes
  │
  ├──2. Graph expansion (BFS, depth=2) → pull connected nodes
  │     "auth middleware" matches [spec: Auth subsystem]
  │       → neighbors: [decision: RS256], [convention: jose], [bug: token refresh]
  │       → depth 2: [file: auth.ts], [task: refresh tests]
  │
  ├──3. Subgraph ranking → PageRank within subgraph
  │
  ├──4. Token budget trim → fit into budget
  │
  └──5. Return: nodes + edges (agent sees relationships)
```

### Memory Lifecycle (DAG-Aware)

```
  Observation (raw input)
       │
       ▼
  ┌─────────┐     ┌──────────┐     ┌───────────────┐
  │ Capture │ ──▶ │ Classify │ ──▶ │ Create Node   │
  └─────────┘     │ (type)   │     │ + Link Edges  │
                  └──────────┘     └───────────────┘
                                         │
                  ┌──────────────────────┤
                  ▼                      ▼
           ┌───────────┐         ┌──────────┐
           │  Traverse │         │  Decay   │
           │  (recall) │         │ (prune)  │
           └───────────┘         └──────────┘
                  │                     │
                  ▼                     ▼
           ┌───────────┐         ┌──────────┐
           │  Inject   │         │  Archive │
           │ (subgraph)│         │ (detach) │
           └───────────┘         └──────────┘

  Promotion pipeline:
    observation → node → linked subgraph → team convention (via git commit)

  Decay:
    - Nodes lose confidence over time (half-life)
    - Accessing a node boosts it + connected nodes
    - Orphan nodes (no edges) decay faster
    - Superseded nodes decay 2x faster
```

### Staleness Detection (Git-Aware + Graph-Aware)

```
On session start:
  1. Read recent git commits since last session
  2. Find file nodes whose source changed
  3. Walk graph BACKWARDS from changed files → find affected memories
  4. Flag entire subgraph as potentially stale
  5. Inject: "⚠ Auth subgraph may be stale (src/auth.ts changed 2h ago)"

This is much more powerful than flat staleness:
  - Flat: "memory X is stale"
  - DAG:  "auth decision, 3 conventions, and 1 bug pattern may be stale
           because src/middleware/auth.ts was modified"
```

## Interfaces

### 1. MCP Server (Primary — works with any MCP client)

```
Tools:
  yaad_recall          — Graph-aware search (BM25 + traversal + vector)
  yaad_remember        — Store a node with type, metadata, and edges
  yaad_context         — Get session-start context (hot tier subgraph)
  yaad_forget          — Archive/detach a node from the graph
  yaad_link            — Create edge between two nodes
  yaad_unlink          — Remove edge between two nodes
  yaad_subgraph        — Get subgraph around a node (BFS, configurable depth)
  yaad_impact          — "What's affected if I change file X?"
  yaad_status          — Health, node/edge count, staleness warnings
  yaad_task_update     — Update task node state + dependency edges
  yaad_sessions        — List recent sessions
  yaad_stale           — Show stale subgraphs (git-aware)

Resources:
  yaad://context              — Hot tier subgraph for current project
  yaad://graph/stats          — Node/edge counts, connected components
  yaad://stale                — Stale subgraph warnings

Prompts:
  recall_context       — Search + expand subgraph + format for injection
  session_handoff      — Generate handoff summary with graph context
```

### 2. REST API (HTTP/HTTPS)

Default: HTTP on `127.0.0.1:3456` (local dev).
HTTPS: Enable with `--tls` flag or config for remote/team use.

```
POST   /yaad/remember        — Create node (+ optional edges)
POST   /yaad/recall          — Graph-aware search
GET    /yaad/context          — Get hot-tier subgraph
POST   /yaad/link            — Create edge between nodes
DELETE /yaad/link/:id         — Remove edge
GET    /yaad/node/:id         — Get node + neighbors
GET    /yaad/subgraph/:id     — Get subgraph (BFS, ?depth=2)
GET    /yaad/impact/:file     — Impact analysis for file
DELETE /yaad/forget/:id       — Archive node
GET    /yaad/health           — Health check
GET    /yaad/graph/stats      — Node/edge counts, components
GET    /yaad/sessions         — List sessions
POST   /yaad/session/start    — Start session (returns context subgraph)
POST   /yaad/session/end      — End session (trigger compression)
GET    /yaad/stale            — Staleness report (subgraph-aware)
```

HTTPS config:
```toml
[server]
tls = true
cert_file = "~/.yaad/cert.pem"
key_file = "~/.yaad/key.pem"
# Or auto-generate self-signed: yaad init --tls
```

### 3. gRPC API

Port `3457`. Strongly typed protobuf contracts with bi-directional streaming.

```protobuf
service Yaad {
  // Core CRUD
  rpc Remember(RememberRequest)     returns (Node);
  rpc Recall(RecallRequest)         returns (RecallResponse);
  rpc Context(ContextRequest)       returns (ContextResponse);
  rpc Forget(ForgetRequest)         returns (Empty);

  // Graph operations
  rpc Link(LinkRequest)             returns (Edge);
  rpc Unlink(UnlinkRequest)         returns (Empty);
  rpc Subgraph(SubgraphRequest)     returns (SubgraphResponse);
  rpc Impact(ImpactRequest)         returns (ImpactResponse);

  // Sessions
  rpc SessionStart(SessionStartReq) returns (ContextResponse);
  rpc SessionEnd(SessionEndReq)     returns (SessionSummary);

  // Streaming (real-time memory updates)
  rpc WatchMemories(WatchRequest)   returns (stream MemoryEvent);
  rpc WatchStale(WatchStaleReq)     returns (stream StaleEvent);

  // System
  rpc Health(Empty)                 returns (HealthResponse);
  rpc GraphStats(Empty)             returns (GraphStatsResponse);
}
```

**Why gRPC:**
- Auto-generated client SDKs (Python, TypeScript, Rust, Java)
- Bi-directional streaming for real-time memory events
- ~10x faster than REST for high-frequency calls
- Strongly typed — no string parsing errors

### 4. CLI

```bash
yaad                          # Start server (MCP + REST)
yaad init                     # Initialize .yaad/ in current project
yaad recall "auth middleware"  # Graph-aware search
yaad remember "Use jose"      # Store a node
yaad link <id1> <id2> <type>  # Create edge between nodes
yaad subgraph <id> --depth 2  # Show subgraph around a node
yaad impact <file>            # What memories are affected by this file?
yaad status                   # Show graph stats (nodes, edges, components)
yaad stale                    # Show stale subgraphs
yaad export                   # Export graph as JSON (nodes + edges)
yaad import <file>            # Import graph
yaad gc                       # Garbage collect decayed/orphan nodes
yaad history <id>             # Show node version history
yaad rollback <id> <version>  # Rollback node to previous version
yaad provenance <id>          # Trace node back to source session
yaad viz                      # Open graph visualization in browser (future)
```

## Agent Integration — How Yaad Works With Any Coding Agent

Yaad runs as a **local background server** exposing MCP + REST + CLI. Every coding agent can integrate at one of four levels:

```
Level 1: MCP (best — full bi-directional)
  Agent natively calls Yaad tools (recall, remember, context, etc.)
  Works with: Claude Code, Cursor, Gemini CLI, OpenCode, Cline,
              Codex CLI, Windsurf, Goose, Roo Code, Kilo Code

Level 2: gRPC (high-performance — typed SDK + streaming)
  Agent uses auto-generated SDK, gets real-time memory events
  Works with: custom agents, CI/CD pipelines, agent frameworks

Level 3: REST/HTTPS (universal — any HTTP client)
  Agent calls localhost:3456 endpoints (HTTPS for remote/team)
  Works with: Aider, custom agents, scripts, anything

Level 4: CLI + file (simple — read-only context)
  `yaad context > .yaad/context.md`, agent reads the file
  Works with: any agent that can read files

Level 5: Agent file bridge (zero-config)
  Yaad writes hot-tier to CLAUDE.md / .cursorrules
  Agent reads its own native memory file, no MCP needed
  Works with: agents that don't support MCP at all
```

### Session Lifecycle (What Actually Happens)

```
SETUP (once, 30 seconds):
  $ yaad init                    # creates .yaad/ in project
  $ yaad                         # starts server on localhost:3456
  + add MCP config to agent      # one JSON block

SESSION FLOW:
  ┌─────────────────────────────────────────────────────────┐
  │ 1. SESSION START                                        │
  │    Agent calls: yaad_context                            │
  │    Yaad returns: hot-tier subgraph (~2K tokens)         │
  │    ├── Conventions: "Use jose, not jsonwebtoken"        │
  │    ├── Commands: "pnpm test before commit"              │
  │    ├── Active task: "rate limiting (in progress)"       │
  │    └── ⚠ "auth.ts changed 2h ago, auth subgraph stale" │
  │                                                         │
  │ 2. MID-SESSION (agent works)                            │
  │    Need context → yaad_recall "auth middleware"          │
  │    Yaad returns: auth subgraph (decisions+bugs+specs)   │
  │                                                         │
  │    Learn something → yaad_remember                      │
  │    "Chose NATS over Redis Streams for backpressure"     │
  │    Yaad: creates node, extracts entities, links edges   │
  │                                                         │
  │    Check impact → yaad_impact "schema.sql"              │
  │    Yaad returns: "3 decisions + 2 specs affected"       │
  │                                                         │
  │ 3. SESSION END                                          │
  │    Hook/agent calls: session end                        │
  │    Yaad: compresses session → session node → links all  │
  └─────────────────────────────────────────────────────────┘
```

### What The Agent Sees (Injected Context)

When an agent calls `yaad_context`, it receives:

```markdown
## Project Memory (Yaad)

### Conventions (always follow)
- Use `jose` library, not `jsonwebtoken` (Edge compatibility)
- Named exports only, no default exports
- Run `pnpm test --coverage` before committing

### Active Tasks
- ✓ JWT token issuance endpoint
- → Rate limiting on /auth/token (in progress)
- ○ Integration tests for auth flow

### Recent Decisions
- Chose RS256 over HS256 for compliance (2 days ago)
- NATS over Redis Streams for event bus (5 days ago)

### ⚠ Stale Warnings
- Auth subgraph outdated: src/middleware/auth.ts modified 2h ago
  Affected: [decision: RS256], [convention: jose], [bug: token refresh]

### Previous Session
- Implemented rate limiting skeleton, hit NATS backpressure issue
```

### Per-Agent Setup

**Claude Code** (MCP + auto-capture hooks):
```jsonc
// .mcp.json
{
  "mcpServers": {
    "yaad": { "command": "yaad", "args": ["mcp"] }
  }
}
```
```jsonc
// .claude/hooks.json (optional — enables auto-capture)
{
  "hooks": {
    "SessionStart": [{ "command": "yaad hook session-start" }],
    "PostToolUse":  [{ "command": "yaad hook post-tool-use" }],
    "SessionEnd":   [{ "command": "yaad hook session-end" }]
  }
}
```

**Cursor**:
```jsonc
// ~/.cursor/mcp.json
{
  "mcpServers": {
    "yaad": { "command": "yaad", "args": ["mcp"] }
  }
}
```

**Gemini CLI**:
```bash
gemini mcp add yaad -- yaad mcp
```

**OpenCode**:
```jsonc
// opencode.json
{
  "mcp": {
    "yaad": { "type": "local", "command": ["yaad", "mcp"], "enabled": true }
  }
}
```

**Codex CLI**:
```yaml
# .codex/config.yaml
mcp_servers:
  yaad:
    command: yaad
    args: ["mcp"]
```

**Cline / Windsurf / Goose / Roo Code / Kilo Code**:
```jsonc
// MCP settings (varies by agent)
{
  "mcpServers": {
    "yaad": { "command": "yaad", "args": ["mcp"] }
  }
}
```

**Aider** (REST — no MCP support):
```bash
# Generate context file, pass to aider
yaad context --format=markdown > .yaad/context.md
aider --read .yaad/context.md
```

**Any custom agent** (REST API):
```python
import requests
BASE = "http://localhost:3456"

# Session start → get context
ctx = requests.post(f"{BASE}/yaad/session/start",
    json={"project": ".", "agent": "my-agent"}).json()

# Search graph
results = requests.post(f"{BASE}/yaad/recall",
    json={"query": "auth middleware", "depth": 2}).json()

# Store memory
requests.post(f"{BASE}/yaad/remember",
    json={"type": "decision", "content": "Use RS256 for JWT"})
```

## Storage Schema (Graph in SQLite)

The DAG is stored in SQLite using an adjacency list pattern — simple, fast, no external graph DB needed.

```sql
-- Nodes (memories)
CREATE TABLE nodes (
  id           TEXT PRIMARY KEY,
  type         TEXT NOT NULL,        -- convention|decision|bug|spec|task|preference|session|file|entity
  content      TEXT NOT NULL,
  content_hash TEXT NOT NULL,        -- SHA-256 for deduplication
  summary      TEXT,                 -- compressed version for hot/warm loading
  scope        TEXT NOT NULL,        -- global|project
  project      TEXT,                 -- project path (null for global)
  tier         INTEGER DEFAULT 2,   -- 1=hot, 2=warm, 3=cold
  tags         TEXT,                 -- comma-separated
  confidence   REAL DEFAULT 1.0,    -- decays over time
  access_count INTEGER DEFAULT 0,
  created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
  accessed_at  DATETIME,
  source_session TEXT,
  source_agent   TEXT,
  version      INTEGER DEFAULT 1
);
CREATE UNIQUE INDEX idx_nodes_hash ON nodes(content_hash, scope, project);  -- dedup

-- Edges (relationships between nodes)
CREATE TABLE edges (
  id        TEXT PRIMARY KEY,
  from_id   TEXT NOT NULL,
  to_id     TEXT NOT NULL,
  type      TEXT NOT NULL,           -- caused_by|led_to|supersedes|relates_to|depends_on|touches|learned_in|part_of
  acyclic   BOOLEAN NOT NULL,        -- true for causal edges, false for relational
  weight    REAL DEFAULT 1.0,        -- edge strength (for ranking)
  metadata  TEXT,                    -- optional JSON properties
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (from_id) REFERENCES nodes(id),
  FOREIGN KEY (to_id) REFERENCES nodes(id)
);

-- Relaxed DAG: unique edge constraint + indexes
CREATE UNIQUE INDEX idx_edges_unique ON edges(from_id, to_id, type);
CREATE INDEX idx_edges_from ON edges(from_id);
CREATE INDEX idx_edges_to ON edges(to_id);

-- Full-text search on nodes
CREATE VIRTUAL TABLE nodes_fts USING fts5(
  content, summary, tags,
  content=nodes, content_rowid=rowid
);

-- Optional vector embeddings
CREATE TABLE embeddings (
  node_id TEXT PRIMARY KEY,
  vector  BLOB,
  model   TEXT,
  FOREIGN KEY (node_id) REFERENCES nodes(id)
);

-- Session tracking
CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  project    TEXT,
  started_at DATETIME,
  ended_at   DATETIME,
  summary    TEXT,
  agent      TEXT
);

-- File→node staleness tracking
CREATE TABLE file_watch (
  file_path TEXT,
  node_id   TEXT,
  git_hash  TEXT,                    -- hash when link was created
  FOREIGN KEY (node_id) REFERENCES nodes(id)
);
CREATE INDEX idx_file_watch_path ON file_watch(file_path);

-- Node version history
CREATE TABLE node_versions (
  node_id    TEXT,
  version    INTEGER,
  content    TEXT,
  changed_at DATETIME,
  changed_by TEXT,
  reason     TEXT,
  PRIMARY KEY (node_id, version)
);
```

### Graph Queries (SQL)

```sql
-- Get all neighbors of a node (depth 1)
SELECT n.* FROM nodes n
JOIN edges e ON (e.to_id = n.id AND e.from_id = :id)
            OR (e.from_id = n.id AND e.to_id = :id);

-- BFS traversal (depth 2) — done in Go with recursive CTE
WITH RECURSIVE subgraph(id, depth) AS (
  SELECT :start_id, 0
  UNION ALL
  SELECT CASE WHEN e.from_id = s.id THEN e.to_id ELSE e.from_id END, s.depth + 1
  FROM subgraph s
  JOIN edges e ON e.from_id = s.id OR e.to_id = s.id
  WHERE s.depth < :max_depth
)
SELECT DISTINCT n.* FROM nodes n
JOIN subgraph s ON n.id = s.id;

-- Impact analysis: what's affected if a file changes?
WITH RECURSIVE affected(id, depth) AS (
  SELECT node_id, 0 FROM file_watch WHERE file_path = :changed_file
  UNION ALL
  SELECT e.from_id, a.depth + 1
  FROM affected a
  JOIN edges e ON e.to_id = a.id
  WHERE a.depth < 3
)
SELECT DISTINCT n.* FROM nodes n JOIN affected a ON n.id = a.id;

-- Relaxed DAG: cycle check ONLY for acyclic edge types
-- (caused_by, led_to, supersedes, learned_in, part_of)
-- Relational edges (relates_to, depends_on, touches) skip this check
WITH RECURSIVE ancestors(id) AS (
  SELECT :from_id
  UNION ALL
  SELECT e.from_id FROM ancestors a
  JOIN edges e ON e.to_id = a.id AND e.acyclic = 1
)
SELECT 1 FROM ancestors WHERE id = :to_id;  -- if returns row → cycle!
```

## Entity Extraction (Auto-Linking)

When a memory is ingested, Yaad auto-extracts entities and creates/links `entity` and `file` nodes:

```
Input: "Use jose instead of jsonwebtoken for Edge compatibility in src/middleware/auth.ts"

Auto-extracted:
  [entity: "jose"]           ←touches── [convention: "Use jose..."]
  [entity: "jsonwebtoken"]   ←touches── [convention: "Use jose..."]
  [file: "src/middleware/auth.ts"] ←touches── [convention: "Use jose..."]
```

Extraction is regex + heuristic-based (no LLM needed):
- **Files**: paths matching `src/`, `.ts`, `.go`, `.py`, etc.
- **Libraries**: known package patterns, import statements
- **Functions/classes**: `camelCase()`, `PascalCase`, `snake_case()`
- **Services**: URLs, ports, service names

Optional LLM-based extraction for richer entity detection (Phase 3+).

## Deduplication

On ingest, content is SHA-256 hashed. If a node with the same hash + scope + project exists:
- **Exact duplicate**: skip, boost confidence of existing node
- **Near duplicate** (same entity/file references, similar content): merge, keep higher-confidence version
- **Update**: if explicitly marked as update, create new version via `node_versions`

## CLAUDE.md / Agent Memory Bridge

Yaad reads and syncs with existing agent memory files:

```
Supported files:
  CLAUDE.md          → import as convention/decision nodes
  .cursorrules       → import as convention nodes
  AGENTS.md          → import as convention nodes
  .augment-guidelines → import as convention nodes

Sync modes:
  import   — one-time import into Yaad graph
  watch    — auto-import on file change
  export   — write hot-tier conventions back to CLAUDE.md
  bidirect — two-way sync (Yaad ↔ CLAUDE.md)
```

This means Yaad **enhances** existing workflows, doesn't replace them.

## Privacy & Security

```
Ingest pipeline:
  1. Strip API keys, tokens, secrets (regex patterns)
  2. Strip .env values, private keys
  3. Replace with placeholders: [REDACTED_API_KEY]
  4. Never store raw credentials in memory DB

Patterns detected:
  - API keys:    sk-*, AKIA*, ghp_*, ghu_*, etc.
  - Tokens:      Bearer *, JWT strings
  - Env vars:    PASSWORD=*, SECRET=*, KEY=*
  - Private keys: -----BEGIN * PRIVATE KEY-----
```

## Audit Trail & Provenance

Every node tracks its origin (`source_session`, `source_agent` in nodes table) and full version history (`node_versions` table in schema above).

This enables:
- `yaad history <id>` — see how a node evolved over time
- `yaad rollback <id> <version>` — undo a bad update
- `yaad provenance <id>` — trace back to the session/agent that created it

## Search Strategy — Graph-Aware Hybrid

### Three-Stage Retrieval

```
Query: "auth middleware JWT"
  │
  ├──STAGE 1: Seed nodes (find entry points)
  │   ├── BM25 (FTS5)     → keyword-matched nodes
  │   ├── Vector (cosine)  → semantically similar nodes (optional)
  │   └── Tag filter       → filter by type, tier, project
  │
  ├──STAGE 2: Graph expansion (BFS from seed nodes)
  │   ├── Depth 1: direct neighbors (edges)
  │   ├── Depth 2: neighbors of neighbors
  │   └── Edge-type weighting (led_to > relates_to > learned_in)
  │
  ├──STAGE 3: Subgraph ranking
  │   ├── RRF fusion of text score + graph centrality
  │   ├── Boost: nodes with more inbound edges rank higher
  │   ├── Boost: recently accessed nodes rank higher
  │   └── Token budget trim → fit into budget
  │
  └──RETURN: Ranked nodes + edges (agent sees relationships)
```

This is fundamentally better than flat search:
- **Flat search**: "auth middleware" → 5 unrelated memories
- **Graph search**: "auth middleware" → auth subgraph with decisions, conventions, bugs, files, all connected

## Token Budget Management

```
Session start injection budget: ~2,000 tokens (configurable)

Allocation:
  Hot tier (conventions, commands, active tasks):  ~800 tokens
  Warm tier (relevant decisions, bug patterns):    ~800 tokens
  Staleness warnings:                              ~200 tokens
  Session summary (previous):                      ~200 tokens
```

Agents can request more context on-demand via `yaad_recall` for cold-tier retrieval.

## Multi-Agent Support

```
Scoping:
  - Global scope:  ~/.yaad/yaad.db         (shared across all projects)
  - Project scope: <project>/.yaad/yaad.db (shared across agents in project)

Coordination:
  - Read-safe: Multiple agents can read simultaneously
  - Write-safe: SQLite WAL mode for concurrent access
  - Conflict detection: Supersedes chain tracks memory evolution
  - Session isolation: Each agent session tracked independently
```

## Configuration

```toml
# ~/.yaad/config.toml (global) or <project>/.yaad/config.toml (project)

[server]
port = 3456
grpc_port = 3457
host = "127.0.0.1"
tls = false
cert_file = ""
key_file = ""

[memory]
hot_token_budget = 800
warm_token_budget = 800
max_memories = 10000

[search]
bm25_weight = 0.5
vector_weight = 0.5
default_limit = 10

[embeddings]
enabled = false                    # true to enable vector search
provider = "local"                 # local | openai | voyage
model = "all-MiniLM-L6-v2"        # for local ONNX

[decay]
enabled = true
half_life_days = 30                # memories lose confidence over time
min_confidence = 0.1               # below this, eligible for GC
boost_on_access = 0.2              # accessing a memory boosts confidence

[git]
watch = true                       # monitor git for staleness
auto_stale = true                  # auto-flag stale memories

[llm]
enabled = false                    # optional: for compression/summarization
provider = "openai"
model = "gpt-4.1-mini"
api_key_env = "OPENAI_API_KEY"     # read from env var
```

## Project Structure

```
yaad/
├── cmd/
│   └── yaad/
│       └── main.go               # CLI entrypoint
├── internal/
│   ├── server/
│   │   ├── mcp.go                # MCP server (stdio + SSE)
│   │   ├── rest.go               # REST/HTTPS API server
│   │   └── grpc.go               # gRPC server
│   ├── proto/
│   │   └── yaad.proto            # Protobuf service definition
│   ├── graph/
│   │   ├── dag.go                # DAG operations (add node, add edge, cycle check)
│   │   ├── traverse.go           # BFS/DFS traversal, subgraph extraction
│   │   ├── rank.go               # PageRank, centrality scoring
│   │   └── impact.go             # Impact analysis (file change → affected nodes)
│   ├── engine/
│   │   ├── memory.go             # Core memory CRUD (node + edge wrappers)
│   │   ├── search.go             # Graph-aware hybrid search (BM25 + graph + vector)
│   │   ├── tiers.go              # Hot/warm/cold tier management
│   │   ├── decay.go              # Confidence decay + GC (graph-aware)
│   │   ├── compress.go           # Memory compression / summarization
│   │   ├── dedup.go              # SHA-256 dedup + near-duplicate detection
│   │   └── entities.go           # Auto entity extraction (files, libs, functions)
│   ├── bridge/
│   │   └── agentfiles.go         # CLAUDE.md / .cursorrules / AGENTS.md sync
│   ├── storage/
│   │   ├── sqlite.go             # SQLite + FTS5 storage
│   │   └── embeddings.go         # Optional vector storage
│   ├── git/
│   │   └── watcher.go            # Git-aware staleness detection
│   ├── privacy/
│   │   └── filter.go             # Secret/key stripping on ingest
│   └── config/
│       └── config.go             # Configuration loading
├── PLAN.md                        # This file
├── README.md
├── go.mod
├── go.sum
├── Makefile
└── LICENSE                        # MIT
```

## Roadmap

### Phase 1: Foundation (MVP)
- [ ] Go project scaffold + CLI framework
- [ ] SQLite storage with FTS5
- [ ] DAG core: nodes, edges, cycle detection (relaxed DAG)
- [ ] Graph traversal: BFS, subgraph extraction
- [ ] Core CRUD (remember node, link edges, recall, forget)
- [ ] BM25 search + graph expansion (seed → traverse → rank)
- [ ] Hot/warm/cold tier system
- [ ] Entity extraction (regex/heuristic — files, libs, functions)
- [ ] Deduplication (SHA-256 content hash)
- [ ] MCP server (stdio transport)
- [ ] REST API (HTTP, localhost)
- [ ] CLI commands (init, recall, remember, link, subgraph, status)
- [ ] Session tracking
- [ ] Privacy filtering (strip secrets/keys on ingest)
- [ ] Basic `yaad` command to start server

### Phase 2: Intelligence
- [ ] HTTPS/TLS support (--tls flag, cert config)
- [ ] gRPC server + protobuf definitions
- [ ] gRPC streaming (WatchMemories, WatchStale)
- [ ] Git-aware staleness detection (graph-propagated)
- [ ] Memory decay + garbage collection (graph-aware)
- [ ] Confidence scoring (boost on access + connected nodes)
- [ ] Contradiction detection (supersedes chain)
- [ ] Session-end compression/summarization
- [ ] Token budget management
- [ ] Memory promotion pipeline
- [ ] Audit trail & version history (node_versions)
- [ ] Memory rollback
- [ ] CLAUDE.md / .cursorrules / AGENTS.md bridge (import/export/sync)
- [ ] Memory feedback/correction API (approve/edit/discard)

### Phase 3: Search & Retrieval
- [ ] Local embeddings (ONNX, all-MiniLM-L6-v2)
- [ ] Hybrid search with RRF fusion
- [ ] Optional cloud embedding providers (OpenAI, Voyage)
- [ ] Contextual re-ranking (graph centrality + recency + confidence)
- [ ] LLM-based entity extraction (richer than regex)
- [ ] Proactive context prediction (pre-load likely-needed subgraphs)

### Phase 4: Agent Ecosystem
- [ ] Auto-capture hooks (SessionStart, PostToolUse, SessionEnd, etc.)
- [ ] Claude Code plugin/hook integration
- [ ] Cursor MCP config generator
- [ ] Gemini CLI integration guide
- [ ] OpenCode plugin
- [ ] Session replay
- [ ] WebSocket/SSE streaming for real-time memory updates

### Phase 5: Team & Scale
- [ ] Team memory sharing (namespaced)
- [ ] Skill/procedural memory (replayable workflows)
- [ ] Memory import/export (JSON, Markdown)
- [ ] Obsidian vault export
- [ ] Web viewer/dashboard (graph visualization)
- [ ] Multi-project memory linking
- [ ] Multi-modal memory (images, diagrams)
- [ ] Benchmark suite (LongMemEval, LoCoMo)

## Competitive Positioning

```
                    Coding-Specific
                         ▲
                         │
              Engram ●   │   ● Yaad (target)
          agentmemory ●  │
                         │
  Lightweight ───────────┼──────────── Feature-Rich
                         │
                 Mem0 ●  │   ● Letta
              memvid ●   │   ● MemOS
                         │
                         ▼
                    General-Purpose
```

Yaad's unique position: **coding-specific + lightweight + graph-native (DAG)**.

No other lightweight coding memory system uses a DAG. Cognee uses knowledge graphs but is general-purpose and heavy. Neo4j agent-memory requires Neo4j. Yaad puts a DAG in SQLite — zero deps, full graph power.

### Yaad's Unique Advantages (No Other OSS Has These)

1. **Relaxed DAG in SQLite** — Graph power without Neo4j
2. **Graph-propagated staleness** — File changes flag entire subgraphs
3. **Impact analysis** — "What breaks if I change this file?"
4. **9 coding-specific node types** — Not generic "facts"
5. **3-stage graph search** — Seed → traverse → rank
6. **Graph-aware decay** — Orphan nodes decay faster
7. **Bi-directional agent file bridge** — Sync with CLAUDE.md, .cursorrules, AGENTS.md
8. **Full version history + rollback** — Undo bad memory updates

### Where Yaad Is Behind (Honest)

| Gap | Who's Ahead | Yaad's Timeline |
|---|---|---|
| Proactive intent prediction | memU | Phase 3 |
| Multi-modal (images) | MemOS, memU | Phase 5 |
| Skill/procedural memory | MemOS, memU | Phase 5 |
| LLM-based entity extraction | Mem0, Cognee | Phase 3 (regex in Phase 1) |
| Auto-capture hooks | agentmemory (12 hooks) | Phase 4 |
| Benchmark scores | agentmemory 95.2% | Phase 5 |
| Community/ecosystem | Mem0 (54k⭐) | Day 0 |

See [COMPARISON.md](COMPARISON.md) for full feature matrix against 8 OSS projects.

## Success Metrics

- **30-second setup**: `brew install yaad && yaad init && yaad`
- **Works with 5+ agents** out of the box via MCP
- **<50ms** recall latency (BM25)
- **<200ms** recall latency (hybrid with embeddings)
- **Zero external deps** — single binary, SQLite only
- **<10MB** binary size

## License

MIT
