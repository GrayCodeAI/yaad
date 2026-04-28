# Changelog

All notable changes to Yaad are documented here.

## [0.1.0] — 2026-04-28

### Initial Release

**Core**
- Relaxed DAG memory graph (Labeled Property Graph in SQLite)
- 9 coding-specific node types: convention, decision, bug, spec, task, skill, preference, file, entity
- 8 edge types with cycle enforcement: led_to, supersedes, caused_by, learned_in, part_of (acyclic) + relates_to, depends_on, touches (cyclic allowed)
- Pure Go SQLite via `modernc.org/sqlite` — no CGO, no C compiler required
- FTS5 full-text search with multi-word OR query support
- SHA-256 content deduplication
- Auto entity extraction (files, packages, functions, classes)
- Privacy filtering (strips API keys, tokens, secrets on ingest)

**Interfaces**
- MCP server (stdio, 12 tools) — works with any MCP-compatible agent
- REST/HTTPS API (25+ endpoints, port 3456)
- gRPC server (port 3457) with WatchMemories streaming
- SSE streaming at `/yaad/events`
- CLI (30 commands)
- Web dashboard at `/yaad/ui` (D3.js force graph)

**Agent Integration**
- `yaad setup` for 10 agents: claude-code, cursor, gemini-cli, opencode, codex-cli, cline, windsurf, goose, roo-code, aider
- Auto-capture hooks: SessionStart, PostToolUse, SessionEnd
- CLAUDE.md / .cursorrules / AGENTS.md bi-directional bridge

**Memory Intelligence**
- Hot/warm/cold tier system with token budget management
- Graph-aware memory decay (half-life, orphan/superseded 2× faster)
- Git-aware staleness detection (propagates through graph)
- Hybrid search: BM25 + vector + graph with RRF fusion
- Contextual re-ranking: centrality × recency × confidence × tier
- Proactive context prediction
- Session compression → summary nodes
- Memory feedback API (approve/edit/discard)
- Version history + rollback

**Team & Scale**
- Git chunk sync (`.yaad/chunks/*.jsonl.gz`) — append-only, no merge conflicts
- Team memory namespacing
- Skill/procedural memory with step sequences
- JSON/Markdown/Obsidian export
- Multi-project memory linking
- LongMemEval-style benchmark harness

**Protocols**
- HTTPS/TLS with auto self-signed cert generation
- gRPC with protobuf service definition
- WebSocket/SSE for real-time memory events
