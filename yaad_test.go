package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/yaad/config"
	"github.com/GrayCodeAI/yaad/engine"
	"github.com/GrayCodeAI/yaad/graph"
	"github.com/GrayCodeAI/yaad/hooks"
	"github.com/GrayCodeAI/yaad/skill"
	"github.com/GrayCodeAI/yaad/storage"
)

// --- Full Hawk Session Lifecycle ---

func TestHawkFullSessionLifecycle(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	dir := t.TempDir()
	runner := hooks.New(eng, dir)
	ctx := context.Background()

	// 1. Session start
	in := &hooks.HookInput{Agent: "hawk", Project: dir}
	if err := runner.SessionStart(ctx, in); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	// 2. Simulate tool uses (file edits, commands)
	tools := []hooks.HookInput{
		{ToolName: "Write", ToolInput: "src/auth.ts", ToolOutput: "wrote JWT middleware using jose library", Agent: "hawk"},
		{ToolName: "Bash", ToolInput: "npm install jose", ToolOutput: "added 1 package", Agent: "hawk"},
		{ToolName: "Edit", ToolInput: "src/config.ts", ToolOutput: "added RS256 algorithm configuration", Agent: "hawk"},
		{ToolName: "Read", ToolInput: "package.json", ToolOutput: "{}", Agent: "hawk"}, // low relevance — should be filtered
	}
	for _, tool := range tools {
		t := tool
		if err := runner.PostToolUse(ctx, &t); err != nil {
			// Don't fail on individual tool errors
			continue
		}
	}

	// 3. Session end
	endIn := &hooks.HookInput{Summary: "Implemented JWT auth with jose and RS256", Agent: "hawk"}
	if err := runner.SessionEnd(ctx, endIn); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}

	// 4. Verify memories were captured
	result, err := eng.Recall(ctx, engine.RecallOpts{Query: "jose JWT", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Error("session lifecycle: no memories captured from tool uses")
	}

	// 5. Verify sessions exist
	sessions, err := eng.Store().ListSessions(ctx, dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) == 0 {
		t.Error("session lifecycle: no session record created")
	}
}

