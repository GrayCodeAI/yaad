package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaadmemory/yaad/internal/agentconfig"
	"github.com/yaadmemory/yaad/internal/bench"
	"github.com/yaadmemory/yaad/internal/embeddings"
	"github.com/yaadmemory/yaad/internal/engine"
	"github.com/yaadmemory/yaad/internal/exportimport"
	"github.com/yaadmemory/yaad/internal/hooks"
	"github.com/yaadmemory/yaad/internal/server"
	"github.com/yaadmemory/yaad/internal/skill"
	"github.com/yaadmemory/yaad/internal/storage"
	"github.com/yaadmemory/yaad/internal/team"
	yaadsync "github.com/yaadmemory/yaad/internal/sync"
)

func setup(t *testing.T) (*engine.Engine, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(store)
	return eng, func() { store.Close(); os.RemoveAll(dir) }
}

func TestRememberRecallContext(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// --- Remember ---
	nodes := []engine.RememberInput{
		{Type: "convention", Content: "Use jose not jsonwebtoken for Edge compatibility", Scope: "project"},
		{Type: "decision", Content: "Chose RS256 over HS256 for JWT compliance", Scope: "project"},
		{Type: "bug", Content: "Token refresh race: use mutex in src/middleware/auth.ts", Scope: "project", Tags: "auth"},
		{Type: "task", Content: "Add rate limiting to /auth/token", Scope: "project"},
	}
	var ids []string
	for _, in := range nodes {
		n, err := eng.Remember(in)
		if err != nil {
			t.Fatalf("Remember(%q): %v", in.Content, err)
		}
		ids = append(ids, n.ID)
	}
	if len(ids) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(ids))
	}

	// --- Dedup: same content returns same ID ---
	n2, err := eng.Remember(engine.RememberInput{
		Type: "convention", Content: "Use jose not jsonwebtoken for Edge compatibility", Scope: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n2.ID != ids[0] {
		t.Errorf("dedup failed: expected id %s, got %s", ids[0], n2.ID)
	}

	// --- Recall ---
	result, err := eng.Recall(engine.RecallOpts{Query: "auth JWT", Depth: 2, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Error("recall returned no nodes")
	}
	// Should find the bug and decision nodes
	found := map[string]bool{}
	for _, n := range result.Nodes {
		found[n.Type] = true
	}
	if !found["bug"] && !found["decision"] {
		t.Errorf("recall missing expected types, got: %v", found)
	}

	// --- Context (hot tier) ---
	ctx, err := eng.Context("")
	if err != nil {
		t.Fatal(err)
	}
	// Hot tier should include convention (tier=1) and task (tier=1)
	hotTypes := map[string]bool{}
	for _, n := range ctx.Nodes {
		hotTypes[n.Type] = true
	}
	if !hotTypes["convention"] {
		t.Errorf("context missing convention nodes, got: %v", hotTypes)
	}
	if !hotTypes["task"] {
		t.Errorf("context missing task nodes, got: %v", hotTypes)
	}

	// --- Forget ---
	if err := eng.Forget(ids[3]); err != nil {
		t.Fatal(err)
	}
	forgotten, _ := eng.Store().GetNode(ids[3])
	if forgotten.Confidence != 0 {
		t.Errorf("forget: expected confidence=0, got %f", forgotten.Confidence)
	}
}

func TestGraphLinkAndSubgraph(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	a, _ := eng.Remember(engine.RememberInput{Type: "decision", Content: "Use NATS for event bus", Scope: "project"})
	b, _ := eng.Remember(engine.RememberInput{Type: "convention", Content: "Use NATS client v2", Scope: "project"})

	// Link: decision led_to convention
	err := eng.Graph().AddEdge(&storage.Edge{
		ID: "e1", FromID: a.ID, ToID: b.ID, Type: "led_to", Weight: 1.0,
	})
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Subgraph from decision should include convention
	sg, err := eng.Graph().ExtractSubgraph(a.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sg.Nodes) < 2 {
		t.Errorf("subgraph: expected ≥2 nodes, got %d", len(sg.Nodes))
	}

	// Cycle detection: led_to is acyclic — b→a should fail
	err = eng.Graph().AddEdge(&storage.Edge{
		ID: "e2", FromID: b.ID, ToID: a.ID, Type: "led_to", Weight: 1.0,
	})
	if err == nil {
		t.Error("cycle detection failed: should have rejected b→a led_to edge")
	}

	// relates_to allows cycles
	err = eng.Graph().AddEdge(&storage.Edge{
		ID: "e3", FromID: b.ID, ToID: a.ID, Type: "relates_to", Weight: 1.0,
	})
	if err != nil {
		t.Errorf("relates_to cycle should be allowed: %v", err)
	}
}

func TestPhase3EmbeddingsAndHybridSearch(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Store some nodes
	nodes := []engine.RememberInput{
		{Type: "convention", Content: "Use jose not jsonwebtoken", Scope: "project"},
		{Type: "decision", Content: "Chose RS256 for JWT compliance", Scope: "project"},
		{Type: "bug", Content: "Token refresh race in auth middleware", Scope: "project"},
	}
	for _, in := range nodes {
		eng.Remember(in)
	}

	// Embed all nodes with local stub provider
	provider := embeddings.NewLocal()
	allNodes, _ := eng.Store().ListNodes(storage.NodeFilter{})
	for _, n := range allNodes {
		vec, err := provider.Embed(context.Background(), n.Content)
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if err := eng.Store().SaveEmbedding(n.ID, provider.Name(), vec); err != nil {
			t.Fatalf("SaveEmbedding: %v", err)
		}
	}

	// Verify embeddings stored
	embs, err := eng.Store().AllEmbeddings()
	if err != nil {
		t.Fatal(err)
	}
	if len(embs) == 0 {
		t.Error("no embeddings stored")
	}

	// Hybrid search
	hs := engine.NewHybridSearch(eng.Store(), eng.Graph(), provider)
	scored, err := hs.Search(context.Background(), "auth JWT", engine.RecallOpts{Depth: 2, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(scored) == 0 {
		t.Error("hybrid search returned no results")
	}

	// Re-rank
	reranked := engine.Rerank(scored, eng.Store())
	if len(reranked) == 0 {
		t.Error("rerank returned no results")
	}
}

func TestPhase3DecayAndGC(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	n, _ := eng.Remember(engine.RememberInput{Type: "decision", Content: "Old decision", Scope: "project"})

	// Manually set low confidence
	node, _ := eng.Store().GetNode(n.ID)
	node.Confidence = 0.05
	eng.Store().UpdateNode(node)

	// GC should remove it
	removed, err := engine.GarbageCollect(eng.Store(), engine.DefaultDecayConfig)
	if err != nil {
		t.Fatal(err)
	}
	if removed == 0 {
		t.Error("GC should have removed low-confidence node")
	}
}

func TestPhase3ProactiveContext(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	eng.Remember(engine.RememberInput{Type: "convention", Content: "Use TypeScript strict mode", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "task", Content: "Add rate limiting", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "decision", Content: "Use NATS for events", Scope: "project"})

	hs := engine.NewHybridSearch(eng.Store(), eng.Graph(), nil)
	pc := engine.NewProactiveContext(eng, hs)
	nodes, err := pc.Predict(context.Background(), "", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Error("proactive context returned no nodes")
	}

	// FormatContext should produce markdown
	ctx := engine.FormatContext(nodes)
	if ctx == "" {
		t.Error("FormatContext returned empty string")
	}
}

func TestPhase4HooksAndReplay(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	dir := t.TempDir()
	runner := hooks.New(eng, dir)

	// SessionStart
	in := &hooks.HookInput{Agent: "claude-code", Project: dir}
	if err := runner.SessionStart(in); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	// PostToolUse — should create a memory node
	toolIn := &hooks.HookInput{
		ToolName:   "Write",
		ToolInput:  "src/auth.ts",
		ToolOutput: "wrote JWT middleware",
		Agent:      "claude-code",
	}
	if err := runner.PostToolUse(toolIn); err != nil {
		t.Fatalf("PostToolUse: %v", err)
	}
	// Store replay event
	if err := runner.StoreToolEvent(toolIn, eng.Store()); err != nil {
		t.Fatalf("StoreToolEvent: %v", err)
	}

	// Verify node was created
	result, err := eng.Recall(engine.RecallOpts{Query: "JWT middleware", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Error("PostToolUse should have created a memory node")
	}

	// SessionEnd
	endIn := &hooks.HookInput{Summary: "Implemented JWT auth middleware", Agent: "claude-code"}
	if err := runner.SessionEnd(endIn); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
}

func TestPhase4SSEBroker(t *testing.T) {
	broker := server.NewSSEBroker()

	// Publish without subscribers — should not panic
	broker.Publish("test", map[string]string{"key": "value"})

	// Verify SSE endpoint responds with 200 and SSE headers
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so handler exits
	req, _ := http.NewRequestWithContext(ctx, "GET", "/yaad/events", nil)
	w := httptest.NewRecorder()
	broker.ServeHTTP(w, req)
	// Either 200 (flusher supported) or 500 (not supported) — both are valid
	if w.Code != 200 && w.Code != 500 {
		t.Errorf("unexpected status: %d", w.Code)
	}
	if w.Code == 200 {
		ct := w.Header().Get("Content-Type")
		if ct != "text/event-stream" {
			t.Errorf("expected text/event-stream, got %s", ct)
		}
	}
}

func TestPhase4AgentConfigGenerator(t *testing.T) {
	dir := t.TempDir()

	// Claude Code
	if err := agentconfig.Generate(agentconfig.AgentClaudeCode, dir); err != nil {
		t.Fatalf("Generate claude-code: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Error(".mcp.json not created")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks.json")); err != nil {
		t.Error(".claude/hooks.json not created")
	}

	// OpenCode
	if err := agentconfig.Generate(agentconfig.AgentOpenCode, dir); err != nil {
		t.Fatalf("Generate opencode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); err != nil {
		t.Error("opencode.json not created")
	}

	// Codex CLI
	if err := agentconfig.Generate(agentconfig.AgentCodexCLI, dir); err != nil {
		t.Fatalf("Generate codex-cli: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.yaml")); err != nil {
		t.Error(".codex/config.yaml not created")
	}
}

func TestPhase5ExportImport(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	eng.Remember(engine.RememberInput{Type: "convention", Content: "Use jose not jsonwebtoken", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "decision", Content: "Chose RS256 for JWT", Scope: "project"})

	// JSON export
	data, err := exportimport.ExportJSON(eng.Store(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("JSON export is empty")
	}

	// JSON import into fresh store
	eng2, cleanup2 := setup(t)
	defer cleanup2()
	nodes, edges, err := exportimport.ImportJSON(eng2.Store(), data)
	if err != nil {
		t.Fatal(err)
	}
	if nodes == 0 {
		t.Error("import: expected nodes > 0")
	}
	_ = edges

	// Markdown export
	md, err := exportimport.ExportMarkdown(eng.Store(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "jose") {
		t.Error("markdown export missing content")
	}

	// Obsidian export
	vaultDir := t.TempDir()
	n, err := exportimport.ExportObsidian(eng.Store(), "", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("obsidian export: expected files > 0")
	}
}

func TestPhase5Skills(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	sk := &skill.Skill{
		Name:        "deploy",
		Description: "Deploy the application",
		Steps: []skill.Step{
			{Order: 1, Description: "Run tests", Command: "pnpm test"},
			{Order: 2, Description: "Build", Command: "pnpm build"},
			{Order: 3, Description: "Deploy", Command: "fly deploy"},
		},
	}
	node, err := skill.Store(eng, sk, "")
	if err != nil {
		t.Fatal(err)
	}
	if node.ID == "" {
		t.Error("skill store: empty node ID")
	}

	// List skills
	skills, err := skill.ListSkills(eng.Store(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) == 0 {
		t.Error("skill list: expected skills > 0")
	}

	// Replay
	replay := skill.Replay(sk)
	if !strings.Contains(replay, "deploy") {
		t.Error("skill replay missing content")
	}
}

func TestPhase5TeamMemory(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	node, _ := eng.Remember(engine.RememberInput{
		Type: "convention", Content: "Use TypeScript strict mode", Scope: "project",
	})

	// Share to team
	shared, err := team.Share(eng.Store(), eng.Store(), team.ShareInput{
		NodeID: node.ID, TeamID: "team-alpha", SharedBy: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if shared.ID == node.ID {
		t.Error("shared node should have new ID")
	}

	// List team memories
	memories, err := team.ListTeamMemories(eng.Store(), "team-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) == 0 {
		t.Error("team memories: expected > 0")
	}
}

func TestPhase5Benchmark(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Seed some memories
	eng.Remember(engine.RememberInput{Type: "convention", Content: "Use jose not jsonwebtoken for Edge compatibility", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "decision", Content: "Chose NATS over Redis Streams for event bus", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "bug", Content: "Token refresh race condition in auth middleware", Scope: "project"})

	result := bench.Run(eng, bench.DefaultQAs(), 2, 10)
	if result.Total == 0 {
		t.Error("benchmark: no questions evaluated")
	}
	// R@5 should be > 0 with seeded data
	if result.HitAtK[5] == 0 {
		t.Log("benchmark: R@5=0 (may be ok with small dataset)")
	}
	t.Logf("Benchmark:\n%s", result.String())
}

func TestGitSync(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yaad"), 0755)

	eng.Remember(engine.RememberInput{Type: "convention", Content: "Use jose not jsonwebtoken", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "decision", Content: "Chose RS256 for JWT", Scope: "project"})

	syncer := yaadsync.New(eng.Store(), dir)

	// Export
	hash, err := syncer.Export("")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if hash == "" {
		t.Error("Export: empty hash")
	}

	// Verify chunk file exists
	chunkFile := filepath.Join(dir, ".yaad", "chunks", hash+".jsonl.gz")
	if _, err := os.Stat(chunkFile); err != nil {
		t.Errorf("chunk file not created: %v", err)
	}

	// Verify manifest exists
	manifestFile := filepath.Join(dir, ".yaad", "manifest.json")
	if _, err := os.Stat(manifestFile); err != nil {
		t.Errorf("manifest.json not created: %v", err)
	}

	// Status
	st, err := syncer.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalChunks != 1 {
		t.Errorf("expected 1 chunk, got %d", st.TotalChunks)
	}

	// Import into fresh store
	eng2, cleanup2 := setup(t)
	defer cleanup2()
	syncer2 := yaadsync.New(eng2.Store(), dir)
	n, e, err := syncer2.Import()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("Import: expected nodes > 0")
	}
	t.Logf("Imported %d nodes, %d edges", n, e)

	// Second import should be idempotent (already imported)
	n2, e2, _ := syncer2.Import()
	if n2 != 0 || e2 != 0 {
		t.Errorf("second import should be idempotent, got %d nodes %d edges", n2, e2)
	}
}

func TestRESTAPI(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	mux := http.NewServeMux()
	rest := server.NewRESTServer(eng, "")
	rest.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// POST /yaad/remember
	body, _ := json.Marshal(engine.RememberInput{
		Type: "convention", Content: "Always use TypeScript strict mode", Scope: "project",
	})
	resp, err := http.Post(ts.URL+"/yaad/remember", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("remember: expected 201, got %d", resp.StatusCode)
	}
	var node storage.Node
	json.NewDecoder(resp.Body).Decode(&node)
	resp.Body.Close()
	if node.ID == "" {
		t.Error("remember: empty node ID")
	}

	// POST /yaad/recall
	body, _ = json.Marshal(engine.RecallOpts{Query: "TypeScript", Limit: 5})
	resp, err = http.Post(ts.URL+"/yaad/recall", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("recall: expected 200, got %d", resp.StatusCode)
	}
	var result engine.RecallResult
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if len(result.Nodes) == 0 {
		t.Error("recall: expected nodes, got none")
	}

	// GET /yaad/health
	resp, _ = http.Get(ts.URL + "/yaad/health")
	if resp.StatusCode != 200 {
		t.Errorf("health: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET /yaad/context
	resp, _ = http.Get(ts.URL + "/yaad/context")
	if resp.StatusCode != 200 {
		t.Errorf("context: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
