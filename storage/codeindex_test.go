package storage

import (
	"context"
	"testing"
)

func TestCodeIndex_CreateUpsertQuery(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create the code index tables
	if err := s.CreateCodeIndex(ctx); err != nil {
		t.Fatalf("CreateCodeIndex: %v", err)
	}

	chunk := &CodeChunkRecord{
		ID:        "chunk-1",
		Path:      "main.go",
		StartLine: 1,
		EndLine:   10,
		Content:   "func main() { fmt.Println(\"hello\") }",
		Symbol:    "main",
		Language:  "go",
		Tokens:    15,
		FileHash:  "abc123",
	}

	// Upsert
	if err := s.UpsertCodeChunk(ctx, chunk); err != nil {
		t.Fatalf("UpsertCodeChunk: %v", err)
	}

	// Upsert again (replace)
	chunk.Content = "func main() { fmt.Println(\"updated\") }"
	chunk.Tokens = 16
	if err := s.UpsertCodeChunk(ctx, chunk); err != nil {
		t.Fatalf("UpsertCodeChunk (update): %v", err)
	}

	// List indexed paths
	paths, err := s.ListIndexedPaths(ctx)
	if err != nil {
		t.Fatalf("ListIndexedPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != "main.go" {
		t.Errorf("expected [main.go], got %v", paths)
	}
}

func TestCodeIndex_FTSSearch(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := s.CreateCodeIndex(ctx); err != nil {
		t.Fatalf("CreateCodeIndex: %v", err)
	}

	chunks := []*CodeChunkRecord{
		{
			ID:       "c1",
			Path:     "handler.go",
			Content:  "func HandleRequest(w http.ResponseWriter, r *http.Request) {}",
			Symbol:   "HandleRequest",
			Language: "go",
			Tokens:   20,
			FileHash: "hash1",
		},
		{
			ID:       "c2",
			Path:     "auth.go",
			Content:  "func Authenticate(token string) bool { return true }",
			Symbol:   "Authenticate",
			Language: "go",
			Tokens:   15,
			FileHash: "hash2",
		},
		{
			ID:       "c3",
			Path:     "utils.go",
			Content:  "func FormatDate(t time.Time) string { return t.String() }",
			Symbol:   "FormatDate",
			Language: "go",
			Tokens:   18,
			FileHash: "hash3",
		},
	}

	for _, c := range chunks {
		if err := s.UpsertCodeChunk(ctx, c); err != nil {
			t.Fatalf("UpsertCodeChunk %s: %v", c.ID, err)
		}
	}

	// Search for authentication-related code
	results, err := s.SearchCodeChunksFTS(ctx, "Authenticate token", 10)
	if err != nil {
		t.Fatalf("SearchCodeChunksFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one FTS result for 'Authenticate token'")
	}

	found := false
	for _, r := range results {
		if r.Symbol == "Authenticate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find Authenticate in FTS results")
	}
}

func TestCodeIndex_DeleteByPath(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := s.CreateCodeIndex(ctx); err != nil {
		t.Fatalf("CreateCodeIndex: %v", err)
	}

	// Insert two chunks for the same file
	for _, c := range []*CodeChunkRecord{
		{ID: "c1", Path: "delete_me.go", Content: "func A() {}", Symbol: "A", Language: "go", Tokens: 5, FileHash: "h1"},
		{ID: "c2", Path: "delete_me.go", Content: "func B() {}", Symbol: "B", Language: "go", Tokens: 5, FileHash: "h1"},
		{ID: "c3", Path: "keep_me.go", Content: "func C() {}", Symbol: "C", Language: "go", Tokens: 5, FileHash: "h2"},
	} {
		if err := s.UpsertCodeChunk(ctx, c); err != nil {
			t.Fatalf("UpsertCodeChunk %s: %v", c.ID, err)
		}
	}

	// Delete chunks for delete_me.go
	if err := s.DeleteChunksByPath(ctx, "delete_me.go"); err != nil {
		t.Fatalf("DeleteChunksByPath: %v", err)
	}

	// Verify only keep_me.go remains
	paths, err := s.ListIndexedPaths(ctx)
	if err != nil {
		t.Fatalf("ListIndexedPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != "keep_me.go" {
		t.Errorf("expected [keep_me.go] after deletion, got %v", paths)
	}
}

func TestCodeIndex_GetFileHash(t *testing.T) {
	s, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := s.CreateCodeIndex(ctx); err != nil {
		t.Fatalf("CreateCodeIndex: %v", err)
	}

	chunk := &CodeChunkRecord{
		ID:       "c1",
		Path:     "hashed.go",
		Content:  "func Hashed() {}",
		Symbol:   "Hashed",
		Language: "go",
		Tokens:   5,
		FileHash: "sha256_abc123",
	}
	if err := s.UpsertCodeChunk(ctx, chunk); err != nil {
		t.Fatalf("UpsertCodeChunk: %v", err)
	}

	// Get hash for existing file
	hash, err := s.GetFileHash(ctx, "hashed.go")
	if err != nil {
		t.Fatalf("GetFileHash: %v", err)
	}
	if hash != "sha256_abc123" {
		t.Errorf("expected sha256_abc123, got %q", hash)
	}

	// Get hash for non-existent file
	hash, err = s.GetFileHash(ctx, "nonexistent.go")
	if err != nil {
		t.Fatalf("GetFileHash (nonexistent): %v", err)
	}
	if hash != "" {
		t.Errorf("expected empty hash for nonexistent file, got %q", hash)
	}
}