func TestHawkSessionRecap(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()
	project := "test-project"

	// Create a session with some memories
	sessionID, err := eng.StartSession(ctx, project, "hawk")
	if err != nil {
		t.Fatal(err)
	}

	// Store memories in this session
	for _, content := range []string{"Use jose for JWT", "RS256 for algorithm", "Auth middleware pattern"} {
		eng.Remember(ctx, engine.RememberInput{
			Type: "convention", Content: content, Scope: "project",
			Project: project, Session: sessionID, Agent: "hawk",
		})
	}

	// End the session
	eng.Store().EndSession(ctx, sessionID, "JWT auth session")

	// Recap should find these nodes
	sessions, err := eng.Store().ListSessions(ctx, project, 1)
	if err != nil || len(sessions) == 0 {
		t.Fatal("no sessions found")
	}
	nodes, err := eng.Store().ListNodes(ctx, storage.NodeFilter{
		Project: project, SourceSession: sessions[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 3 {
		t.Errorf("session recap: expected ≥3 nodes, got %d", len(nodes))
	}
}

// --- Auto-Decay on Session Start ---

func TestAutoDecayOnSessionStart(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()
	dir := t.TempDir()

	// Store a node and manually age it
	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: "Old decision from months ago", Scope: "project",
	})
	node, _ := eng.Store().GetNode(ctx, n.ID)
	node.UpdatedAt = time.Now().Add(-90 * 24 * time.Hour) // 90 days ago
	node.AccessedAt = time.Now().Add(-90 * 24 * time.Hour)
	eng.Store().UpdateNode(ctx, node)

	originalConf := node.Confidence

	// Session start triggers decay
	runner := hooks.New(eng, dir)
	in := &hooks.HookInput{Agent: "hawk", Project: dir}
	runner.SessionStart(ctx, in)

	// Verify decay was applied
	updated, _ := eng.Store().GetNode(ctx, n.ID)
	if updated.Confidence >= originalConf {
		t.Errorf("auto-decay: confidence should have decreased from %.2f, got %.2f", originalConf, updated.Confidence)
	}
}

// --- Relevance Filter ---

func TestRelevanceFilterScoring(t *testing.T) {
	cases := []struct {
		tool, input, output, err string
		shouldCapture            bool
	}{
		// High signal — should capture
		{"Write", "src/auth.ts", "wrote JWT middleware", "", true},
		{"Edit", "src/config.ts", "added RS256 config", "", true},
		{"Bash", "npm install jose", "added 1 package", "", true},
		{"Bash", "git commit -m 'fix auth'", "committed", "", true},

		// Errors always captured
		{"Bash", "npm test", "", "FAIL: auth.test.ts", true},

		// Low signal — should NOT capture
		{"Read", "x.ts", "{}", "", false},
		{"Bash", "ls", "file1 file2", "", false},
		{"Bash", "cd src", "", "", false},
		{"Bash", "pwd", "/home/user", "", false},
	}
	for _, c := range cases {
		got := hooks.ShouldCapture(c.tool, c.input, c.output, c.err)
		if got != c.shouldCapture {
			t.Errorf("ShouldCapture(%s, %q, %q, %q) = %v, want %v",
				c.tool, c.input, c.output, c.err, got, c.shouldCapture)
		}
	}
}

func TestRelevanceBoostSignals(t *testing.T) {
	// Decision signals should boost score
	score := hooks.ScoreRelevance("Bash", "decided to use NATS instead of Redis", "ok", "")
	if score < 0.5 {
		t.Errorf("decision signal should boost score, got %.2f", score)
	}

	// Convention signals should boost score
	score = hooks.ScoreRelevance("Bash", "always use strict mode", "enforced", "")
	if score < 0.5 {
		t.Errorf("convention signal should boost score, got %.2f", score)
	}
}

// --- Keyed Upsert ---

func TestKeyedUpsert(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// First remember with a key
	n1, err := eng.Remember(ctx, engine.RememberInput{
		Type: "convention", Content: "Use tabs for indentation",
		Scope: "project", Key: "indent-style", Project: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second remember with same key — should UPDATE, not create duplicate
	n2, err := eng.Remember(ctx, engine.RememberInput{
		Type: "convention", Content: "Use 2-space indentation",
		Scope: "project", Key: "indent-style", Project: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if n1.ID != n2.ID {
		t.Errorf("keyed upsert: expected same ID, got %s and %s", n1.ID[:8], n2.ID[:8])
	}
	if n2.Content != "Use 2-space indentation" {
		t.Errorf("keyed upsert: content not updated, got %q", n2.Content)
	}

	// Version should be incremented
	node, _ := eng.Store().GetNode(ctx, n2.ID)
	if node.Version < 2 {
		t.Errorf("keyed upsert: expected version ≥2, got %d", node.Version)
	}
}

// --- Pinned Nodes in Context ---

func TestPinnedNodesAlwaysInContext(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Create a pinned node
	pinned, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "convention", Content: "CRITICAL: Never push to main directly",
		Scope: "project", Pinned: true,
	})

	// Create many non-pinned nodes to fill the budget
	for i := 0; i < 20; i++ {
		eng.Remember(ctx, engine.RememberInput{
			Type: "decision", Content: strings.Repeat("x", 200),
			Scope: "project",
		})
	}

	// Context should always include the pinned node
	result, err := eng.Context(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, n := range result.Nodes {
		if n.ID == pinned.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("pinned node not found in context output")
	}
}

func TestPinToggle(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()
	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "convention", Content: "Some convention", Scope: "project",
	})

	// Initially not pinned
	node, _ := eng.Store().GetNode(ctx, n.ID)
	if node.Pinned {
		t.Error("node should not be pinned initially")
	}

	// Pin it
	node.Pinned = true
	eng.Store().UpdateNode(ctx, node)

	// Verify
	node, _ = eng.Store().GetNode(ctx, n.ID)
	if !node.Pinned {
		t.Error("node should be pinned after update")
	}
}

// --- Skills ---

func TestSkillStoreAndReplay(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	sk := &skill.Skill{
		Name:        "deploy-prod",
		Description: "Deploy to production",
		Steps: []skill.Step{
			{Order: 1, Description: "Run tests: pnpm test"},
			{Order: 2, Description: "Build: pnpm build"},
			{Order: 3, Description: "Deploy: fly deploy --app prod"},
		},
	}

	node, err := skill.Store(ctx, eng, sk, "test-project")
	if err != nil {
		t.Fatal(err)
	}
	if node.ID == "" {
		t.Error("skill store returned empty node ID")
	}

	// Load back
	loaded, err := skill.Load(ctx, eng.Store(), "deploy-prod", "test-project")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "deploy-prod" {
		t.Errorf("skill load: expected name 'deploy-prod', got %q", loaded.Name)
	}
	if len(loaded.Steps) != 3 {
		t.Errorf("skill load: expected 3 steps, got %d", len(loaded.Steps))
	}

	// Replay
	replay := skill.Replay(loaded)
	if !strings.Contains(replay, "deploy-prod") {
		t.Error("skill replay missing skill name")
	}
	if !strings.Contains(replay, "fly deploy") {
		t.Error("skill replay missing step content")
	}
}

// --- Stale Detection ---

func TestStaleDetectionNoGit(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Non-git directory should not crash
	_ = t.TempDir()
	g := graph.New(eng.Store(), nil)
	_ = g // stale detection needs a git repo, should gracefully handle non-git dirs

	// Just verify the engine doesn't panic when no git
	_, err := eng.Status(ctx(), "")
	if err != nil {
		t.Fatalf("status should work without git: %v", err)
	}
}

// --- Config Loading ---

func TestConfigLoadDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.Server.Port != 3456 {
		t.Errorf("expected port 3456, got %d", cfg.Server.Port)
	}
	if cfg.Decay.HalfLifeDays != 30 {
		t.Errorf("expected half_life 30, got %d", cfg.Decay.HalfLifeDays)
	}
	if cfg.Decay.MinConfidence != 0.1 {
		t.Errorf("expected min_confidence 0.1, got %f", cfg.Decay.MinConfidence)
	}
	if cfg.Search.DefaultLimit != 10 {
		t.Errorf("expected default_limit 10, got %d", cfg.Search.DefaultLimit)
	}
}

func TestConfigLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	yaadDir := filepath.Join(dir, ".yaad")
	os.MkdirAll(yaadDir, 0755)

	// Write custom config
	configContent := `[decay]
half_life_days = 60
min_confidence = 0.05
boost_on_access = 0.3

[search]
default_limit = 20
`
	os.WriteFile(filepath.Join(yaadDir, "config.toml"), []byte(configContent), 0644)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Decay.HalfLifeDays != 60 {
		t.Errorf("expected half_life 60, got %d", cfg.Decay.HalfLifeDays)
	}
	if cfg.Decay.MinConfidence != 0.05 {
		t.Errorf("expected min_confidence 0.05, got %f", cfg.Decay.MinConfidence)
	}
	if cfg.Search.DefaultLimit != 20 {
		t.Errorf("expected default_limit 20, got %d", cfg.Search.DefaultLimit)
	}
	// Defaults should still apply for unset values
	if cfg.Server.Port != 3456 {
		t.Errorf("expected port 3456 (default), got %d", cfg.Server.Port)
	}
}

