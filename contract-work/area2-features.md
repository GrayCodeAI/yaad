# Validation Contract — Area 2: Missing Features

## VAL-FEAT-001: handleStale returns real stale subgraph data (REST)
Calling `GET /yaad/stale?project=X` returns real stale subgraph data based on `file_watch` + `git.Watcher.StalesSince()`, not a placeholder string.
- **Tool:** curl
- **Evidence:** Response JSON contains `"reports"` array with nodes/edges (not `"staleness detection available in Phase 2"`)

## VAL-FEAT-002: handleStale returns real stale subgraph data (MCP)
Calling the `yaad_stale` MCP tool with `project=X` returns real stale subgraph data, not a placeholder text.
- **Tool:** go-test (via MCP server test harness)
- **Evidence:** Tool result contains JSON with stale report fields (node IDs, file paths), not the string `"staleness detection available in Phase 2"`

## VAL-FEAT-003: Stale report ties to git changes
After remembering a node linked to a file via `file_watch`, modifying that file (via git commit), and calling `GET /yaad/stale`, the stale report includes the changed file and the linked node ID.
- **Tool:** go-test
- **Evidence:** `StalesSince()` returns `StaleReport` with `File` matching the changed file and `NodeIDs` containing the remembered node's ID

---

## VAL-FEAT-004: Local ONNX embeddings produce semantically meaningful vectors
`embeddings.NewONNX(all-MiniLM-L6-v2)` returns a provider whose `Embed()` output has cosine similarity > 0.5 for semantically related texts (e.g., "dog" vs "puppy") and < 0.3 for unrelated ones (e.g., "dog" vs "quantum physics").
- **Tool:** go-test
- **Evidence:** A dedicated test computes cosine similarity between pairs and verifies the semantic ordering

## VAL-FEAT-005: Local ONNX provider model file is documented
The ONNX embedding provider's constructor documents a fallback download URL or instructions for obtaining the `all-MiniLM-L6-v2` ONNX model file.
- **Tool:** go-test (or manual inspection)
- **Evidence:** The constructor's doc comment or a README section specifies where to download the model (e.g., HuggingFace URL) and what path it expects

## VAL-FEAT-006: Local ONNX embeddings register with expected dimensions
`NewONNX(all-MiniLM-L6-v2)` returns a provider whose `Dims()` returns 384 (the correct output dimension for all-MiniLM-L6-v2), not 128 (the stub dimension).
- **Tool:** go-test
- **Evidence:** `provider.Dims()` == 384

---

## VAL-FEAT-007: PageRank computation exists in graph/rank.go
The file `internal/graph/rank.go` exists and exports a `PageRank(nodes []string, edges []Edge, damping float64, iterations int) map[string]float64` function.
- **Tool:** go-test (or file existence check)
- **Evidence:** `internal/graph/rank.go` compiles and `PageRank()` is callable

## VAL-FEAT-008: PageRank scores are distributed with node rankings
For a simple 3-node chain (A → B → C), `PageRank` assigns B a higher score than A or C (due to inbound link centrality), and the sum of all scores approximates 1.0.
- **Tool:** go-test
- **Evidence:** A test with a known graph topology validates correct PageRank ordering and score distribution

## VAL-FEAT-009: Degree centrality is provided alongside PageRank
The `rank.go` file exports a `DegreeCentrality(nodes []string, edges []Edge) map[string]float64` function that returns normalized degree scores (in-degree + out-degree / max degree).
- **Tool:** go-test
- **Evidence:** A test with a star graph (center node connected to 5 leaves) returns 1.0 for the center node and lower scores for leaf nodes

---

## VAL-FEAT-010: WatchStale gRPC streaming method is registered
The `grpc.ServiceDesc` in `grpc.go` includes a `WatchStale` stream alongside the existing `WatchMemories` stream.
- **Tool:** go-test (or grpcurl)
- **Evidence:** `grpc.ServiceDesc.Streams` contains `{StreamName: "WatchStale", Handler: s.grpcWatchStale, ServerStreams: true}`

## VAL-FEAT-011: WatchStale emits StaleEvent on git change
After wiring a `git.Watcher` to the gRPC server, a git commit that changes files tracked in `file_watch` causes the `WatchStale` stream to emit a `StaleEvent` containing the changed file path and affected node IDs.
- **Tool:** go-test (integration test with gRPC client and git operations in temp dir)
- **Evidence:** The gRPC streaming client receives a `StaleEvent` message within a configurable polling interval after the git change

---

## VAL-FEAT-012: Paginated vector search replaces AllEmbeddings()
The `vectorSearch` function (or equivalent) uses a paginated/batched SQL query (e.g., `SELECT node_id, vector FROM embeddings ORDER BY node_id LIMIT ? OFFSET ?`) instead of loading all embeddings at once via `AllEmbeddings()`.
- **Tool:** go-test
- **Evidence:** `AllEmbeddings()` is no longer called by `vectorSearch`; instead, a `GetEmbeddingsBatch(offset, limit)` method exists and is used

## VAL-FEAT-013: Paginated search supports configurable page size
The vector search function accepts a `batchSize` or `pageSize` parameter, and the implementation processes pages sequentially until `limit` results are accumulated or all embeddings are exhausted.
- **Tool:** go-test
- **Evidence:** A test with 25 stored embeddings and a page size of 10 produces the same correct top-5 results as loading all 25 at once

## VAL-FEAT-014: Pagination does not change search result quality
Running hybrid search with the paginated vector search produces the same scored results (top-10 node IDs) as running it with the original `AllEmbeddings()` approach, for a fixed dataset.
- **Tool:** go-test
- **Evidence:** Two identical searches (one using old approach, one using paginated) return the same top-10 node IDs and scores within floating-point tolerance

---

## VAL-FEAT-015: Memory promotion pipeline boosts frequently accessed nodes
The pipeline periodically identifies nodes with `access_count` above a configurable threshold and promotes their `tier` (e.g., tier 2→1), boosting their visibility in context/recall.
- **Tool:** go-test
- **Evidence:** After calling `Remember` + `Recall` (which increments `access_count`) 10+ times on a warm-tier node, calling a promotion run upgrades its `tier` from 2 to 1

## VAL-FEAT-016: Promotion is configurable (threshold + batch size)
The promotion pipeline exposes configuration: a minimum access count threshold and a max batch size per run. A run with threshold=5 processes only nodes with `access_count >= 5`, and at most `batchSize` nodes per invocation.
- **Tool:** go-test
- **Evidence:** Setting threshold=20 causes zero promotions when max access_count is 10; lowering threshold to 8 causes promotions for nodes with access_count ≥ 8

## VAL-FEAT-017: Promotion pipeline is safe to run concurrently
Running the promotion pipeline concurrently with memory writes does not cause panics or data races (verified via `go test -race`).
- **Tool:** go-test (with `-race` flag)
- **Evidence:** A test that starts the promotion pipeline in a goroutine while another goroutine continuously remembers + recalls passes the race detector
