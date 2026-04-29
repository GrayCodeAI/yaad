# Area 1 — Code Quality & Architecture — Validation Contract

## Storage Interface

### VAL-CODE-001: Storage interface extracted
The `Engine` struct references a `Storage` interface (not concrete `*storage.Store`) for persistence operations. A `Storage` interface is defined in the `storage` package or in the `engine` package, exposing at minimum the methods used by Engine: `CreateNode`, `GetNode`, `UpdateNode`, `DeleteNode`, `ListNodes`, `SearchNodes`, `SearchNodeByHash`, `CreateEdge`, `GetEdge`, `DeleteEdge`, `GetEdgesFrom`, `GetEdgesTo`, `GetNeighbors`, `CreateSession`, `EndSession`, `ListSessions`, `SaveVersion`, `GetVersions`, `SaveEmbedding`, `AllEmbeddings`, `Close`, and `DB`. Unit tests can create a `mockStorage` implementing this interface without needing a real SQLite database.
Tool: go-test
Evidence: `grep -rn "type Storage interface"` returns a definition with the methods above; `grep "engine.Store"` in the engine package shows a type assertion or field typed to `Storage` not `*storage.Store`; compilation passes; a mock implementation compiles and can be used in tests.

### VAL-CODE-002: Engine tests use mock Storage
At least one unit test in the `engine` package creates a mock/fake `Storage` implementation (not a real SQLite DB) to test engine logic in isolation.
Tool: go-test
Evidence: A test file in `internal/engine/` (e.g., `engine_test.go`) defines a `mockStore` struct implementing the `Storage` interface and uses it via `engine.New(mockStore)` or equivalent constructor.

### VAL-CODE-003: Storage interface covers DB() method or engine does not depend on raw SQL
The engine does not call `store.DB()` directly for SQL queries — all database access goes through the `Storage` interface methods. If `DB()` remains on the interface, it is only used outside the engine layer.
Tool: go-test
Evidence: `grep -rn "\.DB()" internal/engine/` returns no results (or only in test helpers that are not production paths).

## Graph Interface

### VAL-CODE-004: Graph interface extracted
The `Engine` struct references a `Graph` interface (not concrete `*graph.Graph`) for graph operations. A `Graph` interface is defined exposing at minimum: `AddNode`, `AddEdge`, `RemoveNode`, `RemoveEdge`, `ExtractSubgraph`, `BFS`, `IntentBFS`, `Impact`, `Ancestors`, `Descendants`. Unit tests can mock the interface.
Tool: go-test
Evidence: `grep -rn "type Graph interface"` returns a definition; `Engine.graph` field type is `Graph` or similar interface type; compilation passes; a mock graph implementation compiles.

### VAL-CODE-005: Engine decoupled from concrete graph and storage types
The `engine.New()` constructor accepts `Storage` and `Graph` interfaces (or a combined interface) rather than requiring concrete `*storage.Store` and `*graph.Graph` pointers. The constructor signature in `internal/engine/engine.go` reflects this decoupling.
Tool: go-test
Evidence: The signature `func New(store Storage, graph Graph) *Engine` compiles; instantiating `New(mockStore, mockGraph)` passes compilation without requiring any real database.

## context.Context Propagation

### VAL-CODE-006: All Engine public methods accept context.Context
Every exported method on `*Engine` (including `Remember`, `Recall`, `Context`, `Forget`, `Compact`, `MentalModel`, `Profile`, `Status`, `Feedback`, `Rollback`, `PendingNodes`, `StartSession`, `CompressSession`) accepts `context.Context` as its first parameter, enabling cancellation and deadlines.
Tool: go-test
Evidence: `grep -n "func (e \*Engine) " internal/engine/*.go` shows every method signature has `ctx context.Context` as first parameter; compilation succeeds.

### VAL-CODE-007: Context is propagated to storage and graph operations
Engine methods that accept `context.Context` pass it through to all `Storage` and `Graph` interface calls that accept context. Cancelling the context mid-operation causes the operation to return promptly with `context.Canceled` error.
Tool: go-test
Evidence: A test creates a cancelled context, calls an Engine method (e.g., `Remember`), and verifies the error is `context.Canceled` or the operation returns quickly (<100ms) with a context-related error.

## Bubble Sort Replacement

### VAL-CODE-008: sortByScore uses sort.Slice instead of bubble sort
The `sortByScore` function in `internal/engine/memory.go` is implemented using `sort.Slice` (or `sort.SliceStable`) with an `i < j` style comparator, not a hand-rolled O(n²) bubble/selection sort.
Tool: go-test
Evidence: Review of `sortByScore` shows it calls `sort.Slice(nodes, func(i, j int) bool { return score(nodes[i], now) > score(nodes[j], now) })` or equivalent; the function body no longer contains nested `for i` / `for j` loops for sorting.

### VAL-CODE-009: No remaining bubble sort or insertion sort in hot paths
All sorting in `internal/engine/` and `internal/graph/` uses `sort.Slice` or `sort.Sort` from the standard library. The insertion sort in `graph.IntentBFS` is replaced with `sort.Slice` as well.
Tool: go-test
Evidence: `grep -rn "for j := i + 1; j < len" internal/engine/ internal/graph/` returns no results; `grep -rn "for j := i; j > 0" internal/engine/ internal/graph/` returns no results.

## Graceful SIGTERM/SIGINT Shutdown

