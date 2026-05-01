package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

func setupTestServer(t *testing.T) (*RESTServer, *engine.Engine, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(store, graph.New(store, store.DB()))
	srv := NewRESTServer(eng, "")
	return srv, eng, func() {
		eng.Close()
		store.Close()
	}
}

func TestHandleRememberValidation(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Valid node type
	body, _ := json.Marshal(engine.RememberInput{
		Type:    "convention",
		Content: "Use TypeScript strict mode",
		Scope:   "project",
	})
	req := httptest.NewRequest("POST", "/yaad/remember", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Errorf("valid type: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Invalid node type
	body, _ = json.Marshal(engine.RememberInput{
		Type:    "invalid_type_xyz",
		Content: "should fail",
		Scope:   "project",
	})
	req = httptest.NewRequest("POST", "/yaad/remember", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("invalid type: expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid node type") {
		t.Errorf("expected 'invalid node type' in error, got: %s", rr.Body.String())
	}
}

func TestHandleRecallDepthLimit(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(engine.RecallOpts{Query: "test", Depth: 10})
	req := httptest.NewRequest("POST", "/yaad/recall", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("depth=10: expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "depth exceeds maximum") {
		t.Errorf("expected depth limit error, got: %s", rr.Body.String())
	}

	// Valid depth
	body, _ = json.Marshal(engine.RecallOpts{Query: "test", Depth: 3})
	req = httptest.NewRequest("POST", "/yaad/recall", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("depth=3: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleLinkEdgeTypeValidation(t *testing.T) {
	srv, eng, cleanup := setupTestServer(t)
	defer cleanup()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	// Seed two nodes
	a, _ := eng.Remember(ctx, engine.RememberInput{Type: "decision", Content: "A", Scope: "project"})
	b, _ := eng.Remember(ctx, engine.RememberInput{Type: "convention", Content: "B", Scope: "project"})

	// Missing type
	body, _ := json.Marshal(storage.Edge{FromID: a.ID, ToID: b.ID})
	req := httptest.NewRequest("POST", "/yaad/link", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("missing type: expected 400, got %d", rr.Code)
	}

	// Invalid type
	body, _ = json.Marshal(storage.Edge{FromID: a.ID, ToID: b.ID, Type: "invalid_edge"})
	req = httptest.NewRequest("POST", "/yaad/link", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("invalid type: expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid edge type") {
		t.Errorf("expected 'invalid edge type' in error, got: %s", rr.Body.String())
	}

	// Valid type
	body, _ = json.Marshal(storage.Edge{FromID: a.ID, ToID: b.ID, Type: "led_to"})
	req = httptest.NewRequest("POST", "/yaad/link", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Errorf("valid type: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSubgraphDepthLimit(t *testing.T) {
	srv, eng, cleanup := setupTestServer(t)
	defer cleanup()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()
	a, _ := eng.Remember(ctx, engine.RememberInput{Type: "decision", Content: "A", Scope: "project"})

	req := httptest.NewRequest("GET", "/yaad/subgraph/"+a.ID+"?depth=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("depth=10: expected 400, got %d", rr.Code)
	}

	req = httptest.NewRequest("GET", "/yaad/subgraph/"+a.ID+"?depth=2", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("depth=2: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleHealth(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/yaad/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("health: expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("health response decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("health status: expected 'ok', got %q", resp["status"])
	}
	if resp["version"] == "" {
		t.Error("health version: expected non-empty")
	}
}

func TestRequestBodyLimit(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	wrapped := srv.withMiddleware(mux)

	// Create a 2MB JSON payload
	bigContent := strings.Repeat("x", 2<<20)
	body, _ := json.Marshal(map[string]string{"content": bigContent})
	req := httptest.NewRequest("POST", "/yaad/remember", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != 413 {
		t.Errorf("oversized body: expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
}


func TestShutdown(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Shutdown on nil server should not panic
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown nil server: %v", err)
	}
}