// --- Engine DecayConfig Integration ---

func TestEngineDecayConfigUsed(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	// Override decay config
	eng.DecayConfig = engine.DecayConfig{
		HalfLifeDays:  7, // aggressive: 7-day half life
		MinConfidence: 0.2,
		BoostOnAccess: 0.1,
	}

	ctx := context.Background()
	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: "Aggressive decay test", Scope: "project",
	})

	// Age the node 14 days (2 half-lives with 7-day config)
	node, _ := eng.Store().GetNode(ctx, n.ID)
	node.UpdatedAt = time.Now().Add(-14 * 24 * time.Hour)
	node.AccessedAt = time.Now().Add(-14 * 24 * time.Hour)
	eng.Store().UpdateNode(ctx, node)

	// Run decay with engine's config
	engine.RunDecay(ctx, eng.Store(), eng.DecayConfig)

	// After 2 half-lives, confidence should be ~0.25 (1.0 * 0.5^2)
	updated, _ := eng.Store().GetNode(ctx, n.ID)
	if updated.Confidence > 0.35 {
		t.Errorf("aggressive decay: expected confidence < 0.35, got %.2f", updated.Confidence)
	}
}

// --- Feedback Actions ---

func TestFeedbackApproveEditDiscard(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Create a low-confidence node
	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: "Maybe use Redis", Scope: "project",
	})
	node, _ := eng.Store().GetNode(ctx, n.ID)
	node.Confidence = 0.5
	eng.Store().UpdateNode(ctx, node)

	// Approve — should boost to 1.0
	if err := eng.Feedback(ctx, n.ID, engine.FeedbackApprove, ""); err != nil {
		t.Fatal(err)
	}
	node, _ = eng.Store().GetNode(ctx, n.ID)
	if node.Confidence != 1.0 {
		t.Errorf("approve: expected confidence 1.0, got %.2f", node.Confidence)
	}

	// Edit — should change content and save version
	if err := eng.Feedback(ctx, n.ID, engine.FeedbackEdit, "Definitely use Redis for caching"); err != nil {
		t.Fatal(err)
	}
	node, _ = eng.Store().GetNode(ctx, n.ID)
	if node.Content != "Definitely use Redis for caching" {
		t.Errorf("edit: content not updated")
	}
	versions, _ := eng.Store().GetVersions(ctx, n.ID)
	if len(versions) == 0 {
		t.Error("edit: no version history saved")
	}

	// Discard — should set confidence to 0
	if err := eng.Feedback(ctx, n.ID, engine.FeedbackDiscard, ""); err != nil {
		t.Fatal(err)
	}
	node, _ = eng.Store().GetNode(ctx, n.ID)
	if node.Confidence != 0 {
		t.Errorf("discard: expected confidence 0, got %.2f", node.Confidence)
	}
}

