# Yaad — Architecture

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     CODING AGENTS                               │
│  Claude Code │ Cursor │ Gemini CLI │ OpenCode │ Cline │ Any     │
└──────┬───────────────┬──────────────────────────┬───────────────┘
       │ MCP (stdio)   │ REST/HTTPS (:3456)       │ gRPC (:3457)
       ▼               ▼                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                       YAAD SERVER                               │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────┐ ┌─────────┐ │
│  │MCP Server│  │REST/HTTPS│  │gRPC      │  │CLI │ │Git Watch│ │
│  │(stdio)   │  │(:3456)   │  │(:3457)   │  │    │ │(fsnoti) │ │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─┬──┘ └────┬────┘ │
│       └──────────────┴─────────────┴──────────┴─────────┘      │
│                          │                                      │
│  ┌───────────────────────▼──────────────────────────────────┐   │
│  │                   MEMORY ENGINE                          │   │
│  │                                                          │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐  │   │
│  │  │ Ingest   │ │ Search   │ │ Tiers    │ │ Decay/GC   │  │   │
│  │  │+entities │ │+graph    │ │hot/warm/ │ │+graph-aware│  │   │
│  │  │+dedup    │ │+BM25     │ │cold      │ │+orphan     │  │   │
│  │  │+privacy  │ │+vector   │ │+budget   │ │+superseded │  │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └────────────┘  │   │
│  └───────────────────────┬──────────────────────────────────┘   │
│                          │                                      │
│  ┌───────────────────────▼──────────────────────────────────┐   │
│  │                   GRAPH ENGINE                           │   │
│  │                                                          │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐  │   │
│  │  │ DAG Ops  │ │Traversal │ │ Ranking  │ │  Impact    │  │   │
│  │  │+add node │ │+BFS/DFS  │ │+PageRank │ │  Analysis  │  │   │
│  │  │+add edge │ │+subgraph │ │+centrality│ │+file→graph│  │   │
│  │  │+cycle chk│ │+depth ctl│ │+recency  │ │+staleness │  │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └────────────┘  │   │
│  └───────────────────────┬──────────────────────────────────┘   │
│                          │                                      │
│  ┌───────────────────────▼──────────────────────────────────┐   │
│  │                   STORAGE (SQLite)                       │   │
│  │                                                          │   │
│  │  ┌────────┐ ┌────────┐ ┌────────┐ ┌─────────┐ ┌──────┐ │   │
│  │  │ Nodes  │ │ Edges  │ │ FTS5   │ │Versions │ │Embed │ │   │
│  │  │(graph) │ │(graph) │ │(search)│ │(audit)  │ │(opt) │ │   │
│  │  └────────┘ └────────┘ └────────┘ └─────────┘ └──────┘ │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘

Storage:
  ~/.yaad/yaad.db           → Global (preferences, cross-project)
  <project>/.yaad/yaad.db   → Project (conventions, decisions, bugs)
```

## Component Details

### 1. MCP Server (`internal/server/mcp.go`)

Implements Model Context Protocol over stdio transport.

```
12 Tools:
  yaad_recall        → engine.Search()     → graph-aware hybrid search
  yaad_remember      → engine.Remember()   → create node + auto-link edges
  yaad_context       → engine.Context()    → hot-tier subgraph
  yaad_forget        → engine.Forget()     → archive/detach node
  yaad_link          → graph.AddEdge()     → create edge
  yaad_unlink        → graph.RemoveEdge()  → remove edge
  yaad_subgraph      → graph.Subgraph()    → BFS from node
  yaad_impact        → graph.Impact()      → reverse traversal from file
  yaad_status        → engine.Status()     → health + stats
  yaad_task_update   → engine.TaskUpdate() → update task node
  yaad_sessions      → engine.Sessions()   → list sessions
  yaad_stale         → git.Stale()         → staleness report

3 Resources:
  yaad://context     → hot-tier subgraph
  yaad://graph/stats → node/edge counts
  yaad://stale       → stale warnings

2 Prompts:
  recall_context     → search + format for injection
  session_handoff    → generate handoff summary
```

### 2. REST/HTTPS API (`internal/server/rest.go`)

HTTP server on `127.0.0.1:3456`. Optional TLS with `--tls` flag.

```
Nodes:
  POST   /yaad/remember       → create node + edges
  GET    /yaad/node/:id       → get node + neighbors
  DELETE /yaad/forget/:id     → archive node

Edges:
  POST   /yaad/link           → create edge
  DELETE /yaad/link/:id       → remove edge

Search:
  POST   /yaad/recall         → graph-aware search
  GET    /yaad/context        → hot-tier subgraph
  GET    /yaad/subgraph/:id   → BFS subgraph
  GET    /yaad/impact/:file   → impact analysis

Sessions:
  POST   /yaad/session/start  → start + return context
  POST   /yaad/session/end    → end + compress
  GET    /yaad/sessions       → list

System:
  GET    /yaad/health         → health check
  GET    /yaad/graph/stats    → graph statistics
  GET    /yaad/stale          → staleness report
