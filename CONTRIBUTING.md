# Contributing to Yaad

Thank you for your interest in contributing! Yaad is a memory layer for coding agents — your contributions help make it better for everyone.

## Quick Start

```bash
git clone https://github.com/GrayCodeAI/yaad
cd yaad
make build    # verify it builds
make test     # run tests
```

**Requirements:** Go 1.25+. No CGO, no C compiler needed.

## What to Work On

Check [open issues](https://github.com/GrayCodeAI/yaad/issues) for things to pick up.

Good first issues:
- Improve entity extraction patterns in `internal/engine/entities.go`
- Add a new memory node type
- Improve privacy filter patterns in `internal/privacy/filter.go`
- Add export formats in `internal/exportimport/export.go`

## Development

```bash
# Build
make build

# Test
make test

# Lint
go vet ./...
```

## Pull Request Guidelines

1. **One thing per PR** — keep it focused
2. **Tests required** — add a test for new functionality
3. **No CGO** — Yaad must build with `CGO_ENABLED=0`
4. **No LLM API calls in hot paths** — Yaad is a memory layer, not an LLM client
5. **Keep it minimal** — avoid unnecessary abstractions
6. **Localhost-only** — REST server must never bind to public interfaces

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full technical design.

Key principles:
- **Yaad is a memory layer.** It stores, retrieves, and organizes memories.
- **MCP-first.** The primary integration is via MCP stdio — agents call `yaad mcp`.
- **Single-user.** No auth, no multi-tenancy. One developer, one machine.
- **Pure Go.** No CGO, no external dependencies at runtime.

## Project Structure

```
cmd/yaad/           CLI entry point (cobra commands)
internal/
  engine/           Core memory engine (remember, recall, context, decay)
  graph/            DAG operations (BFS, impact, ancestors, subgraph)
  storage/          SQLite storage layer (FTS5, WAL mode)
  server/           REST API + MCP server
  hooks/            Auto-capture hooks (session lifecycle)
  compact/          Memory compaction (summarize old memories)
  config/           TOML config loading
  privacy/          Secret detection and redaction
  skill/            Procedural memory (reusable step sequences)
  git/              Git-aware staleness detection
  embeddings/       Vector embedding providers (OpenAI, Voyage, local stub)
```

## License

By contributing, you agree your contributions are licensed under [MIT](LICENSE).
