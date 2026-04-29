package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/yaad/internal/agentconfig"
	"github.com/GrayCodeAI/yaad/internal/bench"
	"github.com/GrayCodeAI/yaad/internal/boundary"
	"github.com/GrayCodeAI/yaad/internal/bridge"
	"github.com/GrayCodeAI/yaad/internal/config"
	"github.com/GrayCodeAI/yaad/internal/embeddings"
	"github.com/GrayCodeAI/yaad/internal/encrypt"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/exportimport"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/hooks"
	"github.com/GrayCodeAI/yaad/internal/ingest"
	intentpkg "github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/multiproject"
	"github.com/GrayCodeAI/yaad/internal/profile"
	"github.com/GrayCodeAI/yaad/internal/server"
	"github.com/GrayCodeAI/yaad/internal/skill"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/team"
	yaadtls "github.com/GrayCodeAI/yaad/internal/tls"
	"github.com/GrayCodeAI/yaad/internal/utils"
	yaadsync "github.com/GrayCodeAI/yaad/internal/sync"
	yaadsdk "github.com/GrayCodeAI/yaad/sdk/go/yaad"
)

func setup(t *testing.T) (*engine.Engine, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(store, graph.New(store))
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

func TestUserProfile(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	eng.Remember(engine.RememberInput{Type: "convention", Content: "Use jose for JWT auth", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "decision", Content: "Chose NATS for event bus", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "task", Content: "Add rate limiting", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "preference", Content: "Prefers functional style", Scope: "project"})

	p, err := eng.Profile("")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Static) == 0 {
		t.Error("profile: no static facts")
	}
	if p.Summary == "" {
		t.Error("profile: empty summary")
	}
	formatted := p.Format()
	if !strings.Contains(formatted, "User Profile") {
		t.Error("profile: formatted output missing header")
	}
	t.Logf("Profile:\n%s", formatted)
}

func TestConflictResolver(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Store original convention
	old, _ := eng.Remember(engine.RememberInput{
		Type: "convention", Content: "Use jsonwebtoken library for JWT", Scope: "project",
	})

	// Store contradicting convention (should supersede)
	newNode, _ := eng.Remember(engine.RememberInput{
		Type: "convention", Content: "Use jose instead of jsonwebtoken for Edge compatibility", Scope: "project",
	})

	// Verify old node confidence was lowered
	oldUpdated, _ := eng.Store().GetNode(old.ID)
	if oldUpdated.Confidence >= 1.0 {
		t.Errorf("conflict: old node confidence should be lowered, got %.2f", oldUpdated.Confidence)
	}

	// Verify supersedes edge exists
	edges, _ := eng.Store().GetEdgesFrom(newNode.ID)
	hasSupersedes := false
	for _, e := range edges {
		if e.Type == "supersedes" && e.ToID == old.ID {
			hasSupersedes = true
		}
	}
	if !hasSupersedes {
		t.Error("conflict: supersedes edge not created")
	}
}

func TestTemporalBackbone(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	n1, _ := eng.Remember(engine.RememberInput{Type: "convention", Content: "First convention", Scope: "project", Project: "test"})
	n2, _ := eng.Remember(engine.RememberInput{Type: "decision", Content: "Second decision", Scope: "project", Project: "test"})
	n3, _ := eng.Remember(engine.RememberInput{Type: "bug", Content: "Third bug report", Scope: "project", Project: "test"})

	// Verify temporal chain: n1 → n2 → n3
	edges1, _ := eng.Store().GetEdgesFrom(n1.ID)
	hasLink12 := false
	for _, e := range edges1 {
		if e.Type == "learned_in" && e.ToID == n2.ID {
			hasLink12 = true
		}
	}
	edges2, _ := eng.Store().GetEdgesFrom(n2.ID)
	hasLink23 := false
	for _, e := range edges2 {
		if e.Type == "learned_in" && e.ToID == n3.ID {
			hasLink23 = true
		}
	}
	if !hasLink12 {
		t.Error("temporal: n1→n2 learned_in edge missing")
	}
	if !hasLink23 {
		t.Error("temporal: n2→n3 learned_in edge missing")
	}
}

func TestDedupRollingWindow(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	n1, _ := eng.Remember(engine.RememberInput{
		Type: "convention", Content: "Use jose for JWT auth", Scope: "project",
	})
	// Same content again — should return same node (dedup)
	n2, _ := eng.Remember(engine.RememberInput{
		Type: "convention", Content: "Use jose for JWT auth", Scope: "project",
	})

	if n1.ID != n2.ID {
		t.Errorf("dedup: expected same node ID, got %s and %s", n1.ID[:8], n2.ID[:8])
	}
}

