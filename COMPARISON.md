# Yaad vs OSS — Competitive Comparison

## Feature Matrix

| Feature | **Yaad** | **Mem0** 54k⭐ | **Letta** 22k⭐ | **Cognee** 17k⭐ | **memU** 13k⭐ | **MemOS** 9k⭐ | **Engram** 3k⭐ | **agentmemory** 2k⭐ |
|---|---|---|---|---|---|---|---|---|
| Focus | Coding agents | General AI | General AI | General AI | Proactive agents | General AI | Coding agents | Coding agents |
| Language | Go | Python | Python | Python | Python | Python/TS | Go | TypeScript |
| Graph memory | ✅ Relaxed DAG | ⚠️ Optional Neo4j | ❌ Flat blocks | ✅ Knowledge graph | ❌ Hierarchical | ❌ Flat | ❌ Flat | ⚠️ Basic entity graph |
| Coding-specific types | ✅ 9 types | ❌ Generic | ❌ Core/archival | ❌ Generic | ❌ Generic | ⚠️ Skills | ❌ Generic | ⚠️ 4 tiers |
| Tiered loading | ✅ Hot/warm/cold | ❌ | ✅ 2-tier | ❌ | ❌ | ❌ | ❌ | ⚠️ Token budget |
| Graph traversal | ✅ BFS + impact | ❌ | ❌ | ✅ Graph-RAG | ❌ | ❌ | ❌ | ❌ |
| Git-aware staleness | ✅ Graph-propagated | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Entity extraction | ✅ Auto regex | ✅ LLM | ❌ | ✅ LLM | ✅ LLM | ✅ LLM | ❌ | ❌ |
| Deduplication | ✅ SHA-256 | ✅ LLM | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ SHA-256 |
| Agent file bridge | ✅ CLAUDE.md sync | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ MEMORY.md |
| MCP server | ✅ 12 tools | ✅ Plugin | ❌ Own API | ✅ | ✅ | ✅ | ✅ | ✅ 51 tools |
| REST API | ✅ 15 endpoints | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ 107 endpoints |
| HTTPS/TLS | ✅ Phase 2 | ✅ Cloud | ✅ Cloud | ✅ | ❌ | ✅ Cloud | ❌ | ❌ |
| gRPC + streaming | ✅ Phase 2 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| CLI | ✅ 16 commands | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Auto-capture hooks | ✅ Phase 4 | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ✅ 12 hooks |
| Search | ✅ BM25+graph+vector | ✅ Vector+entity+BM25 | ✅ Vector | ✅ Graph-RAG | ✅ RAG+LLM | ✅ Vector+graph | ✅ FTS5 | ✅ BM25+vector+graph |
| Memory decay | ✅ Graph-aware | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ Ebbinghaus |
| Contradiction detection | ✅ Supersedes chain | ✅ LLM | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Version history | ✅ Full rollback | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ Git snapshots |
| Impact analysis | ✅ Graph traversal | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Privacy filtering | ✅ Regex strip | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Multi-agent | ✅ WAL + isolation | ✅ API scoping | ✅ Within runtime | ❌ | ✅ | ✅ | ❌ | ✅ Leases |
| Zero external deps | ✅ SQLite only | ❌ Qdrant/pgvector | ❌ Postgres+vector | ❌ Multiple DBs | ❌ PostgreSQL | ❌ Neo4j+Qdrant+Redis | ✅ SQLite only | ❌ iii-engine |
| Proactive memory | ⚠️ Phase 3 | ❌ | ❌ | ❌ | ✅ Core | ❌ | ❌ | ❌ |
| Multi-modal | ⚠️ Phase 5 | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ |
| Benchmarks | ⚠️ Phase 5 | ✅ LoCoMo | ❌ | ❌ | ✅ 92% | ✅ 75.8% | ❌ | ✅ 95.2% |

## Yaad's Unique Advantages

These features exist in **no other** lightweight coding memory system:

1. **Relaxed DAG in SQLite** — Graph power without Neo4j. Causal edges are acyclic, relational edges allow cycles.
2. **Graph-propagated staleness** — File changes flag entire subgraphs, not just individual memories.
3. **Impact analysis** — "What memories break if I change auth.ts?" via reverse graph traversal.
4. **9 coding-specific node types** — First-class `convention`, `decision`, `bug`, `spec`, `task`, `preference`, `session`, `file`, `entity`.
5. **3-stage graph search** — Seed nodes (BM25) → graph expansion (BFS) → subgraph ranking (centrality + recency).
6. **Graph-aware decay** — Orphan nodes decay faster, connected nodes boost each other on access.
7. **Bi-directional agent file bridge** — Sync with CLAUDE.md, .cursorrules, AGENTS.md.
8. **Full version history + rollback** — Undo bad memory updates with `yaad rollback`.
9. **gRPC + streaming** — Only coding memory system with gRPC. Auto-generated SDKs, real-time memory events, ~10x faster than REST.

## Where Yaad Is Behind (Honest)

| Gap | Who's Ahead | Yaad's Timeline |
|---|---|---|
| Proactive intent prediction | memU | Phase 3 |
| Multi-modal (images) | MemOS, memU | Phase 5 |
| Skill/procedural memory | MemOS, memU | Phase 5 |
| LLM-based entity extraction | Mem0, Cognee | Phase 3 (regex in Phase 1) |
| Auto-capture hooks | agentmemory (12 hooks) | Phase 4 |
| Benchmark scores | agentmemory 95.2%, MemOS 75.8% | Phase 5 |
| Community/ecosystem | Mem0 (54k⭐) | Day 0 — need to ship |

## Positioning

```
                    Coding-Specific
                         ▲
                         │
                         │   ● Yaad
              Engram ●   │     (graph + tiers + git-aware + zero deps)
          agentmemory ●  │
                         │
  Lightweight ───────────┼──────────── Feature-Rich
                         │
                 Mem0 ●  │   ● Letta
              memvid ●   │   ● MemOS
                  memU ● │   ● Cognee
                         │
                         ▼
                    General-Purpose
```

**Yaad's bet**: Coding agents don't need a heavy general-purpose memory system. They need a lightweight, graph-native, coding-specific memory that understands codebases, tracks relationships between decisions, and detects when things go stale. All in a single binary with zero dependencies.