```

### 3. gRPC Server (`internal/server/grpc.go`)

Port `3457`. Protobuf-defined, supports streaming.

```
Unary RPCs (request/response):
  Remember, Recall, Context, Forget
  Link, Unlink, Subgraph, Impact
  SessionStart, SessionEnd
  Health, GraphStats

Streaming RPCs (real-time):
  WatchMemories  → stream of memory create/update/delete events
  WatchStale     → stream of staleness alerts as git changes happen

Benefits over REST:
  - Auto-generated SDKs (Python, TypeScript, Rust, Java)
  - ~10x faster for high-frequency calls
  - Bi-directional streaming for real-time updates
  - Strongly typed contracts — no string parsing
```

### 4. Graph Engine (`internal/graph/`)

The core DAG implementation. All graph operations happen here.

```go
// dag.go — Core operations
AddNode(node Node) error              // insert node, compute content_hash
AddEdge(edge Edge) error              // insert edge, check cycles if acyclic type
RemoveNode(id string) error           // archive node, detach edges
RemoveEdge(id string) error           // remove edge

// traverse.go — Graph traversal
BFS(startID string, maxDepth int) []Node       // breadth-first search
Subgraph(startID string, depth int) Graph      // extract connected subgraph
Ancestors(id string) []Node                     // walk backwards
Descendants(id string) []Node                   // walk forwards

// rank.go — Scoring
PageRank(nodeIDs []string) map[string]float64  // rank within subgraph
Score(node Node) float64                        // confidence × recency × centrality

// impact.go — Impact analysis
Impact(filePath string, depth int) []Node      // file change → affected nodes
StaleNodes(since time.Time) []StaleReport      // git-aware staleness
```

**Cycle detection** (relaxed DAG):
```
Acyclic edges: caused_by, led_to, supersedes, learned_in, part_of
  → Run recursive CTE ancestor check before insert
  → Reject if cycle detected

Cyclic-allowed edges: relates_to, depends_on, touches
  → Skip cycle check
  → Allow bidirectional relationships
```

### 5. Memory Engine (`internal/engine/`)

Business logic layer that wraps graph + storage.

```go
// memory.go — CRUD
Remember(input RememberInput) (Node, error)
  1. Privacy filter (strip secrets)
  2. Compute content_hash (SHA-256)
  3. Dedup check (hash + scope + project)
  4. Extract entities (files, libs, functions)
  5. Create node
  6. Create entity/file nodes if new
  7. Create edges (touches, relates_to)
  8. Update FTS5 index

Recall(query string, opts SearchOpts) ([]Node, []Edge, error)
  1. BM25 search → seed nodes
  2. (Optional) Vector search → more seeds
  3. Graph expansion (BFS, depth=opts.Depth)
  4. Subgraph ranking (PageRank + recency + confidence)
  5. Token budget trim
  6. Return nodes + edges

Context(project string) (ContextResponse, error)
  1. Load hot-tier nodes (tier=1, high confidence)
  2. Load active tasks (type=task, not completed)
  3. Load stale warnings (git watcher)
  4. Load previous session summary
  5. Format within token budget (~2K tokens)

Forget(id string) error
  1. Archive node (set confidence=0)
  2. Detach edges (or mark as archived)
  3. Log to node_versions

// search.go — Hybrid search
Search(query string, filters SearchFilters) []ScoredNode
  Stage 1: BM25 via FTS5
  Stage 2: Graph expansion via BFS
  Stage 3: RRF fusion + ranking

// tiers.go — Tier management
HotNodes(project string) []Node     // tier=1, high confidence
WarmNodes(query string) []Node      // tier=2, relevant to query
ColdSearch(query string) []Node     // tier=3, full search

// decay.go — Confidence decay
RunDecay()
  - Apply half-life formula: confidence *= 0.5^(days/half_life)
  - Orphan nodes (0 edges): decay 2x faster
  - Superseded nodes: decay 2x faster
  - Accessed nodes: boost by 0.2
  - Below min_confidence: eligible for GC

GarbageCollect()
  - Remove nodes with confidence < min_confidence
  - Remove orphan entity/file nodes with no edges
  - Compact FTS5 index
```

### 6. Storage (`internal/storage/sqlite.go`)

SQLite with WAL mode for concurrent reads.

```
Tables:
  nodes          — graph nodes (memories)
  edges          — graph edges (relationships)
  nodes_fts      — FTS5 full-text search index
  embeddings     — optional vector embeddings
  sessions       — session tracking
  file_watch     — file→node staleness mapping
  node_versions  — version history for audit/rollback

Indexes:
  idx_nodes_hash       — UNIQUE(content_hash, scope, project) for dedup
  idx_edges_unique     — UNIQUE(from_id, to_id, type)
  idx_edges_from       — edges by from_id
  idx_edges_to         — edges by to_id
  idx_file_watch_path  — file_watch by file_path

Concurrency:
  WAL mode enabled for concurrent reads
  Write serialization via Go mutex