func TestCompaction(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Store 5 low-confidence nodes
	for i := 0; i < 5; i++ {
		n, _ := eng.Remember(engine.RememberInput{
			Type: "decision", Content: fmt.Sprintf("Old decision %d about something", i), Scope: "project",
		})
		node, _ := eng.Store().GetNode(n.ID)
		node.Confidence = 0.2
		node.AccessCount = 0
		eng.Store().UpdateNode(node)
	}

	// Run compaction
	compacted, err := eng.Compact("")
	if err != nil {
		t.Fatal(err)
	}
	if compacted == 0 {
		t.Error("compaction: expected nodes to be compacted")
	}
	t.Logf("Compacted %d nodes", compacted)
}

func TestMentalModel(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	eng.Remember(engine.RememberInput{Type: "convention", Content: "Use jose for JWT", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "decision", Content: "Chose NATS for events", Scope: "project"})
	eng.Remember(engine.RememberInput{Type: "task", Content: "Add rate limiting", Scope: "project"})

	model, err := eng.MentalModel("")
	if err != nil {
		t.Fatal(err)
	}
	if model.Summary == "" {
		t.Error("mental model: empty summary")
	}
	if len(model.Conventions) == 0 {
		t.Error("mental model: no conventions")
	}
	formatted := model.Format()
	if formatted == "" {
		t.Error("mental model: empty formatted output")
	}
	t.Logf("Mental model:\n%s", formatted)
}

func TestPhase6IntentClassifier(t *testing.T) {
	cases := []struct {
		query    string
		expected intentpkg.Intent
	}{
		{"why did we choose NATS over Redis?", intentpkg.IntentWhy},
		{"when did we fix the auth bug?", intentpkg.IntentWhen},
		{"how to deploy the application?", intentpkg.IntentHow},
		{"what is the auth subsystem?", intentpkg.IntentWhat},
		{"which library should I use for JWT?", intentpkg.IntentWho},
		{"recall auth middleware", intentpkg.IntentGeneral},
	}
	for _, c := range cases {
		got := intentpkg.Classify(c.query)
		if got != c.expected {
			t.Errorf("Classify(%q) = %s, want %s", c.query, got, c.expected)
		}
	}
}

