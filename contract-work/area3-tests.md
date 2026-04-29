# Area 3: Test Quality — Validation Contract

## Per-package unit tests (storage, graph, engine)

### VAL-TEST-001: Storage package has unit tests with SQLite DB
Package `internal/storage` has a `_test.go` file with unit tests that cover `NewStore`, `CreateNode`, `GetNode`, `CreateEdge`, `GetNeighbors`, and versioning using a real SQLite database.
Tool: go-test
Evidence: `go test ./internal/storage/... -count=1` passes and reports at least 5 test functions covering CRUD operations.

### VAL-TEST-002: Graph package has unit tests
Package `internal/graph` has a `_test.go` file with unit tests for core graph operations (AddEdge, GetNeighbors, Subgraph extraction, cycle detection).
Tool: go-test
Evidence: `go test ./internal/graph/... -count=1` passes and reports at least 3 test functions covering edge creation, subgraph extraction, and cycle detection.

### VAL-TEST-003: Engine package has unit tests
Package `internal/engine` has a `_test.go` file with unit tests covering Remember, Recall, Context, Forget, and Decay operations.
Tool: go-test
Evidence: `go test ./internal/engine/... -count=1` passes and reports at least 5 test functions covering core engine operations.

### VAL-TEST-004: All per-package unit tests pass without race conditions
Running per-package unit tests with the race detector enabled reports no data races.
Tool: go-test
Evidence: `go test -race ./internal/storage/... ./internal/graph/... ./internal/engine/... -count=1` all pass with zero race warnings.

---

## Table-driven tests for edge cases

### VAL-TEST-005: Table-driven test for empty content
There is a table-driven test in `internal/engine/` that tests `Remember` with empty content (zero-length string, whitespace-only string) and verifies appropriate error handling.
Tool: go-test
Evidence: A test with a `[]struct{...}` or `map[string]struct{...}` cases where one case provides an empty string as content and the test asserts an error is returned (e.g., `ErrEmptyContent` or similar).

### VAL-TEST-006: Table-driven test for UUID collision
There is a table-driven test in `internal/storage/` that tests behavior when a node with a duplicate/conflicting ID is created, verifying the storage layer rejects or handles ID collisions deterministically.
Tool: go-test
Evidence: A test that attempts to create two nodes with the same ID and asserts the second creation either fails with a specific error or returns the existing node (dedup behavior).

### VAL-TEST-007: Table-driven test for special FTS5 characters
There is a table-driven test in `internal/storage/` (or `internal/engine/`) that tests search/query with special FTS5 characters (e.g., `*`, `"`, `-`, `+`, `~`, parentheses, `AND`/`OR` operators) and verifies they are properly escaped or handled without SQL syntax errors.
Tool: go-test
Evidence: A test case set containing strings like `"test*"`, `"foo -bar"`, `"hello AND world"`, `"parentheses (test)"` that verifies query results are correct and no SQL error occurs.

### VAL-TEST-008: Table-driven test for edge case on empty database
There is a table-driven test that verifies `Recall`, `Context`, and other query operations on a freshly initialized empty database return empty results (not errors).
Tool: go-test
Evidence: A test that calls `eng.Recall("anything")` and `eng.Context("project")` on a store with zero nodes and asserts the returned result is empty (nil or zero-length list) and no error is returned.

### VAL-TEST-009: Table-driven test for nonexistent node operations
There is a table-driven test that verifies `Forget`, `GetNode`, and `Link` operations with nonexistent node IDs return appropriate errors.
Tool: go-test
Evidence: A test that attempts `Forget("nonexistent-id")`, `Link("bad1", "bad2", "edge-type")` and asserts specific error types or messages are returned.

---

## Race condition tests (concurrent access)

### VAL-TEST-010: Concurrent Remember has no data races
There is a test that spawns multiple goroutines calling `eng.Remember()` concurrently on the same engine instance, run with `-race` flag, to verify goroutine safety.
Tool: go-test
Evidence: `go test -race -run TestConcurrentRemember ./... -count=1` passes with zero race warnings. The test launches at least 10 concurrent goroutines writing different content.

### VAL-TEST-011: Concurrent Recall has no data races
There is a test that spawns multiple goroutines calling `eng.Recall()` concurrently while another goroutine writes (Remember), run with `-race` flag, to verify read/write safety.
Tool: go-test
Evidence: `go test -race -run TestConcurrentReadWrite ./... -count=1` passes with zero race warnings. The test runs concurrent readers and writers with `sync.WaitGroup` synchronization.

### VAL-TEST-012: Concurrent Context and Forget has no data races
There is a test that calls `Context()` and `Forget()` concurrently to verify no race conditions between hot-tier context retrieval and node archival.
Tool: go-test
Evidence: `go test -race -run TestConcurrentContextForget ./... -count=1` passes with zero race warnings. The test verifies that concurrent context reads and forget operations do not produce races.

---

## Existing 38 integration tests still pass (no regressions)

### VAL-TEST-013: All 38 integration tests pass
Running the full integration test suite produces no failures and the output confirms all 38 test functions pass.
Tool: go-test
Evidence: Running `go test -count=1 -v .` (or `./...`) from the repo root shows all 38 `Test*` functions from `integration_test.go` pass with `--- PASS:` lines and a final `ok` status.

### VAL-TEST-014: All 38 integration tests also pass with race detector
Running the integration test suite with `-race` produces zero data race warnings across all 38 tests.
Tool: go-test
Evidence: `go test -race -count=1 -v .` passes with zero `WARNING: DATA RACE` lines in the output, and all 38 tests are reported as `PASS`.

### VAL-TEST-015: Integration test setup isolates each test
Every integration test uses `t.TempDir()` for an isolated database and cleanup is called via `defer cleanup()`, meaning no test shares database state with any other test (no cross-test pollution).
Tool: go-test
Evidence: All 38 `Test*` functions in `integration_test.go` call `setup(t)` and `defer cleanup()`, confirmed by code inspection and by the fact that tests can run in any order without failures (`go test -count=3 -shuffle=on .` passes consistently).
