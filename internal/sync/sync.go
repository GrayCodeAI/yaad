// Package sync implements git-based chunk sync for team memory sharing.
//
// Layout (committed to git):
//   .yaad/
//     manifest.json          ← index of all chunks (small, git-mergeable)
//     chunks/
//       a3f8c1d2.jsonl.gz    ← chunk (gzipped JSONL, append-only)
//       b7d2e4f1.jsonl.gz
//
// Each `yaad sync` creates a NEW chunk file — old chunks are never modified.
// No merge conflicts, just append-only git history.
package sync

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Manifest tracks all known chunks.
type Manifest struct {
	Version   string        `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
	Chunks    []ChunkMeta   `json:"chunks"`
}

// ChunkMeta describes a single chunk file.
type ChunkMeta struct {
	Hash      string    `json:"hash"`
	File      string    `json:"file"`
	CreatedAt time.Time `json:"created_at"`
	NodeCount int       `json:"node_count"`
	EdgeCount int       `json:"edge_count"`
}

// ChunkRecord is a single JSONL line in a chunk file.
type ChunkRecord struct {
	Kind string          `json:"kind"` // "node" or "edge"
	Data json.RawMessage `json:"data"`
}

// Syncer manages chunk-based sync.
type Syncer struct {
	store   storage.Storage
	syncDir string // <project>/.yaad
}

// New creates a Syncer for the given project directory.
func New(store storage.Storage, projectDir string) *Syncer {
	return &Syncer{store: store, syncDir: filepath.Join(projectDir, ".yaad")}
}

// Export creates a new chunk from all current nodes/edges and updates the manifest.
// Returns the chunk hash.
func (s *Syncer) Export(ctx context.Context, project string) (string, error) {
	nodes, err := s.store.ListNodes(ctx, storage.NodeFilter{Project: project})
	if err != nil {
		return "", err
	}
	var edges []*storage.Edge
	for _, n := range nodes {
		e, _ := s.store.GetEdgesFrom(ctx, n.ID)
		edges = append(edges, e...)
	}

	if len(nodes) == 0 {
		return "", fmt.Errorf("no nodes to export")
	}

	// Build JSONL content
	var records []ChunkRecord
	for _, n := range nodes {
		b, _ := json.Marshal(n)
		records = append(records, ChunkRecord{Kind: "node", Data: b})
	}
	for _, e := range edges {
		b, _ := json.Marshal(e)
		records = append(records, ChunkRecord{Kind: "edge", Data: b})
	}

	// Compute hash of content
	content := marshalJSONL(records)
	hash := fmt.Sprintf("%x", sha256.Sum256(content))[:8]

	// Write gzipped chunk
	chunksDir := filepath.Join(s.syncDir, "chunks")
	os.MkdirAll(chunksDir, 0755)
	chunkFile := filepath.Join(chunksDir, hash+".jsonl.gz")

	f, err := os.Create(chunkFile)
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(f)
	gz.Write(content)
	gz.Close()
	f.Close()

	// Update manifest
	meta := ChunkMeta{
		Hash:      hash,
		File:      "chunks/" + hash + ".jsonl.gz",
		CreatedAt: time.Now(),
		NodeCount: len(nodes),
		EdgeCount: len(edges),
	}
	return hash, s.updateManifest(meta)
}

// Import reads all chunks from the manifest that aren't already imported.
// Returns counts of imported nodes and edges.
func (s *Syncer) Import(ctx context.Context) (int, int, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return 0, 0, nil // no manifest yet = nothing to import
	}

	// Track which chunks we've already imported (via a simple marker file)
	imported := s.loadImported()
	totalNodes, totalEdges := 0, 0

	for _, chunk := range manifest.Chunks {
		if imported[chunk.Hash] {
			continue
		}
		n, e, err := s.importChunk(ctx, chunk.File)
		if err != nil {
			continue // skip bad chunks
		}
		totalNodes += n
		totalEdges += e
		imported[chunk.Hash] = true
	}

	s.saveImported(imported)
	return totalNodes, totalEdges, nil
}

// Status returns sync status.
type Status struct {
	TotalChunks    int
	ImportedChunks int
	PendingChunks  int
}

func (s *Syncer) Status() (*Status, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return &Status{}, nil
	}
	imported := s.loadImported()
	pending := 0
	for _, c := range manifest.Chunks {
		if !imported[c.Hash] {
			pending++
		}
	}
	return &Status{
		TotalChunks:    len(manifest.Chunks),
		ImportedChunks: len(imported),
		PendingChunks:  pending,
	}, nil
}

// --- helpers ---

func (s *Syncer) importChunk(ctx context.Context, relPath string) (int, int, error) {
	f, err := os.Open(filepath.Join(s.syncDir, relPath))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, 0, err
	}
	defer gz.Close()

	nodes, edges := 0, 0
	dec := json.NewDecoder(gz)
	for dec.More() {
		var rec ChunkRecord
		if err := dec.Decode(&rec); err != nil {
			break
		}
		switch rec.Kind {
		case "node":
			var n storage.Node
			if json.Unmarshal(rec.Data, &n) == nil {
				if s.store.CreateNode(ctx, &n) == nil {
					nodes++
				}
			}
		case "edge":
			var e storage.Edge
			if json.Unmarshal(rec.Data, &e) == nil {
				if s.store.CreateEdge(ctx, &e) == nil {
					edges++
				}
			}
		}
	}
	return nodes, edges, nil
}

func (s *Syncer) loadManifest() (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(s.syncDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	return &m, json.Unmarshal(data, &m)
}

func (s *Syncer) updateManifest(meta ChunkMeta) error {
	manifest, err := s.loadManifest()
	if err != nil {
		manifest = &Manifest{Version: "1.0"}
	}
	// Avoid duplicate chunks
	for _, c := range manifest.Chunks {
		if c.Hash == meta.Hash {
			return nil
		}
	}
	manifest.Chunks = append(manifest.Chunks, meta)
	manifest.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.syncDir, "manifest.json"), b, 0644)
}

func (s *Syncer) loadImported() map[string]bool {
	data, err := os.ReadFile(filepath.Join(s.syncDir, ".imported"))
	if err != nil {
		return map[string]bool{}
	}
	var m map[string]bool
	json.Unmarshal(data, &m)
	if m == nil {
		m = map[string]bool{}
	}
	return m
}

func (s *Syncer) saveImported(m map[string]bool) {
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(s.syncDir, ".imported"), b, 0644)
}

func marshalJSONL(records []ChunkRecord) []byte {
	var out []byte
	for _, r := range records {
		b, _ := json.Marshal(r)
		out = append(out, b...)
		out = append(out, '\n')
	}
	return out
}
