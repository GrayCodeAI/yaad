package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
)

// CodeChunkRecord represents a stored code chunk in the index.
type CodeChunkRecord struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	Content       string `json:"content"`
	Symbol        string `json:"symbol"`
	Language      string `json:"language"`
	Tokens        int    `json:"tokens"`
	FileHash      string `json:"file_hash"`
	SchemaVersion string `json:"schema_version"`
	Vector        []byte `json:"vector,omitempty"` // embedding stored as BLOB
}

// CreateCodeIndex creates the code_chunks table and its FTS5 virtual table.
// Safe to call multiple times (uses IF NOT EXISTS).
func (s *Store) CreateCodeIndex(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, codeIndexSchema)
	return err
}

const codeIndexSchema = `
CREATE TABLE IF NOT EXISTS code_chunks (
  id TEXT PRIMARY KEY,
  path TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  end_line INTEGER NOT NULL,
  content TEXT NOT NULL,
  symbol TEXT,
  language TEXT,
  tokens INTEGER,
  file_hash TEXT,
  schema_version TEXT DEFAULT '',
  vector BLOB
);
CREATE INDEX IF NOT EXISTS idx_code_chunks_path ON code_chunks(path);
CREATE INDEX IF NOT EXISTS idx_code_chunks_hash ON code_chunks(file_hash);
CREATE INDEX IF NOT EXISTS idx_code_chunks_schema ON code_chunks(schema_version);
`

// ensureCodeChunksFTS creates the FTS5 virtual table if it does not exist.
// FTS5 virtual tables do not support IF NOT EXISTS in all SQLite builds,
// so we check for existence first.
func (s *Store) ensureCodeChunksFTS(ctx context.Context) error {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='code_chunks_fts'`).Scan(&name)
	if err == nil {
		return nil // already exists
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`CREATE VIRTUAL TABLE code_chunks_fts USING fts5(content, symbol, path)`)
	return err
}

// UpsertCodeChunk inserts or replaces a code chunk record and updates the FTS index.
func (s *Store) UpsertCodeChunk(ctx context.Context, chunk *CodeChunkRecord) error {
	if err := s.ensureCodeChunksFTS(ctx); err != nil {
		return fmt.Errorf("ensure FTS: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete old FTS entry if the chunk already exists
	var oldContent, oldSymbol, oldPath string
	err = tx.QueryRowContext(ctx,
		`SELECT content, symbol, path FROM code_chunks WHERE id = ?`, chunk.ID).
		Scan(&oldContent, &oldSymbol, &oldPath)
	if err == nil {
		// Old row exists, remove its FTS entry
		_, _ = tx.ExecContext(ctx,
			`DELETE FROM code_chunks_fts WHERE rowid = (SELECT rowid FROM code_chunks WHERE id = ?)`,
			chunk.ID)
	}

	// Upsert the main record
	_, err = tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO code_chunks (id, path, start_line, end_line, content, symbol, language, tokens, file_hash, schema_version, vector)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chunk.ID, chunk.Path, chunk.StartLine, chunk.EndLine, chunk.Content,
		chunk.Symbol, chunk.Language, chunk.Tokens, chunk.FileHash, chunk.SchemaVersion, chunk.Vector)
	if err != nil {
		return err
	}

	// Insert into FTS
	_, err = tx.ExecContext(ctx,
		`INSERT INTO code_chunks_fts (rowid, content, symbol, path)
		 VALUES ((SELECT rowid FROM code_chunks WHERE id = ?), ?, ?, ?)`,
		chunk.ID, chunk.Content, chunk.Symbol, chunk.Path)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteChunksByPath removes all code chunks for a given file path.
func (s *Store) DeleteChunksByPath(ctx context.Context, path string) error {
	if err := s.ensureCodeChunksFTS(ctx); err != nil {
		return fmt.Errorf("ensure FTS: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete FTS entries for all chunks of this path
	_, err = tx.ExecContext(ctx,
		`DELETE FROM code_chunks_fts WHERE rowid IN (SELECT rowid FROM code_chunks WHERE path = ?)`,
		path)
	if err != nil {
		return err
	}

	// Delete the main records
	_, err = tx.ExecContext(ctx,
		`DELETE FROM code_chunks WHERE path = ?`, path)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// SearchCodeChunksFTS performs a full-text search on code chunks.
func (s *Store) SearchCodeChunksFTS(ctx context.Context, query string, limit int) ([]*CodeChunkRecord, error) {
	if err := s.ensureCodeChunksFTS(ctx); err != nil {
		return nil, fmt.Errorf("ensure FTS: %w", err)
	}
	if limit <= 0 {
		limit = 10
	}

	ftsQuery := escapeCodeFTS5(query)
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.path, c.start_line, c.end_line, c.content, c.symbol, c.language, c.tokens, c.file_hash, c.schema_version, c.vector
		 FROM code_chunks_fts f
		 JOIN code_chunks c ON f.rowid = c.rowid
		 WHERE code_chunks_fts MATCH ?
		 ORDER BY rank LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanCodeChunks(rows)
}

// GetFileHash returns the file hash for the first chunk of the given path.
func (s *Store) GetFileHash(ctx context.Context, path string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT file_hash FROM code_chunks WHERE path = ? LIMIT 1`, path).Scan(&hash)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return hash, nil
}