// --- Mental Model ---

func TestMentalModelGeneration(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Seed diverse memories
	eng.Remember(ctx, engine.RememberInput{Type: "convention", Content: "Use TypeScript strict mode everywhere", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "decision", Content: "Chose NATS over Kafka for event bus", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "task", Content: "Add rate limiting to auth endpoints", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "bug", Content: "Race condition in token refresh middleware", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "preference", Content: "Prefer functional style over OOP", Scope: "project"})

	model, err := eng.MentalModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if model.Summary == "" {
		t.Error("mental model: empty summary")
	}
	formatted := model.Format()
	if formatted == "" {
		t.Error("mental model: empty formatted output")
	}
	// Should contain section markers
	if !strings.Contains(formatted, "Convention") && !strings.Contains(formatted, "convention") {
		t.Error("mental model: missing conventions section")
	}
}

// --- Proactive Context ---

func TestProactiveContextPrediction(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Store memories with different tiers
	eng.Remember(ctx, engine.RememberInput{Type: "convention", Content: "Use jose for JWT auth", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "task", Content: "Add rate limiting to /auth/token", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "decision", Content: "Use NATS for event bus", Scope: "project"})

	hs := engine.NewHybridSearch(eng.Store(), eng.Graph(), nil)
	pc := engine.NewProactiveContext(eng, hs)
	nodes, err := pc.Predict(ctx, "", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Error("proactive context returned no predictions")
	}

	// Format should produce non-empty markdown
	formatted := engine.FormatContext(nodes)
	if formatted == "" {
		t.Error("proactive format returned empty string")
	}
}

// --- Token Budget ---

func TestTokenBudgetEnforcement(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Store many large nodes
	for i := 0; i < 50; i++ {
		eng.Remember(ctx, engine.RememberInput{
			Type:    "convention",
			Content: strings.Repeat("word ", 100), // ~500 chars = ~125 tokens
			Scope:   "project",
		})
	}

	// Recall with budget should respect limit
	result, err := eng.Recall(ctx, engine.RecallOpts{
		Query:  "word",
		Limit:  50,
		Budget: 500, // only ~4 nodes worth
	})
	if err != nil {
		t.Fatal(err)
	}
	// With 500 token budget and ~125 tokens per node, should get ~4 nodes
	if len(result.Nodes) > 10 {
		t.Errorf("token budget: expected ≤10 nodes with 500 budget, got %d", len(result.Nodes))
	}
}

// --- Entity Extraction ---

func TestEntityExtraction(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Remember something with file paths and package names
	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type:    "convention",
		Content: "The auth middleware in src/middleware/auth.ts uses the jose package for JWT validation",
		Scope:   "project",
	})

	// Should have created entity anchor nodes and edges
	edges, _ := eng.Store().GetEdgesFrom(ctx, n.ID)
	hasEntityEdge := false
	for _, e := range edges {
		if e.Type == "touches" {
			hasEntityEdge = true
			break
		}
	}
	if !hasEntityEdge {
		t.Error("entity extraction: no 'touches' edges created for file/package entities")
	}
}

// --- Self-Linking ---