```

### 7. Entity Extraction (`internal/engine/entities.go`)

Regex + heuristic-based, no LLM needed for Phase 1.

```
Patterns:
  Files:      /[\w\/]+\.(go|ts|py|js|rs|java|rb|sql|yaml|toml|json)/
  Imports:    /import\s+["']([^"']+)["']/
  Functions:  /\b[a-z][a-zA-Z0-9]*\(/  (camelCase followed by paren)
  Classes:    /\b[A-Z][a-zA-Z0-9]+\b/  (PascalCase)
  URLs:       /https?:\/\/[^\s]+/
  Ports:      /:\d{4,5}\b/
  Packages:   known patterns (npm: @scope/pkg, go: github.com/x/y, pip: pkg-name)

Output:
  List of (entity_name, entity_type) tuples
  Each becomes a node (if new) + edge (touches) to the memory node
```

### 8. Privacy Filter (`internal/privacy/filter.go`)

Runs on every ingest before storage.

```
Patterns stripped:
  API keys:     /sk-[a-zA-Z0-9]{20,}/, /AKIA[A-Z0-9]{16}/
  GitHub:       /gh[pousr]_[A-Za-z0-9_]{36,}/
  JWT:          /eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/
  Bearer:       /Bearer\s+[A-Za-z0-9_.-]+/
  Env values:   /(PASSWORD|SECRET|KEY|TOKEN)\s*=\s*\S+/
  Private keys: /-----BEGIN\s+\w+\s+PRIVATE\s+KEY-----/

Replacement: [REDACTED]
```

### 9. Git Watcher (`internal/git/watcher.go`)

Monitors git for file changes to detect stale memories.

```
On session start:
  1. git log --since=<last_session> --name-only
  2. For each changed file:
     a. Look up file_watch table → find linked node IDs
     b. graph.Impact(file, depth=3) → find affected subgraph
     c. Mark affected nodes as potentially stale
  3. Return StaleReport with affected subgraphs

On file save (optional, via fsnotify):
  - Real-time staleness flagging
  - Lightweight: only checks file_watch table
```

### 10. Agent File Bridge (`internal/bridge/agentfiles.go`)

Bi-directional sync with native agent memory files.

```
Import:
  Read CLAUDE.md → parse sections → create convention/decision nodes
  Read .cursorrules → parse rules → create convention nodes
  Read AGENTS.md → parse → create convention nodes

Export:
  Query hot-tier conventions → format as markdown → write to CLAUDE.md
  Append "# Generated by Yaad" header to distinguish

Watch mode:
  fsnotify on CLAUDE.md → re-import on change
  On yaad node change (hot tier) → re-export to CLAUDE.md

Conflict resolution:
  Yaad is source of truth for graph
  Agent files are a "view" of the hot tier
  Manual edits to CLAUDE.md get imported as new nodes
```

## Data Flow

### Remember (Store a Memory)

```
Input: "Use jose instead of jsonwebtoken for Edge compatibility"
  │
  ├─1. Privacy filter → strip any secrets
  ├─2. SHA-256 hash → check dedup
  ├─3. Classify type → "convention"
  ├─4. Extract entities → ["jose", "jsonwebtoken"]
  ├─5. Create node (type=convention, tier=1)
  ├─6. Create entity nodes if new
  ├─7. Create edges: convention →touches→ entity:jose
  │                   convention →touches→ entity:jsonwebtoken
  ├─8. Update FTS5 index
  └─9. Return node + edges
```

### Recall (Search)

```
Query: "auth middleware"
  │
  ├─1. BM25 search (FTS5) → [spec:auth, convention:jose, bug:token-refresh]
  ├─2. Graph expand (BFS depth=2) → pull neighbors
  │    spec:auth → decision:RS256, convention:jose, file:auth.ts
  │    convention:jose → entity:jose, entity:jsonwebtoken
  │    bug:token-refresh → fix:keepalive, session:jan-15
  ├─3. Score: RRF(text_rank, graph_centrality) × confidence × recency
  ├─4. Token budget: trim to 800 tokens (warm tier)
  └─5. Return: ranked nodes + edges
```

### Session Start

```
Agent connects → yaad_context
  │
  ├─1. Load hot tier (conventions, commands, active tasks)
  ├─2. Load stale warnings (git watcher)
  ├─3. Load previous session summary
  ├─4. Format to ~2K tokens
  └─5. Return context markdown
```

## Concurrency Model

```
SQLite WAL mode:
  - Multiple readers (agents) simultaneously ✅
  - Single writer at a time (Go mutex) ✅
  - No external lock service needed ✅

Session isolation:
  - Each agent session gets unique session_id
  - Memories tagged with source_session + source_agent
  - No cross-session interference

Multi-agent safety:
  - Reads: always safe (WAL)
  - Writes: serialized via mutex
  - Conflicts: supersedes chain tracks evolution
  - No distributed locking needed (single-machine)
```

## Configuration Hierarchy

```
Precedence (highest to lowest):
  1. CLI flags          (--port 3456)
  2. Environment vars   (YAAD_PORT=3456)
  3. Project config     (<project>/.yaad/config.toml)
  4. Global config      (~/.yaad/config.toml)
  5. Defaults           (built into binary)
```