// ListIndexedPaths returns all distinct file paths that have indexed chunks.
func (s *Store) ListIndexedPaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT path FROM code_chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

func escapeCodeFTS5(query string) string {
	words := strings.Fields(query)
	for i, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		words[i] = `"` + w + `"`
	}
	return strings.Join(words, " OR ")
}

func scanCodeChunks(rows *sql.Rows) ([]*CodeChunkRecord, error) {
	var out []*CodeChunkRecord
	for rows.Next() {
		c := &CodeChunkRecord{}
		var schemaVersion sql.NullString
		var vector []byte
		if err := rows.Scan(&c.ID, &c.Path, &c.StartLine, &c.EndLine,
			&c.Content, &c.Symbol, &c.Language, &c.Tokens, &c.FileHash,
			&schemaVersion, &vector); err != nil {
			return nil, err
		}
		if schemaVersion.Valid {
			c.SchemaVersion = schemaVersion.String
		}
		c.Vector = vector
		out = append(out, c)
	}
	return out, rows.Err()
}

// ComputeSchemaVersion produces a version string from the chunker version and
// embedding model name. It is the first 16 hex chars of a SHA-256 hash.
func ComputeSchemaVersion(chunkerVersion, modelName string) string {
	h := sha256.Sum256([]byte(chunkerVersion + ":" + modelName))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}

