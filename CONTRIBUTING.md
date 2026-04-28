# Contributing to Yaad

Thank you for your interest in contributing! Yaad is a memory layer for coding agents — your contributions help make it better for everyone.

## Quick Start

```bash
git clone https://github.com/GrayCodeAI/yaad
cd yaad
go build ./...          # verify it builds
go test -count=1 ./...  # run tests
```

**Requirements:** Go 1.23+. No CGO, no C compiler needed.

## What to Work On

Check [open issues](https://github.com/GrayCodeAI/yaad/issues) or pick from the roadmap in [PLAN.md](PLAN.md).

Good first issues:
- Add a new agent to `internal/agentconfig/generator.go`
- Improve entity extraction patterns in `internal/engine/entities.go`
- Add a new memory node type
- Improve the TUI screens in `internal/tui/tui.go`

## Development

```bash
# Build
CGO_ENABLED=0 go build -o yaad ./cmd/yaad

# Test (run multiple times to catch flakiness)
CGO_ENABLED=0 go test -count=3 ./...

# Lint
go vet ./...
```

## Pull Request Guidelines

1. **One thing per PR** — keep it focused
2. **Tests required** — add a test for new functionality
3. **No CGO** — Yaad must build with `CGO_ENABLED=0`
4. **No LLM API calls** — Yaad is a memory layer, not an LLM client. The coding agent handles LLM calls.
5. **Keep it minimal** — avoid unnecessary abstractions

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full technical design.

Key principle: **Yaad is a memory layer.** It stores, retrieves, and organizes memories. It does NOT call LLM APIs. The coding agent (Hawk, Claude Code, Cursor, etc.) handles all LLM interactions.

## Adding a New Agent

Edit `internal/agentconfig/generator.go`:
1. Add a constant: `AgentMyAgent Agent = "my-agent"`
2. Add a case in `Generate()` — either use `generateGenericMCP()` or write a custom generator
3. Add to the CLI help text in `cmd/yaad/main.go`
4. Add to the README agent table

## License

By contributing, you agree your contributions are licensed under [MIT](LICENSE).
