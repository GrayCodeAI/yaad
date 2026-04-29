# Area 4: Cross-Area / Non-Regression Validation Contract

This contract covers flows that span multiple areas or provide non-regression guarantees after all changes are applied.

---

### VAL-CROSS-001: All 38 existing tests still pass
Running the full test suite (with `-race` flag) shows all 38 existing tests pass. No regressions from interface, refactoring, or behavioral changes across any area.
Tool: go-test
Evidence: `go test -race -count=1 ./...` outputs "PASS" across all packages and reports 38 passing test functions. Confirmed by identical results across two consecutive runs (5.562s and 5.856s wall time). The `-race` flag ensures the data-race detector also passes clean.

---

### VAL-CROSS-002: `go vet ./...` passes without warnings
Running Go's static analysis tool produces zero warnings, errors, or vet failures across all packages. No suspicious constructs, unreachable code, or misused printf-style calls were introduced.
Tool: go-test
Evidence: `go vet ./...` exits with code 0 and produces no output (clean pass). Baseline confirmed before changes; re-verify after all area changes.

---

### VAL-CROSS-003: `go build ./...` succeeds without errors
All packages compile without errors. No broken imports, type mismatches, or missing symbols after all area refactoring and feature additions.
Tool: go-test
Evidence: `go build ./...` exits with code 0 and produces no error output. Verified across both `CGO_ENABLED=0` and default build modes.

---

### VAL-CROSS-004: Binary size stays within reasonable limits
The compiled binary does not exhibit massive bloat. Acceptable threshold: no more than ~35 MB (current baseline is ~27 MB). A moderate increase is expected due to new features, but order-of-magnitude growth indicates accidental inclusion of large dependencies or embedded assets.
Tool: go-test
Evidence: `CGO_ENABLED=0 go build -ldflags="-s -w" -o /tmp/yaad-bin ./cmd/yaad && ls -lh /tmp/yaad-bin` shows binary size ≤ 35 MB. Baseline established at 27 MB during validation readiness check.

---

### VAL-CROSS-005: Zero new external dependencies
No new module requirements were added to `go.mod`. All new functionality must be implemented using the existing dependency set (stdlib + current 13 direct dependencies: BurntSushi/toml, charmbracelet/bubbles, charmbracelet/bubbletea, charmbracelet/lipgloss, google/uuid, mark3labs/mcp-go, spf13/cobra, google.golang.org/grpc, modernc.org/sqlite, atotto/clipboard, spf13/cast, spf13/pflag, yosida95/uritemplate/v3).
Tool: cli
Evidence: `diff <(git show HEAD:go.mod) go.mod` shows zero changes to the `require` block (no new module paths). Only indirect dependency bumps from existing modules (if any) are tolerated, as managed by `go mod tidy`.

---

### VAL-CROSS-006: Zero CGO dependencies (CGO_ENABLED=0)
The project must compile cleanly with `CGO_ENABLED=0` to support fully static builds across all platforms (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64). No CGO-based libraries are introduced.
Tool: go-test
Evidence: `CGO_ENABLED=0 go build -o /dev/null ./cmd/yaad` succeeds with exit code 0. Build command from the project Makefile (`.PHONY: build` target) confirms this as the standard build mode. Failure would produce `CGO_ENABLED=0` linker errors referencing C symbol dependencies.

---

### VAL-CROSS-007: No breaking changes to Go SDK public API
The public Go SDK (`github.com/GrayCodeAI/yaad/sdk/go/yaad`) retains full backward compatibility. All exported types, constants, functions, and methods must either remain unchanged or only be extended (additive changes only). Removing, renaming, or changing signatures of exported identifiers is prohibited.
Tool: go-test
Evidence: Running `go vet ./sdk/...` passes clean (already covered by VAL-CROSS-002). Compilation against all SDK consumers (integration test imports) succeeds. Manual verification: diff the exported API surface before and after changes. Key public API surface to preserve: `Open(dbPath string)`, `Close()`, `Remember(content, nodeType, ...RememberOption)`, `Recall(query, ...RecallOption)`, `Context(project)`, `Forget(id)`, `Compact(project)`, `MentalModel(project)`, `Approve(id)`, `Edit(id, newContent)`, `Discard(id)`, plus all `With*` option constructors, all 7 memory type constants (`Convention`, `Decision`, `Bug`, `Spec`, `Task`, `Skill`, `Preference`), and all type aliases (`Node`, `Edge`, `RecallResult`, `MentalModel`).
