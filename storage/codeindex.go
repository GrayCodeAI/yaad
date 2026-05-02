package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CodeChunkRecord represents a stored code chunk in the index.
type CodeChunkRecord struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
	Symbol    string `json:"symbol"`
	Language  string `json:"language"`
	Tokens    int    `json:"tokens"`
	FileHash  string `json:"file_hash"`
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
  file_hash TEXT
);
CREATE INDEX IF NOT EXISTS idx_code_chunks_path ON code_chunks(path);
CREATE INDEX IF NOT EXISTS idx_code_chunks_hash ON code_chunks(file_hash);
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
		`INSERT OR REPLACE INTO code_chunks (id, path, start_line, end_line, content, symbol, language, tokens, file_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chunk.ID, chunk.Path, chunk.StartLine, chunk.EndLine, chunk.Content,
		chunk.Symbol, chunk.Language, chunk.Tokens, chunk.FileHash)
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
		`SELECT c.id, c.path, c.start_line, c.end_line, c.content, c.symbol, c.language, c.tokens, c.file_hash
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
		if err := rows.Scan(&c.ID, &c.Path, &c.StartLine, &c.EndLine,
			&c.Content, &c.Symbol, &c.Language, &c.Tokens, &c.FileHash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
