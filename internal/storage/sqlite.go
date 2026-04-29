package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Types

// Node represents a memory node in the Yaad graph.
type Node struct {
	ID, Type, Content, ContentHash, Summary, Scope, Project, Tags string
	Tier                                                          int
	Confidence                                                    float64
	AccessCount                                                   int
	CreatedAt, UpdatedAt, AccessedAt                              time.Time
	SourceSession, SourceAgent                                    string
	Version                                                       int
}

// Edge represents a relationship between two nodes in the graph.
type Edge struct {
	ID, FromID, ToID, Type, Metadata string
	Acyclic                          bool
	Weight                           float64
	CreatedAt                        time.Time
}

// Session tracks a coding agent session.
type Session struct {
	ID, Project, Summary, Agent string
	StartedAt, EndedAt          time.Time
}

// NodeVersion stores a historical version of a node for audit/rollback.
type NodeVersion struct {
	NodeID, Content, ChangedBy, Reason string
	Version                            int
	ChangedAt                          time.Time
}

// NodeFilter specifies criteria for listing nodes.
type NodeFilter struct {
	Type, Scope, Project string
	Tier                 int
	MinConfidence        float64
}

// Store

// Store is the SQLite-backed storage layer for Yaad.
const defaultBusyTimeoutMs = 5000

type Store struct {
	db *sql.DB
}