// InvalidateStaleChunks deletes all code chunks whose schema_version does not
// match currentVersion, returning the count of deleted rows.
func (s *Store) InvalidateStaleChunks(ctx context.Context, currentVersion string) (int, error) {
	if err := s.ensureCodeChunksFTS(ctx); err != nil {
		return 0, fmt.Errorf("ensure FTS: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Delete FTS entries for stale chunks
	_, err = tx.ExecContext(ctx,
		`DELETE FROM code_chunks_fts WHERE rowid IN (SELECT rowid FROM code_chunks WHERE schema_version != ?)`,
		currentVersion)
	if err != nil {
		return 0, err
	}

	// Delete stale chunks
	result, err := tx.ExecContext(ctx,
		`DELETE FROM code_chunks WHERE schema_version != ?`, currentVersion)
	if err != nil {
		return 0, err
	}

	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(n), tx.Commit()
}

// StoreVector saves an embedding vector for a code chunk.
func (s *Store) StoreVector(ctx context.Context, chunkID string, vec []float32) error {
	blob := EncodeVector(vec)
	_, err := s.db.ExecContext(ctx,
		`UPDATE code_chunks SET vector = ? WHERE id = ?`, blob, chunkID)
	return err
}

// SearchCodeChunksByLanguage performs a full-text search filtered by language.
// If languages is empty the search covers all languages (same as SearchCodeChunksFTS).
func (s *Store) SearchCodeChunksByLanguage(ctx context.Context, query string, languages []string, limit int) ([]*CodeChunkRecord, error) {
	if err := s.ensureCodeChunksFTS(ctx); err != nil {
		return nil, fmt.Errorf("ensure FTS: %w", err)
	}
	if limit <= 0 {
		limit = 10
	}

	ftsQuery := escapeCodeFTS5(query)

	if len(languages) == 0 {
		return s.SearchCodeChunksFTS(ctx, query, limit)
	}

	// Build WHERE language IN (?, ?, ...) clause
	placeholders := make([]string, len(languages))
	args := make([]any, 0, len(languages)+2)
	args = append(args, ftsQuery)
	for i, lang := range languages {
		placeholders[i] = "?"
		args = append(args, lang)
	}
	args = append(args, limit)

	q := `SELECT c.id, c.path, c.start_line, c.end_line, c.content, c.symbol, c.language, c.tokens, c.file_hash, c.schema_version, c.vector
		 FROM code_chunks_fts f
		 JOIN code_chunks c ON f.rowid = c.rowid
		 WHERE code_chunks_fts MATCH ?
		   AND c.language IN (` + strings.Join(placeholders, ",") + `)
		 ORDER BY rank LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanCodeChunks(rows)
}

// SearchCodeChunksHybrid performs a hybrid search combining FTS5 ranking with
// cosine similarity on stored vectors. If languages is non-empty, only chunks
// in those languages are considered.
func (s *Store) SearchCodeChunksHybrid(ctx context.Context, query string, queryVec []float32, limit int, languages []string) ([]*CodeChunkRecord, error) {
	if err := s.ensureCodeChunksFTS(ctx); err != nil {
		return nil, fmt.Errorf("ensure FTS: %w", err)
	}
	if limit <= 0 {
		limit = 10
	}

	// Step 1: FTS5 search to get top 50 candidates
	ftsQuery := escapeCodeFTS5(query)

	var rows *sql.Rows
	var err error
	if len(languages) > 0 {
		placeholders := make([]string, len(languages))
		args := make([]any, 0, len(languages)+2)
		args = append(args, ftsQuery)
		for i, lang := range languages {
			placeholders[i] = "?"
			args = append(args, lang)
		}
		args = append(args, 50)
		q := `SELECT c.id, c.path, c.start_line, c.end_line, c.content, c.symbol, c.language, c.tokens, c.file_hash, c.schema_version, c.vector,
			        rank
			 FROM code_chunks_fts f
			 JOIN code_chunks c ON f.rowid = c.rowid
			 WHERE code_chunks_fts MATCH ?
			   AND c.language IN (` + strings.Join(placeholders, ",") + `)
			 ORDER BY rank LIMIT ?`
		rows, err = s.db.QueryContext(ctx, q, args...)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT c.id, c.path, c.start_line, c.end_line, c.content, c.symbol, c.language, c.tokens, c.file_hash, c.schema_version, c.vector,
			        rank
			 FROM code_chunks_fts f
			 JOIN code_chunks c ON f.rowid = c.rowid
			 WHERE code_chunks_fts MATCH ?
			 ORDER BY rank LIMIT 50`, ftsQuery)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scoredChunk struct {
		chunk    *CodeChunkRecord
		ftsRank  float64
		cosineSim float64
	}

	var candidates []scoredChunk
	for rows.Next() {
		c := &CodeChunkRecord{}
		var schemaVersion sql.NullString
		var vector []byte
		var ftsRank float64
		if err := rows.Scan(&c.ID, &c.Path, &c.StartLine, &c.EndLine,
			&c.Content, &c.Symbol, &c.Language, &c.Tokens, &c.FileHash,
			&schemaVersion, &vector, &ftsRank); err != nil {
			return nil, err
		}
		if schemaVersion.Valid {
			c.SchemaVersion = schemaVersion.String
		}
		c.Vector = vector
		candidates = append(candidates, scoredChunk{chunk: c, ftsRank: ftsRank})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Step 2: compute cosine similarity for candidates with vectors
	for i := range candidates {
		if len(candidates[i].chunk.Vector) > 0 && len(queryVec) > 0 {
			docVec := DecodeVector(candidates[i].chunk.Vector)
			candidates[i].cosineSim = float64(cosineSimFloat32(queryVec, docVec))
		}
	}

	// Step 3: combine scores using Reciprocal Rank Fusion (RRF), k=60
	// First, rank by FTS (ftsRank from SQLite is negative; more negative = better match)
	sort.Slice(candidates, func(a, b int) bool {
		return candidates[a].ftsRank < candidates[b].ftsRank
	})
	for i := range candidates {
		candidates[i].ftsRank = 1.0 / float64(60+i+1)
	}

	// Rank by cosine similarity (higher is better)
	sort.Slice(candidates, func(a, b int) bool {
		return candidates[a].cosineSim > candidates[b].cosineSim
	})
	rrfScores := make([]float64, len(candidates))
	for i := range candidates {
		cosineRRF := 1.0 / float64(60+i+1)
		rrfScores[i] = candidates[i].ftsRank + cosineRRF
	}

	// Step 4: sort by combined RRF score and return top limit
	type indexedScore struct {
		idx   int
		score float64
	}
	ranked := make([]indexedScore, len(candidates))
	for i := range candidates {
		ranked[i] = indexedScore{idx: i, score: rrfScores[i]}
	}
	sort.Slice(ranked, func(a, b int) bool {
		return ranked[a].score > ranked[b].score
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	out := make([]*CodeChunkRecord, len(ranked))
	for i, r := range ranked {
		out[i] = candidates[r.idx].chunk
	}
	return out, nil
}

// cosineSimFloat32 computes cosine similarity between two float32 vectors.
func cosineSimFloat32(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}