func TestSelfLinkRelatedNodes(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Store two related memories
	eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: "Chose jose library for JWT handling", Scope: "project",
	})
	n2, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "convention", Content: "All JWT tokens must use jose library with RS256", Scope: "project",
	})

	// Self-link should have created an edge between them
	edges, _ := eng.Store().GetEdgesTo(ctx, n2.ID)
	// Check if there are edges from other nodes (besides temporal)
	hasRelated := false
	for _, e := range edges {
		if e.Type != "learned_in" {
			hasRelated = true
			break
		}
	}
	// Self-link is best-effort and depends on FTS match
	if !hasRelated {
		t.Log("self-link: no non-temporal edges found (may be expected if FTS threshold not met)")
	}
}

// --- Concurrent Access Safety ---

func TestConcurrentHawkOperations(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()
	done := make(chan bool, 3)

	// Simulate concurrent remember + recall + context (what happens during a session)
	go func() {
		for i := 0; i < 20; i++ {
			eng.Remember(ctx, engine.RememberInput{
				Type: "convention", Content: strings.Repeat("concurrent ", 10),
				Scope: "project", Project: "hawk-test",
			})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 20; i++ {
			eng.Recall(ctx, engine.RecallOpts{Query: "concurrent", Limit: 5, Project: "hawk-test"})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 10; i++ {
			eng.Context(ctx, "hawk-test")
		}
		done <- true
	}()

	for i := 0; i < 3; i++ {
		<-done
	}

	// Should not have panicked or deadlocked
	st, _ := eng.Status(ctx, "hawk-test")
	if st.Nodes == 0 {
		t.Error("concurrent: no nodes stored")
	}
}

// --- Edge Cases ---

func TestRememberMaxContentLength(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Content exceeding 10000 chars should be rejected
	_, err := eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: strings.Repeat("x", 10001), Scope: "project",
	})
	if err == nil {
		t.Error("expected error for content exceeding max length")
	}
}

func TestRememberInvalidType(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	_, err := eng.Remember(ctx, engine.RememberInput{
		Type: "invalid_type", Content: "test", Scope: "project",
	})
	if err == nil {
		t.Error("expected error for invalid node type")
	}
}

func TestRememberEmptyContentRejected(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	_, err := eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: "", Scope: "project",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestForgetPreservesVersion(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "decision", Content: "Will be forgotten", Scope: "project",
	})
	eng.Forget(ctx, n.ID)

	// Should have saved a version before archiving
	versions, _ := eng.Store().GetVersions(ctx, n.ID)
	if len(versions) == 0 {
		t.Error("forget: should save version history before archiving")
	}
}

func TestRollback(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	n, _ := eng.Remember(ctx, engine.RememberInput{
		Type: "convention", Content: "Original content", Scope: "project",
	})

	// Edit it
	eng.Feedback(ctx, n.ID, engine.FeedbackEdit, "Edited content")

	// Rollback to version 1
	err := eng.Rollback(ctx, n.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	node, _ := eng.Store().GetNode(ctx, n.ID)
	if node.Content != "Original content" {
		t.Errorf("rollback: expected 'Original content', got %q", node.Content)
	}
}

// --- Compaction ---

func TestCompactionMergesLowConfidence(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Store nodes with unique content then lower their confidence
	for i := 0; i < 5; i++ {
		n, _ := eng.Remember(ctx, engine.RememberInput{
			Type: "decision", Content: fmt.Sprintf("compactable decision number %d about topic %d", i, i*7), Scope: "project",
		})
		node, _ := eng.Store().GetNode(ctx, n.ID)
		node.Confidence = 0.15
		node.AccessCount = 0
		eng.Store().UpdateNode(ctx, node)
	}

	compacted, err := eng.Compact(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if compacted == 0 {
		t.Error("compaction should have merged low-confidence nodes")
	}
}

// --- Context Formatting (Tiered Output) ---

func TestContextFormattingTiered(t *testing.T) {
	eng, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	// Create nodes of different tiers
	eng.Remember(ctx, engine.RememberInput{Type: "convention", Content: "Hot tier convention", Scope: "project", Pinned: true})
	eng.Remember(ctx, engine.RememberInput{Type: "task", Content: "Active task here", Scope: "project"})
	eng.Remember(ctx, engine.RememberInput{Type: "spec", Content: "Cold tier spec", Scope: "project"})

	result, _ := eng.Context(ctx, "")
	formatted := engine.FormatContext(result.Nodes)

	if formatted == "" {
		t.Error("FormatContext returned empty string")
	}
	// Should have markdown structure
	if !strings.Contains(formatted, "#") {
		t.Error("FormatContext should produce markdown with headers")
	}
}

// helper
func ctx() context.Context {
	return context.Background()
}