// DB returns the underlying database connection for direct queries.
func (s *Store) DB() *sql.DB { return s.db }

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	// Set busy timeout so concurrent reads wait instead of failing immediately
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", defaultBusyTimeoutMs)); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.createTables(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SearchNodeByHash finds a node by content hash + scope + project (dedup check).
func (s *Store) SearchNodeByHash(ctx context.Context, hash, scope, project string) (*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id,type,content,content_hash,summary,scope,project,tier,tags,confidence,
		access_count,created_at,updated_at,accessed_at,source_session,source_agent,version
		FROM nodes WHERE content_hash=? AND scope=? AND (project=? OR project IS NULL) LIMIT 1`,
		hash, scope, project)
	n := &Node{}
	var at sql.NullTime
	err := row.Scan(&n.ID, &n.Type, &n.Content, &n.ContentHash, &n.Summary, &n.Scope, &n.Project,
		&n.Tier, &n.Tags, &n.Confidence, &n.AccessCount, &n.CreatedAt, &n.UpdatedAt, &at,
		&n.SourceSession, &n.SourceAgent, &n.Version)
	if err != nil {
		return nil, err
	}
	if at.Valid {
		n.AccessedAt = at.Time
	}
	return n, nil
}

func (s *Store) createTables() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  summary TEXT,
  scope TEXT NOT NULL,
  project TEXT,
  tier INTEGER DEFAULT 2,
  tags TEXT,
  confidence REAL DEFAULT 1.0,
  access_count INTEGER DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  accessed_at DATETIME,
  source_session TEXT,
  source_agent TEXT,
  version INTEGER DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_hash ON nodes(content_hash, scope, project);

CREATE TABLE IF NOT EXISTS edges (
  id TEXT PRIMARY KEY,
  from_id TEXT NOT NULL,
  to_id TEXT NOT NULL,
  type TEXT NOT NULL,
  acyclic BOOLEAN NOT NULL,
  weight REAL DEFAULT 1.0,
  metadata TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (from_id) REFERENCES nodes(id),
  FOREIGN KEY (to_id) REFERENCES nodes(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique ON edges(from_id, to_id, type);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);

CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(content, summary, tags, content=nodes, content_rowid=rowid);

CREATE TABLE IF NOT EXISTS embeddings (
  node_id TEXT PRIMARY KEY,
  vector BLOB,
  model TEXT,
  FOREIGN KEY (node_id) REFERENCES nodes(id)
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  project TEXT,
  started_at DATETIME,
  ended_at DATETIME,
  summary TEXT,
  agent TEXT
);

CREATE TABLE IF NOT EXISTS file_watch (
  file_path TEXT,
  node_id TEXT,
  git_hash TEXT,
  FOREIGN KEY (node_id) REFERENCES nodes(id)
);
CREATE INDEX IF NOT EXISTS idx_file_watch_path ON file_watch(file_path);

CREATE TABLE IF NOT EXISTS node_versions (
  node_id TEXT,
  version INTEGER,
  content TEXT,
  changed_at DATETIME,
  changed_by TEXT,
  reason TEXT,
  PRIMARY KEY (node_id, version)
);

CREATE TABLE IF NOT EXISTS replay_events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  data       TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_replay_session ON replay_events(session_id);

-- FTS5 triggers
CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
  INSERT INTO nodes_fts(rowid, content, summary, tags) VALUES (new.rowid, new.content, new.summary, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
  INSERT INTO nodes_fts(nodes_fts, rowid, content, summary, tags) VALUES ('delete', old.rowid, old.content, old.summary, old.tags);
  INSERT INTO nodes_fts(rowid, content, summary, tags) VALUES (new.rowid, new.content, new.summary, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
  INSERT INTO nodes_fts(nodes_fts, rowid, content, summary, tags) VALUES ('delete', old.rowid, old.content, old.summary, old.tags);
END;
`

// --- Nodes ---

func (s *Store) CreateNode(ctx context.Context, n *Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO nodes (id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Type, n.Content, n.ContentHash, n.Summary, n.Scope, n.Project, n.Tier, n.Tags, n.Confidence, n.AccessCount,
		n.CreatedAt, n.UpdatedAt, nullTime(n.AccessedAt), n.SourceSession, n.SourceAgent, n.Version)
	return err
}

func (s *Store) GetNode(ctx context.Context, id string) (*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := &Node{}
	var accessedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version FROM nodes WHERE id = ?`, id).
		Scan(&n.ID, &n.Type, &n.Content, &n.ContentHash, &n.Summary, &n.Scope, &n.Project, &n.Tier, &n.Tags, &n.Confidence, &n.AccessCount, &n.CreatedAt, &n.UpdatedAt, &accessedAt, &n.SourceSession, &n.SourceAgent, &n.Version)
	if err != nil {
		return nil, err
	}
	if accessedAt.Valid {
		n.AccessedAt = accessedAt.Time
	}
	return n, nil
}

func (s *Store) UpdateNode(ctx context.Context, n *Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET type=?, content=?, content_hash=?, summary=?, scope=?, project=?, tier=?, tags=?, confidence=?, access_count=?, updated_at=?, accessed_at=?, source_session=?, source_agent=?, version=? WHERE id=?`,
		n.Type, n.Content, n.ContentHash, n.Summary, n.Scope, n.Project, n.Tier, n.Tags, n.Confidence, n.AccessCount,
		n.UpdatedAt, nullTime(n.AccessedAt), n.SourceSession, n.SourceAgent, n.Version, n.ID)
	return err
}

func (s *Store) DeleteNode(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM edges WHERE from_id=? OR to_id=?`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListNodes(ctx context.Context, f NodeFilter) ([]*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := "SELECT id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version FROM nodes WHERE 1=1"
	var args []any
	if f.Type != "" {
		q += " AND type=?"
		args = append(args, f.Type)
	}
	if f.Scope != "" {
		q += " AND scope=?"
		args = append(args, f.Scope)
	}
	if f.Project != "" {
		q += " AND project=?"
		args = append(args, f.Project)
	}
	if f.Tier > 0 {
		q += " AND tier=?"
		args = append(args, f.Tier)
	}
	if f.MinConfidence > 0 {
		q += " AND confidence>=?"
		args = append(args, f.MinConfidence)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *Store) SearchNodes(ctx context.Context, query string, limit int) ([]*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	// Convert multi-word query to FTS5 OR query: "auth JWT" → "auth OR JWT"
	words := strings.Fields(query)
	ftsQuery := strings.Join(words, " OR ")
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.type, n.content, n.content_hash, n.summary, n.scope, n.project, n.tier, n.tags, n.confidence, n.access_count, n.created_at, n.updated_at, n.accessed_at, n.source_session, n.source_agent, n.version
		FROM nodes_fts f JOIN nodes n ON f.rowid = n.rowid WHERE nodes_fts MATCH ? ORDER BY rank LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// --- Edges ---

func (s *Store) CreateEdge(ctx context.Context, e *Edge) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO edges (id, from_id, to_id, type, acyclic, weight, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.FromID, e.ToID, e.Type, e.Acyclic, e.Weight, e.Metadata, e.CreatedAt)
	return err
}

func (s *Store) GetEdge(ctx context.Context, id string) (*Edge, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := &Edge{}
	err := s.db.QueryRowContext(ctx, `SELECT id, from_id, to_id, type, acyclic, weight, metadata, created_at FROM edges WHERE id=?`, id).
		Scan(&e.ID, &e.FromID, &e.ToID, &e.Type, &e.Acyclic, &e.Weight, &e.Metadata, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Store) DeleteEdge(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM edges WHERE id=?`, id)
	return err
}