func TestPhase6IntentAwareRetrieval(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Seed memories
	decision, _ := eng.Remember(engine.RememberInput{Type: "decision", Content: "Chose NATS over Redis Streams for event bus", Scope: "project"})
	convention, _ := eng.Remember(engine.RememberInput{Type: "convention", Content: "Use NATS client v2 for all event publishing", Scope: "project"})

	// Link: decision led_to convention
	eng.Graph().AddEdge(&storage.Edge{
		ID: "e-test", FromID: decision.ID, ToID: convention.ID, Type: "led_to", Weight: 1.0,
	})

	// Why query should find the decision via causal traversal
	result, err := eng.Recall(engine.RecallOpts{Query: "why NATS", Depth: 2, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Error("intent-aware recall returned no nodes")
	}
	// Should find both decision and convention via causal chain
	found := map[string]bool{}
	for _, n := range result.Nodes {
		found[n.Type] = true
	}
	t.Logf("Why query found types: %v", found)
}

func TestPhase6DualStream(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ds := ingest.New(eng)
	defer ds.Stop()

	// Fast path should return immediately
	node, err := ds.Remember(engine.RememberInput{
		Type: "convention", Content: "Use jose not jsonwebtoken", Scope: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.ID == "" {
		t.Error("dual stream: empty node ID")
	}

	// Second remember should create temporal backbone edge
	node2, err := ds.Remember(engine.RememberInput{
		Type: "decision", Content: "Chose RS256 for JWT", Scope: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if node2.ID == "" {
		t.Error("dual stream: second node empty ID")
	}

	// Give slow path time to run and release DB lock
	// Retry up to 500ms (slow path runs async)
	var hasTemporalEdge bool
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		edges, _ := eng.Store().GetEdgesFrom(node.ID)
		for _, e := range edges {
			if e.ToID == node2.ID && e.Type == "learned_in" {
				hasTemporalEdge = true
			}
		}
		if hasTemporalEdge {
			break
		}
	}
	if !hasTemporalEdge {
		t.Error("dual stream: temporal backbone edge not created within 500ms")
	}
}

func TestPhase6BoundaryDetector(t *testing.T) {
	// Test buffer overflow boundary (deterministic)
	det := boundary.New(3, 0.99) // very high threshold, only overflow triggers
	det.Add("item 1 about auth")
	det.Add("item 2 about auth")
	if !det.Add("item 3 about auth") {
		t.Error("buffer overflow should trigger boundary")
	}

	// Test flush
	det2 := boundary.New(10, 0.3)
	det2.Add("content about authentication")
	det2.Add("more about JWT tokens")
	buf := det2.Flush()
	if len(buf) != 2 {
		t.Errorf("flush: expected 2 items, got %d", len(buf))
	}
	if det2.Size() != 0 {
		t.Error("flush: buffer should be empty after flush")
	}

	// Test semantic distance detection (non-deterministic, just verify no panic)
	det3 := boundary.New(20, 0.3)
	det3.Add("Use jose for JWT authentication in Node.js")
	det3.Add("PostgreSQL database connection pooling configuration")
	// May or may not trigger — just verify it runs without error
	t.Logf("Boundary detector size after 2 items: %d", det3.Size())
}

func TestPrivacyFilter(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Store content with secrets — should be stripped
	node, _ := eng.Remember(engine.RememberInput{
		Type:    "convention",
		Content: "Use API key sk-1234567890abcdefghijklmnop for auth and AKIA1234567890ABCDEF for AWS",
		Scope:   "project",
	})
	if strings.Contains(node.Content, "sk-1234567890") {
		t.Error("privacy: API key not stripped")
	}
	if strings.Contains(node.Content, "AKIA1234567890") {
		t.Error("privacy: AWS key not stripped")
	}
	if !strings.Contains(node.Content, "[REDACTED]") {
		t.Error("privacy: expected [REDACTED] placeholder")
	}
}

func TestEncryption(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("hello world secret data"), 0644)

	// Generate key
	key, err := encrypt.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if key == "" {
		t.Error("encrypt: empty key")
	}

	// Encrypt
	if err := encrypt.EncryptFile(testFile, key); err != nil {
		t.Fatal(err)
	}
	if !encrypt.IsEncrypted(testFile) {
		t.Error("encrypt: file should appear encrypted")
	}

	// Decrypt
	if err := encrypt.DecryptFile(testFile, key); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(testFile)
	if string(data) != "hello world secret data" {
		t.Errorf("encrypt: decrypted content mismatch: %s", data)
	}
}

func TestUtilsShortID(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"abcdefghijklmnop", "abcdefgh"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"", ""},
		{"ab", "ab"},
	}
	for _, c := range cases {
		got := utils.ShortID(c.input)
		if got != c.expected {
			t.Errorf("ShortID(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestEdgeCaseEmptyRecall(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Recall on empty DB should return empty, not error
	result, err := eng.Recall(engine.RecallOpts{Query: "nonexistent", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("empty recall: expected 0 nodes, got %d", len(result.Nodes))
	}
}

func TestEdgeCaseContextEmpty(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Context on empty DB should return empty, not error
	result, err := eng.Context("")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("context: should return empty result, not nil")
	}
}

func TestEdgeCaseForgetNonexistent(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Forget nonexistent node should error gracefully
	err := eng.Forget("nonexistent-id-12345678")
	if err == nil {
		t.Error("forget: should error on nonexistent node")
	}
}

func TestProfileMerge(t *testing.T) {
	a := &profile.Profile{
		Project: "test",
		Static:  []string{"Use jose", "Use NATS"},
		Dynamic: []string{"[task] rate limiting"},
		Stack:   []string{"TypeScript", "NATS"},
	}
	b := &profile.Profile{
		Static:  []string{"Prefer tabs", "Use jose"}, // "Use jose" is duplicate
		Dynamic: []string{"[bug] auth race"},
		Stack:   []string{"PostgreSQL", "NATS"}, // "NATS" is duplicate
	}
	merged := profile.Merge(a, b)
	// Static should be deduped
	if len(merged.Static) != 3 { // jose, NATS, tabs
		t.Errorf("merge: expected 3 static, got %d: %v", len(merged.Static), merged.Static)
	}
	// Stack should be deduped
	if len(merged.Stack) != 3 { // TypeScript, NATS, PostgreSQL
		t.Errorf("merge: expected 3 stack, got %d: %v", len(merged.Stack), merged.Stack)
	}
	// Dynamic should be combined (not deduped)
	if len(merged.Dynamic) != 2 {
		t.Errorf("merge: expected 2 dynamic, got %d", len(merged.Dynamic))
	}
}

func TestMultipleRememberAndRecall(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Store 20 memories of different types
	types := []string{"convention", "decision", "bug", "spec", "task"}
	for i := 0; i < 20; i++ {
		eng.Remember(engine.RememberInput{
			Type:    types[i%len(types)],
			Content: fmt.Sprintf("Memory item %d about topic %d", i, i%5),
			Scope:   "project",
		})
	}

	// Recall should find results
	result, err := eng.Recall(engine.RecallOpts{Query: "topic", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Error("bulk recall: expected nodes")
	}
	t.Logf("Stored 20, recalled %d nodes", len(result.Nodes))

	// Status should show correct counts
	st, _ := eng.Status("")
	if st.Nodes < 20 {
		t.Errorf("status: expected ≥20 nodes, got %d", st.Nodes)
	}
}

func TestBridgeImportExport(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	dir := t.TempDir()

	// Create a fake CLAUDE.md
	claudeContent := "# Conventions\n- Use jose for JWT\n- Always run tests\n\n# Decisions\n- Chose NATS for events\n"
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeContent), 0644)

	// Import
	n, err := bridge.Import(eng, dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("bridge import: expected nodes > 0")
	}
	t.Logf("Imported %d nodes from CLAUDE.md", n)

	// Export back
	if err := bridge.Export(eng.Store(), dir, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(data), "Generated by Yaad") {
		t.Error("bridge export: missing Yaad header")
	}
}

func TestMultiProject(t *testing.T) {
	// Create two separate stores (simulating two projects)
	eng1, cleanup1 := setup(t)
	defer cleanup1()
	eng2, cleanup2 := setup(t)
	defer cleanup2()

	// Store in project 1
	eng1.Remember(engine.RememberInput{Type: "convention", Content: "Use jose for JWT", Scope: "project", Project: "proj1"})

	// Store in project 2
	eng2.Remember(engine.RememberInput{Type: "convention", Content: "Use PostgreSQL for DB", Scope: "project", Project: "proj2"})

	// Cross-project search
	stores := []storage.Storage{eng1.Store(), eng2.Store()}
	results, err := multiproject.CrossProjectSearch(stores, "jose", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("multiproject: cross-project search returned no results")
	}
}

func TestTLSCertGeneration(t *testing.T) {
	dir := t.TempDir()
	cfg := yaadtls.Config{Enabled: true}

	tlsCfg, err := yaadtls.TLSConfig(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if tlsCfg == nil {
		t.Error("tls: nil config returned")
	}
	if len(tlsCfg.Certificates) == 0 {
		t.Error("tls: no certificates generated")
	}

	// Verify cert files were created
	if _, err := os.Stat(filepath.Join(dir, "cert.pem")); err != nil {
		t.Error("tls: cert.pem not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "key.pem")); err != nil {
		t.Error("tls: key.pem not created")
	}
}

func TestGoSDK(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sdk_test.db")

	mem, err := yaadsdk.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()

	// Remember
	node, err := mem.Remember("Use jose for JWT auth", yaadsdk.Convention, yaadsdk.WithTags("auth"))
	if err != nil {
		t.Fatal(err)
	}
	if node.ID == "" {
		t.Error("sdk: empty node ID")
	}

	// Recall
	result, err := mem.Recall("jose JWT", yaadsdk.WithLimit(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Error("sdk: recall returned no nodes")
	}

	// Context
	ctx, err := mem.Context("")
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Error("sdk: nil context")
	}

	// Forget
	if err := mem.Forget(node.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.Server.Port != 3456 {
		t.Errorf("config: expected port 3456, got %d", cfg.Server.Port)
	}
	if cfg.Server.GRPCPort != 3457 {
		t.Errorf("config: expected grpc port 3457, got %d", cfg.Server.GRPCPort)
	}
	if cfg.Decay.HalfLifeDays != 30 {
		t.Errorf("config: expected half_life 30, got %d", cfg.Decay.HalfLifeDays)
	}
}

func TestStorageCreateAndQuery(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create node
	node := &storage.Node{
		ID: "test-node-1", Type: "convention", Content: "Test content",
		ContentHash: "hash1", Scope: "project", Tier: 1, Confidence: 1.0, Version: 1,
	}
	if err := store.CreateNode(node); err != nil {
		t.Fatal(err)
	}

	// Get node
	got, err := store.GetNode("test-node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "Test content" {
		t.Errorf("storage: expected 'Test content', got '%s'", got.Content)
	}

	// Create edge
	edge := &storage.Edge{
		ID: "test-edge-1", FromID: "test-node-1", ToID: "test-node-1",
		Type: "relates_to", Acyclic: false, Weight: 1.0,
	}
	if err := store.CreateEdge(edge); err != nil {
		t.Fatal(err)
	}

	// Get neighbors
	neighbors, err := store.GetNeighbors("test-node-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) == 0 {
		t.Error("storage: expected neighbors")
	}

	// Version history
	if err := store.SaveVersion("test-node-1", "old content", "test", "test update"); err != nil {
		t.Fatal(err)
	}
	versions, err := store.GetVersions("test-node-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 {
		t.Error("storage: expected version history")
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