### VAL-CODE-010: Server handles SIGTERM and SIGINT for graceful shutdown
The main CLI entry point (`cmd/yaad/main.go` root command and `serve` command) registers signal handlers for `syscall.SIGTERM` and `syscall.SIGINT` that trigger a graceful shutdown: the HTTP server calls `Shutdown()` (not `Close()`) with a timeout context, and `store.Close()` is called before process exit.
Tool: cli
Evidence: Starting `yaad serve`, sending SIGTERM via `kill <pid>`, and observing that the process exits cleanly with log messages like "shutting down..." and no "database locked" errors on restart; code review confirms `signal.Notify` is called with SIGTERM and SIGINT.

### VAL-CODE-011: Shutdown drains active connections before closing
The graceful shutdown path calls `http.Server.Shutdown(ctx)` (which waits for active connections to complete) rather than `http.Server.Close()` (which terminates them immediately).
Tool: cli
Evidence: Code review shows `srv.Shutdown(ctx)` or equivalent graceful drain; a test starts the server, initiates a long-running request, sends SIGTERM, and verifies the in-flight request completes before the process exits (or returns within the shutdown timeout).

### VAL-CODE-012: Database store is closed during shutdown
During graceful shutdown, `store.Close()` is called to ensure WAL checkpointing and clean SQLite closure. The deferred `defer eng.Store().Close()` from the main function runs during shutdown.
Tool: go-test
Evidence: Code review shows `store.Close()` or `eng.Store().Close()` is called (or deferred) in the signal handler path; after a SIGTERM, the .db file is not corrupted and can be reopened immediately.

## CORS Headers on REST API

### VAL-CODE-013: CORS middleware applied to all REST API routes
A CORS middleware (or handler wrapper) is applied to all `/yaad/*` routes, setting at minimum `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`, and `Access-Control-Allow-Headers: Content-Type, Authorization`. This covers all 40+ REST endpoints, not just the SSE endpoint.
Tool: curl
Evidence: `curl -H "Origin: https://example.com" -H "Access-Control-Request-Method: POST" -X OPTIONS http://localhost:3456/yaad/recall` returns status 200/204 with `Access-Control-Allow-Origin: *` header; `curl -H "Origin: https://example.com" http://localhost:3456/yaad/health` includes `Access-Control-Allow-Origin: *` in response headers.

### VAL-CODE-014: CORS middleware handles preflight OPTIONS requests
The CORS middleware correctly responds to HTTP `OPTIONS` requests (preflight) on all `/yaad/*` routes with appropriate `Access-Control-Allow-Methods` and `Access-Control-Allow-Headers` headers, without invoking the actual route handler.
Tool: curl
Evidence: `curl -X OPTIONS -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: POST" -v http://localhost:3456/yaad/remember 2>&1 | grep -i "access-control"` returns headers as described; the preflight returns 200/204 without attempting to process a body.

### VAL-CODE-015: SSE endpoint retains its CORS header
The SSE endpoint `GET /yaad/events` still sets `Access-Control-Allow-Origin: *` after the middleware is added (either via the middleware covering it or the existing explicit header).
Tool: curl
Evidence: `curl -H "Origin: https://example.com" http://localhost:3456/yaad/events -N` includes `Access-Control-Allow-Origin: *` in response headers.

## Shell Completion

### VAL-CODE-016: cobra shell completion command registered
A `completion` subcommand is registered on the root `yaad` command that generates shell completion scripts. It supports at minimum `bash` and `zsh` completion.
Tool: cli
Evidence: Running `yaad completion --help` shows completion instructions; `yaad completion bash` outputs a bash completion script; `yaad completion zsh` outputs a zsh completion script.

### VAL-CODE-017: Shell completion covers all subcommands
Running the generated completion script and typing `yaad <TAB><TAB>` lists all subcommands including `remember`, `recall`, `link`, `subgraph`, `impact`, `status`, `mcp`, `serve`, `export`, `embed`, `hybrid-recall`, `proactive`, `decay`, `gc`, `bridge-import`, `bridge-export`, `hook`, `setup`, `replay`, `export-json`, `export-md`, `export-obsidian`, `import-json`, `skill-store`, `skill-list`, `skill-replay`, `bench`, `sync`, `tui`, `intent`, `doctor`, `watch`.
Tool: cli
Evidence: Sourcing the completion script and running `yaad ` with tab triggers lists all expected commands; `yaad remember ` with tab triggers lists flag completions (`--type`, `--tags`).

## Config.Load() Actually Called

### VAL-CODE-018: config.Load() is called at startup
The `openEngine()` function (or the main `yaad` and `serve` commands) calls `config.Load(os.Getenv("YAAD_PROJECT_DIR") or ".")` before creating the engine, and uses the loaded config (e.g., server port, embedding provider) rather than hardcoded defaults.
Tool: go-test
Evidence: Code review shows `cfg, err := config.Load(projectDir)` is called in `cmd/yaad/helpers.go` or `cmd/yaad/main.go`; the loaded config values are used to configure the server address, embedding provider, and other settings.

### VAL-CODE-019: Server address respects config
The REST server address (port and host) is read from the loaded config (e.g., `cfg.Server.Host:cfg.Server.Port`) instead of the hardcoded `:3456` default. The CLI `--addr` flag may override the config value.
Tool: cli
Evidence: Starting `yaad serve` without `--addr` flag uses the port from `.yaad/config.toml` (if it exists) or the compiled default; `grep ":3456" cmd/yaad/main.go` shows the hardcoded default is removed or changed to a config-driven fallback.

### VAL-CODE-020: Config changes take effect without recompilation
Creating a `.yaad/config.toml` file with `[server] port = 3457` and restarting `yaad serve` causes the server to listen on port 3457 instead of 3456.
Tool: cli
Evidence: Start `yaad serve` with a `.yaad/config.toml` that sets a custom port; verify via `curl http://localhost:<custom-port>/yaad/health` that the server responds on the configured port.