func (s *Store) GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.queryEdges(ctx, `SELECT id, from_id, to_id, type, acyclic, weight, metadata, created_at FROM edges WHERE from_id=?`, nodeID)
}

func (s *Store) GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.queryEdges(ctx, `SELECT id, from_id, to_id, type, acyclic, weight, metadata, created_at FROM edges WHERE to_id=?`, nodeID)
}

func (s *Store) GetNeighbors(ctx context.Context, nodeID string) ([]*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT n.id, n.type, n.content, n.content_hash, n.summary, n.scope, n.project, n.tier, n.tags, n.confidence, n.access_count, n.created_at, n.updated_at, n.accessed_at, n.source_session, n.source_agent, n.version
		FROM nodes n JOIN edges e ON (e.to_id = n.id AND e.from_id = ?) OR (e.from_id = n.id AND e.to_id = ?)`, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, project, started_at, ended_at, summary, agent) VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Project, sess.StartedAt, nullTime(sess.EndedAt), sess.Summary, sess.Agent)
	return err
}

func (s *Store) EndSession(ctx context.Context, id string, summary string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET ended_at=?, summary=? WHERE id=?`, time.Now(), summary, id)
	return err
}

func (s *Store) ListSessions(ctx context.Context, project string, limit int) ([]*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	q := "SELECT id, project, started_at, ended_at, summary, agent FROM sessions"
	var args []any
	if project != "" {
		q += " WHERE project=?"
		args = append(args, project)
	}
	q += " ORDER BY started_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		sess := &Session{}
		var endedAt sql.NullTime
		if err := rows.Scan(&sess.ID, &sess.Project, &sess.StartedAt, &endedAt, &sess.Summary, &sess.Agent); err != nil {
			return nil, err
		}
		if endedAt.Valid {
			sess.EndedAt = endedAt.Time
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// --- File Watch ---

func (s *Store) AddFileWatch(ctx context.Context, filePath, nodeID, gitHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO file_watch (file_path, node_id, git_hash) VALUES (?, ?, ?)`, filePath, nodeID, gitHash)
	return err
}

func (s *Store) GetNodesByFile(ctx context.Context, filePath string) ([]*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.type, n.content, n.content_hash, n.summary, n.scope, n.project, n.tier, n.tags, n.confidence, n.access_count, n.created_at, n.updated_at, n.accessed_at, n.source_session, n.source_agent, n.version
		FROM nodes n JOIN file_watch fw ON fw.node_id = n.id WHERE fw.file_path = ?`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// --- Versions ---

func (s *Store) SaveVersion(ctx context.Context, nodeID string, content, changedBy, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var maxVer int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM node_versions WHERE node_id=?`, nodeID).Scan(&maxVer)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO node_versions (node_id, version, content, changed_at, changed_by, reason) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID, maxVer+1, content, time.Now(), changedBy, reason)
	return err
}

func (s *Store) GetVersions(ctx context.Context, nodeID string) ([]*NodeVersion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, version, content, changed_at, changed_by, reason FROM node_versions WHERE node_id=? ORDER BY version`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*NodeVersion
	for rows.Next() {
		v := &NodeVersion{}
		if err := rows.Scan(&v.NodeID, &v.Version, &v.Content, &v.ChangedAt, &v.ChangedBy, &v.Reason); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- Helpers ---

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func scanNodes(rows *sql.Rows) ([]*Node, error) {
	var out []*Node
	for rows.Next() {
		n := &Node{}
		var accessedAt sql.NullTime
		if err := rows.Scan(&n.ID, &n.Type, &n.Content, &n.ContentHash, &n.Summary, &n.Scope, &n.Project, &n.Tier, &n.Tags, &n.Confidence, &n.AccessCount, &n.CreatedAt, &n.UpdatedAt, &accessedAt, &n.SourceSession, &n.SourceAgent, &n.Version); err != nil {
			return nil, err
		}
		if accessedAt.Valid {
			n.AccessedAt = accessedAt.Time
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) queryEdges(ctx context.Context, query string, args ...any) ([]*Edge, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Edge
	for rows.Next() {
		e := &Edge{}
		if err := rows.Scan(&e.ID, &e.FromID, &e.ToID, &e.Type, &e.Acyclic, &e.Weight, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetNodesBatch fetches multiple nodes by ID in a single query.
func (s *Store) GetNodesBatch(ctx context.Context, ids []string) ([]*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(`SELECT id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version FROM nodes WHERE id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// CountEdges returns inbound and outbound edge counts for a node.
func (s *Store) CountEdges(ctx context.Context, nodeID string) (inbound int, outbound int, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM edges WHERE to_id = ?), (SELECT COUNT(*) FROM edges WHERE from_id = ?)`,
		nodeID, nodeID).Scan(&inbound, &outbound)
	return inbound, outbound, err
}

// CountAllEdges returns the total number of edges in the graph.
func (s *Store) CountAllEdges(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&count)
	return count, err
}

// CheckCycle uses a recursive CTE to detect if adding from→to would create a cycle
// among acyclic edges.
func (s *Store) CheckCycle(ctx context.Context, fromID, toID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	query := `
		WITH RECURSIVE ancestors(id) AS (
			SELECT ?
			UNION ALL
			SELECT e.from_id FROM ancestors a
			JOIN edges e ON e.to_id = a.id AND e.acyclic = 1
		)
		SELECT 1 FROM ancestors WHERE id = ? LIMIT 1`
	var exists int
	err := s.db.QueryRowContext(ctx, query, fromID, toID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

// WithTx runs the given function inside a SQL transaction.
// If the function returns an error, the transaction is rolled back.
func (s *Store) WithTx(ctx context.Context, fn func(Storage) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	txStore := &txStore{tx: tx}
	if err := fn(txStore); err != nil {
		return err
	}
	return tx.Commit()
}

// txStore is a Storage implementation backed by a SQL transaction.
type txStore struct {
	tx *sql.Tx
}

func (t *txStore) queryNodes(ctx context.Context, query string, args ...any) ([]*Node, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (t *txStore) CreateNode(ctx context.Context, n *Node) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO nodes (id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Type, n.Content, n.ContentHash, n.Summary, n.Scope, n.Project, n.Tier, n.Tags, n.Confidence, n.AccessCount,
		n.CreatedAt, n.UpdatedAt, nullTime(n.AccessedAt), n.SourceSession, n.SourceAgent, n.Version)
	return err
}

func (t *txStore) GetNode(ctx context.Context, id string) (*Node, error) {
	n := &Node{}
	var accessedAt sql.NullTime
	err := t.tx.QueryRowContext(ctx, `SELECT id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version FROM nodes WHERE id = ?`, id).
		Scan(&n.ID, &n.Type, &n.Content, &n.ContentHash, &n.Summary, &n.Scope, &n.Project, &n.Tier, &n.Tags, &n.Confidence, &n.AccessCount, &n.CreatedAt, &n.UpdatedAt, &accessedAt, &n.SourceSession, &n.SourceAgent, &n.Version)
	if err != nil {
		return nil, err
	}
	if accessedAt.Valid {
		n.AccessedAt = accessedAt.Time
	}
	return n, nil
}

func (t *txStore) GetNodesBatch(ctx context.Context, ids []string) ([]*Node, error) { return nil, nil }
func (t *txStore) UpdateNode(ctx context.Context, n *Node) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE nodes SET type=?, content=?, content_hash=?, summary=?, scope=?, project=?, tier=?, tags=?, confidence=?, access_count=?, updated_at=?, accessed_at=?, source_session=?, source_agent=?, version=? WHERE id=?`,
		n.Type, n.Content, n.ContentHash, n.Summary, n.Scope, n.Project, n.Tier, n.Tags, n.Confidence, n.AccessCount,
		n.UpdatedAt, nullTime(n.AccessedAt), n.SourceSession, n.SourceAgent, n.Version, n.ID)
	return err
}
func (t *txStore) DeleteNode(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM edges WHERE from_id=? OR to_id=?`, id, id)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, id)
	return err
}
func (t *txStore) ListNodes(ctx context.Context, f NodeFilter) ([]*Node, error) { return nil, nil }
func (t *txStore) SearchNodes(ctx context.Context, query string, limit int) ([]*Node, error) { return nil, nil }
func (t *txStore) SearchNodeByHash(ctx context.Context, hash, scope, project string) (*Node, error) { return nil, nil }
func (t *txStore) GetNeighbors(ctx context.Context, nodeID string) ([]*Node, error) { return nil, nil }
func (t *txStore) CreateEdge(ctx context.Context, e *Edge) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO edges (id, from_id, to_id, type, acyclic, weight, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.FromID, e.ToID, e.Type, e.Acyclic, e.Weight, e.Metadata, e.CreatedAt)
	return err
}
func (t *txStore) GetEdge(ctx context.Context, id string) (*Edge, error) { return nil, nil }
func (t *txStore) DeleteEdge(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM edges WHERE id=?`, id)
	return err
}
func (t *txStore) GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error) { return nil, nil }
func (t *txStore) GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error) { return nil, nil }
func (t *txStore) CountEdges(ctx context.Context, nodeID string) (int, int, error) { return 0, 0, nil }
func (t *txStore) CountAllEdges(ctx context.Context) (int, error) { return 0, nil }
func (t *txStore) CheckCycle(ctx context.Context, fromID, toID string) (bool, error) { return false, nil }
func (t *txStore) CreateSession(ctx context.Context, sess *Session) error { return nil }
func (t *txStore) EndSession(ctx context.Context, id string, summary string) error { return nil }
func (t *txStore) ListSessions(ctx context.Context, project string, limit int) ([]*Session, error) { return nil, nil }
func (t *txStore) SaveVersion(ctx context.Context, nodeID string, content, changedBy, reason string) error { return nil }
func (t *txStore) GetVersions(ctx context.Context, nodeID string) ([]*NodeVersion, error) { return nil, nil }
func (t *txStore) SaveEmbedding(ctx context.Context, nodeID, model string, vector []float32) error { return nil }
func (t *txStore) DeleteEmbedding(ctx context.Context, nodeID string) error { return nil }
func (t *txStore) AllEmbeddings(ctx context.Context) (map[string][]float32, error) { return nil, nil }
func (t *txStore) GetEmbeddingsBatch(ctx context.Context, offset, limit int) (map[string][]float32, error) { return nil, nil }
func (t *txStore) AddFileWatch(ctx context.Context, filePath, nodeID, gitHash string) error { return nil }
func (t *txStore) AddReplayEvent(ctx context.Context, sessionID, data string) error { return nil }
func (t *txStore) GetReplayEvents(ctx context.Context, sessionID string) ([]*ReplayEvent, error) { return nil, nil }
func (t *txStore) WithTx(ctx context.Context, fn func(Storage) error) error { return fn(t) }
func (t *txStore) Close() error { return nil }
