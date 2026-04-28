package storage

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Types

type Node struct {
	ID, Type, Content, ContentHash, Summary, Scope, Project, Tags string
	Tier                                                          int
	Confidence                                                    float64
	AccessCount                                                   int
	CreatedAt, UpdatedAt, AccessedAt                              time.Time
	SourceSession, SourceAgent                                    string
	Version                                                       int
}

type Edge struct {
	ID, FromID, ToID, Type, Metadata string
	Acyclic                          bool
	Weight                           float64
	CreatedAt                        time.Time
}

type Session struct {
	ID, Project, Summary, Agent string
	StartedAt, EndedAt          time.Time
}

type NodeVersion struct {
	NodeID, Content, ChangedBy, Reason string
	Version                            int
	ChangedAt                          time.Time
}

type NodeFilter struct {
	Type, Scope, Project string
	Tier                 int
	MinConfidence        float64
}

// Store

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
	s := &Store{db: db}
	if err := s.createTables(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SearchNodeByHash finds a node by content hash + scope + project (dedup check).
func (s *Store) SearchNodeByHash(hash, scope, project string) (*Node, error) {
	row := s.db.QueryRow(
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

func (s *Store) CreateNode(n *Node) error {
	_, err := s.db.Exec(`INSERT INTO nodes (id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Type, n.Content, n.ContentHash, n.Summary, n.Scope, n.Project, n.Tier, n.Tags, n.Confidence, n.AccessCount,
		n.CreatedAt, n.UpdatedAt, nullTime(n.AccessedAt), n.SourceSession, n.SourceAgent, n.Version)
	return err
}

func (s *Store) GetNode(id string) (*Node, error) {
	n := &Node{}
	var accessedAt sql.NullTime
	err := s.db.QueryRow(`SELECT id, type, content, content_hash, summary, scope, project, tier, tags, confidence, access_count, created_at, updated_at, accessed_at, source_session, source_agent, version FROM nodes WHERE id = ?`, id).
		Scan(&n.ID, &n.Type, &n.Content, &n.ContentHash, &n.Summary, &n.Scope, &n.Project, &n.Tier, &n.Tags, &n.Confidence, &n.AccessCount, &n.CreatedAt, &n.UpdatedAt, &accessedAt, &n.SourceSession, &n.SourceAgent, &n.Version)
	if err != nil {
		return nil, err
	}
	if accessedAt.Valid {
		n.AccessedAt = accessedAt.Time
	}
	return n, nil
}

func (s *Store) UpdateNode(n *Node) error {
	_, err := s.db.Exec(`UPDATE nodes SET type=?, content=?, content_hash=?, summary=?, scope=?, project=?, tier=?, tags=?, confidence=?, access_count=?, updated_at=?, accessed_at=?, source_session=?, source_agent=?, version=? WHERE id=?`,
		n.Type, n.Content, n.ContentHash, n.Summary, n.Scope, n.Project, n.Tier, n.Tags, n.Confidence, n.AccessCount,
		n.UpdatedAt, nullTime(n.AccessedAt), n.SourceSession, n.SourceAgent, n.Version, n.ID)
	return err
}

func (s *Store) DeleteNode(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM edges WHERE from_id=? OR to_id=?`, id, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM nodes WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListNodes(f NodeFilter) ([]*Node, error) {
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
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *Store) SearchNodes(query string, limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 10
	}
	// Convert multi-word query to FTS5 OR query: "auth JWT" → "auth OR JWT"
	words := strings.Fields(query)
	ftsQuery := strings.Join(words, " OR ")
	rows, err := s.db.Query(`SELECT n.id, n.type, n.content, n.content_hash, n.summary, n.scope, n.project, n.tier, n.tags, n.confidence, n.access_count, n.created_at, n.updated_at, n.accessed_at, n.source_session, n.source_agent, n.version
		FROM nodes_fts f JOIN nodes n ON f.rowid = n.rowid WHERE nodes_fts MATCH ? ORDER BY rank LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// --- Edges ---

func (s *Store) CreateEdge(e *Edge) error {
	_, err := s.db.Exec(`INSERT INTO edges (id, from_id, to_id, type, acyclic, weight, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.FromID, e.ToID, e.Type, e.Acyclic, e.Weight, e.Metadata, e.CreatedAt)
	return err
}

func (s *Store) GetEdge(id string) (*Edge, error) {
	e := &Edge{}
	err := s.db.QueryRow(`SELECT id, from_id, to_id, type, acyclic, weight, metadata, created_at FROM edges WHERE id=?`, id).
		Scan(&e.ID, &e.FromID, &e.ToID, &e.Type, &e.Acyclic, &e.Weight, &e.Metadata, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Store) DeleteEdge(id string) error {
	_, err := s.db.Exec(`DELETE FROM edges WHERE id=?`, id)
	return err
}

func (s *Store) GetEdgesFrom(nodeID string) ([]*Edge, error) {
	return s.queryEdges(`SELECT id, from_id, to_id, type, acyclic, weight, metadata, created_at FROM edges WHERE from_id=?`, nodeID)
}

func (s *Store) GetEdgesTo(nodeID string) ([]*Edge, error) {
	return s.queryEdges(`SELECT id, from_id, to_id, type, acyclic, weight, metadata, created_at FROM edges WHERE to_id=?`, nodeID)
}

func (s *Store) GetNeighbors(nodeID string) ([]*Node, error) {
	rows, err := s.db.Query(`SELECT DISTINCT n.id, n.type, n.content, n.content_hash, n.summary, n.scope, n.project, n.tier, n.tags, n.confidence, n.access_count, n.created_at, n.updated_at, n.accessed_at, n.source_session, n.source_agent, n.version
		FROM nodes n JOIN edges e ON (e.to_id = n.id AND e.from_id = ?) OR (e.from_id = n.id AND e.to_id = ?)`, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// --- Sessions ---

func (s *Store) CreateSession(sess *Session) error {
	_, err := s.db.Exec(`INSERT INTO sessions (id, project, started_at, ended_at, summary, agent) VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Project, sess.StartedAt, nullTime(sess.EndedAt), sess.Summary, sess.Agent)
	return err
}

func (s *Store) EndSession(id string, summary string) error {
	_, err := s.db.Exec(`UPDATE sessions SET ended_at=?, summary=? WHERE id=?`, time.Now(), summary, id)
	return err
}

func (s *Store) ListSessions(project string, limit int) ([]*Session, error) {
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
	rows, err := s.db.Query(q, args...)
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

func (s *Store) AddFileWatch(filePath, nodeID, gitHash string) error {
	_, err := s.db.Exec(`INSERT INTO file_watch (file_path, node_id, git_hash) VALUES (?, ?, ?)`, filePath, nodeID, gitHash)
	return err
}

func (s *Store) GetNodesByFile(filePath string) ([]*Node, error) {
	rows, err := s.db.Query(`SELECT n.id, n.type, n.content, n.content_hash, n.summary, n.scope, n.project, n.tier, n.tags, n.confidence, n.access_count, n.created_at, n.updated_at, n.accessed_at, n.source_session, n.source_agent, n.version
		FROM nodes n JOIN file_watch fw ON fw.node_id = n.id WHERE fw.file_path = ?`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// --- Versions ---

func (s *Store) SaveVersion(nodeID string, content, changedBy, reason string) error {
	var maxVer int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM node_versions WHERE node_id=?`, nodeID).Scan(&maxVer)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO node_versions (node_id, version, content, changed_at, changed_by, reason) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID, maxVer+1, content, time.Now(), changedBy, reason)
	return err
}

func (s *Store) GetVersions(nodeID string) ([]*NodeVersion, error) {
	rows, err := s.db.Query(`SELECT node_id, version, content, changed_at, changed_by, reason FROM node_versions WHERE node_id=? ORDER BY version`, nodeID)
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

func (s *Store) queryEdges(query string, args ...any) ([]*Edge, error) {
	rows, err := s.db.Query(query, args...)
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
