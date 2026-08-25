package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	CreatedAt int64  `json:"createdAt"`
}

type Worktree struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Path      string `json:"path"`
	IsPrimary bool   `json:"isPrimary"`
	CreatedAt int64  `json:"createdAt"`
}

type Session struct {
	ID                string  `json:"id"`
	WorktreeID        string  `json:"worktreeId"`
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	Status            string  `json:"status"`
	ProviderSessionID string  `json:"providerSessionId"`
	Error             string  `json:"error"`
	LastMessage       string  `json:"lastMessage"`
	Title             string  `json:"title"`
	TitleLocked       bool    `json:"titleLocked"`
	ParentID          string  `json:"parentId,omitempty"`
	BaselineHead      string  `json:"baselineHead"`
	CostKnown         bool    `json:"costKnown"`
	CachedTokens      int64   `json:"cachedTokens"`
	ReasoningTokens   int64   `json:"reasoningTokens"`
	Cost              float64 `json:"cost"`
	TokensIn          int64   `json:"tokensIn"`
	TokensOut         int64   `json:"tokensOut"`
	PID               int     `json:"pid"`
	CreatedAt         int64   `json:"createdAt"`
	UpdatedAt         int64   `json:"updatedAt"`
}

type FleetRow struct {
	Session
	WorktreeName string `json:"worktreeName"`
	Branch       string `json:"branch"`
	ProjectName  string `json:"projectName"`
	ProjectPath  string `json:"projectPath"`
}

type Message struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	Meta      string `json:"meta"`
	Ts        int64  `json:"ts"`
}

const schemaV1 = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	path TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS worktrees (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	branch TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL UNIQUE,
	is_primary INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_worktrees_project ON worktrees(project_id);
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	worktree_id TEXT NOT NULL REFERENCES worktrees(id) ON DELETE CASCADE,
	provider TEXT NOT NULL,
	model TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'idle',
	provider_session_id TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	last_message TEXT NOT NULL DEFAULT '',
	cost REAL NOT NULL DEFAULT 0,
	tokens_in INTEGER NOT NULL DEFAULT 0,
	tokens_out INTEGER NOT NULL DEFAULT 0,
	pid INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_worktree ON sessions(worktree_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT 'text',
	content TEXT NOT NULL DEFAULT '',
	meta TEXT NOT NULL DEFAULT '',
	ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, id);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

var schemaV2 = []string{
	`ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN title_locked INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sessions ADD COLUMN parent_id TEXT REFERENCES sessions(id) ON DELETE SET NULL`,
	`ALTER TABLE sessions ADD COLUMN baseline_head TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN cost_known INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sessions ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sessions ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0`,
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version < 1 {
		if _, err := db.Exec(schemaV1); err != nil {
			return err
		}
	}
	if version < 2 {
		for _, stmt := range schemaV2 {
			if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("%s: %w", stmt, err)
			}
		}
	}
	_, err := db.Exec(`PRAGMA user_version = 2`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func NewID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func now() int64 { return time.Now().UnixMilli() }

func (s *Store) CreateProject(name, path string) (*Project, error) {
	p := &Project{ID: NewID(), Name: name, Path: path, CreatedAt: now()}
	_, err := s.db.Exec(
		`INSERT INTO projects (id, name, path, created_at) VALUES (?,?,?,?)`,
		p.ID, p.Name, p.Path, p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id, name, path, created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteProject(id string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (s *Store) UpsertWorktree(w *Worktree) error {
	if w.ID == "" {
		w.ID = NewID()
	}
	if w.CreatedAt == 0 {
		w.CreatedAt = now()
	}
	_, err := s.db.Exec(
		`INSERT INTO worktrees (id, project_id, name, branch, path, is_primary, created_at)
		 VALUES (?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET branch=excluded.branch, path=excluded.path, name=excluded.name`,
		w.ID, w.ProjectID, w.Name, w.Branch, w.Path, b2i(w.IsPrimary), w.CreatedAt,
	)
	return err
}

func (s *Store) ListWorktrees(projectID string) ([]Worktree, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, name, branch, path, is_primary, created_at
		 FROM worktrees WHERE project_id = ? ORDER BY is_primary DESC, created_at`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Worktree
	for rows.Next() {
		var w Worktree
		var prim int
		if err := rows.Scan(&w.ID, &w.ProjectID, &w.Name, &w.Branch, &w.Path, &prim, &w.CreatedAt); err != nil {
			return nil, err
		}
		w.IsPrimary = prim == 1
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) GetWorktree(id string) (*Worktree, error) {
	var w Worktree
	var prim int
	err := s.db.QueryRow(
		`SELECT id, project_id, name, branch, path, is_primary, created_at
		 FROM worktrees WHERE id = ?`, id,
	).Scan(&w.ID, &w.ProjectID, &w.Name, &w.Branch, &w.Path, &prim, &w.CreatedAt)
	if err != nil {
		return nil, err
	}
	w.IsPrimary = prim == 1
	return &w, nil
}

func (s *Store) DeleteWorktree(id string) error {
	_, err := s.db.Exec(`DELETE FROM worktrees WHERE id = ?`, id)
	return err
}

func (s *Store) CreateSession(sess *Session) error {
	if sess.ID == "" {
		sess.ID = NewID()
	}
	if sess.Status == "" {
		sess.Status = "starting"
	}
	sess.CreatedAt = now()
	sess.UpdatedAt = sess.CreatedAt
	var parent any
	if sess.ParentID != "" {
		parent = sess.ParentID
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, worktree_id, provider, model, status, provider_session_id,
		   error, last_message, title, title_locked, parent_id, baseline_head,
		   cost_known, cached_tokens, reasoning_tokens,
		   cost, tokens_in, tokens_out, pid, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sess.ID, sess.WorktreeID, sess.Provider, sess.Model, sess.Status, sess.ProviderSessionID,
		sess.Error, sess.LastMessage, sess.Title, b2i(sess.TitleLocked), parent, sess.BaselineHead,
		b2i(sess.CostKnown), sess.CachedTokens, sess.ReasoningTokens,
		sess.Cost, sess.TokensIn, sess.TokensOut, sess.PID,
		sess.CreatedAt, sess.UpdatedAt,
	)
	return err
}

func (s *Store) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id)
	return scanSession(row)
}

func (s *Store) ListSessionsByWorktree(worktreeID string) ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT `+sessionCols+` FROM sessions WHERE worktree_id = ? ORDER BY created_at DESC`, worktreeID,
	)
	if err != nil {
		return nil, err
	}
	return collectSessions(rows)
}

func (s *Store) ListSessionsByStatus(statuses []string) ([]FleetRow, error) {
	q := `SELECT s.id, s.worktree_id, s.provider, s.model, s.status, s.provider_session_id,
	             s.error, s.last_message, s.title, s.title_locked, s.parent_id,
	             s.baseline_head, s.cost_known, s.cached_tokens, s.reasoning_tokens,
	             s.cost, s.tokens_in, s.tokens_out, s.pid,
	             s.created_at, s.updated_at,
	             w.name, w.branch, p.name, p.path
	      FROM sessions s
	      JOIN worktrees w ON w.id = s.worktree_id
	      JOIN projects p ON p.id = w.project_id`
	args := []interface{}{}
	if len(statuses) > 0 {
		q += ` WHERE s.status IN (` + placeholders(len(statuses)) + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY s.updated_at DESC LIMIT 500`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FleetRow
	for rows.Next() {
		var r FleetRow
		var titleLocked, costKnown int
		var parentID sql.NullString
		if err := rows.Scan(
			&r.ID, &r.WorktreeID, &r.Provider, &r.Model, &r.Status, &r.ProviderSessionID,
			&r.Error, &r.LastMessage, &r.Title, &titleLocked, &parentID,
			&r.BaselineHead, &costKnown, &r.CachedTokens, &r.ReasoningTokens,
			&r.Cost, &r.TokensIn, &r.TokensOut, &r.PID,
			&r.CreatedAt, &r.UpdatedAt,
			&r.WorktreeName, &r.Branch, &r.ProjectName, &r.ProjectPath,
		); err != nil {
			return nil, err
		}
		r.ParentID = parentID.String
		r.TitleLocked = titleLocked == 1
		r.CostKnown = costKnown == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSessionStatus(id, status, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, errMsg, now(), id,
	)
	return err
}

func (s *Store) SetSessionProviderID(id, psid string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET provider_session_id = ?, updated_at = ? WHERE id = ?`,
		psid, now(), id,
	)
	return err
}

func (s *Store) SetSessionPID(id string, pid int) error {
	_, err := s.db.Exec(`UPDATE sessions SET pid = ? WHERE id = ?`, pid, id)
	return err
}

func (s *Store) UpdateSessionProgress(id, lastMessage string, cost float64, tin, tout int64) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET last_message = ?, cost = cost + ?, tokens_in = tokens_in + ?,
		 tokens_out = tokens_out + ?, updated_at = ? WHERE id = ?`,
		truncate(lastMessage, 300), cost, tin, tout, now(), id,
	)
	return err
}

func (s *Store) RenameSession(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title must not be empty")
	}
	_, err := s.db.Exec(
		`UPDATE sessions SET title = ?, title_locked = 1, updated_at = ? WHERE id = ?`,
		truncate(title, 120), now(), id,
	)
	return err
}

func (s *Store) AutoTitle(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE sessions SET title = ? WHERE id = ? AND title = '' AND title_locked = 0`,
		truncate(title, 120), id,
	)
	return err
}

func (s *Store) SetBaselineHead(id, sha string) error {
	_, err := s.db.Exec(`UPDATE sessions SET baseline_head = ? WHERE id = ?`, sha, id)
	return err
}

func (s *Store) UpdateSessionUsage(id string, costKnown bool, cost float64, tin, tout, cached, reasoning int64) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET cost_known = MAX(cost_known, ?), cost = cost + ?, tokens_in = tokens_in + ?,
		 tokens_out = tokens_out + ?, cached_tokens = cached_tokens + ?, reasoning_tokens = reasoning_tokens + ?,
		 updated_at = ? WHERE id = ?`,
		b2i(costKnown), cost, tin, tout, cached, reasoning, now(), id,
	)
	return err
}

func (s *Store) GetSessionChildren(parentID string) ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT `+sessionCols+` FROM sessions WHERE parent_id = ? ORDER BY created_at ASC`, parentID,
	)
	if err != nil {
		return nil, err
	}
	return collectSessions(rows)
}

func (s *Store) DeleteSessionByID(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) ForkSession(id string, upToMessageID int64) (*Session, error) {
	src, err := s.GetSession(id)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	fork := &Session{
		ID:                NewID(),
		WorktreeID:        src.WorktreeID,
		Provider:          src.Provider,
		Model:             src.Model,
		Status:            "idle",
		ProviderSessionID: src.ProviderSessionID,
		Title:             truncate("Fork: "+firstNonEmptyStr(src.Title, "session"), 120),
		BaselineHead:      src.BaselineHead,
		CreatedAt:         now(),
		UpdatedAt:         now(),
	}
	var forkParent any
	if fork.ParentID != "" {
		forkParent = fork.ParentID
	}
	_, err = tx.Exec(
		`INSERT INTO sessions (id, worktree_id, provider, model, status, provider_session_id,
		   error, last_message, title, title_locked, parent_id, baseline_head,
		   cost_known, cached_tokens, reasoning_tokens,
		   cost, tokens_in, tokens_out, pid, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		fork.ID, fork.WorktreeID, fork.Provider, fork.Model, fork.Status, fork.ProviderSessionID,
		fork.Error, fork.LastMessage, fork.Title, b2i(fork.TitleLocked), forkParent, fork.BaselineHead,
		b2i(fork.CostKnown), fork.CachedTokens, fork.ReasoningTokens,
		fork.Cost, fork.TokensIn, fork.TokensOut, fork.PID,
		fork.CreatedAt, fork.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	upTo := upToMessageID
	if upTo <= 0 {
		upTo = int64(^uint64(0) >> 1)
	}
	if _, err := tx.Exec(
		`INSERT INTO messages (session_id, role, kind, content, meta, ts)
		 SELECT ?, role, kind, content, meta, ts FROM messages
		 WHERE session_id = ? AND id <= ? ORDER BY id ASC`,
		fork.ID, id, upTo,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return fork, nil
}

type SearchHit struct {
	MessageID int64  `json:"messageId"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Snippet   string `json:"snippet"`
}

func (s *Store) SearchSessions(query string, limit int) ([]FleetRow, []SearchHit, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil, nil
	}
	like := "%" + strings.ReplaceAll(strings.ReplaceAll(q, "%", "\\%"), "_", "\\_") + "%"
	fleetQ := `SELECT s.id, s.worktree_id, s.provider, s.model, s.status, s.provider_session_id,
	             s.error, s.last_message, s.title, s.title_locked, s.parent_id,
	             s.baseline_head, s.cost_known, s.cached_tokens, s.reasoning_tokens,
	             s.cost, s.tokens_in, s.tokens_out, s.pid,
	             s.created_at, s.updated_at,
	             w.name, w.branch, p.name, p.path
	      FROM sessions s
	      JOIN worktrees w ON w.id = s.worktree_id
	      JOIN projects p ON p.id = w.project_id
	      WHERE s.title LIKE ? ESCAPE '\' OR s.last_message LIKE ? ESCAPE '\'
	         OR w.name LIKE ? ESCAPE '\' OR w.branch LIKE ? ESCAPE '\'
	         OR p.name LIKE ? ESCAPE '\' OR s.provider LIKE ? ESCAPE '\' OR s.model LIKE ? ESCAPE '\'
	      ORDER BY s.updated_at DESC LIMIT ?`
	args := []interface{}{like, like, like, like, like, like, like, limit}
	rows, err := s.db.Query(fleetQ, args...)
	if err != nil {
		return nil, nil, err
	}
	var fleet []FleetRow
	for rows.Next() {
		var r FleetRow
		var titleLocked, costKnown int
		if err := rows.Scan(
			&r.ID, &r.WorktreeID, &r.Provider, &r.Model, &r.Status, &r.ProviderSessionID,
			&r.Error, &r.LastMessage, &r.Title, &titleLocked, &r.ParentID,
			&r.BaselineHead, &costKnown, &r.CachedTokens, &r.ReasoningTokens,
			&r.Cost, &r.TokensIn, &r.TokensOut, &r.PID,
			&r.CreatedAt, &r.UpdatedAt,
			&r.WorktreeName, &r.Branch, &r.ProjectName, &r.ProjectPath,
		); err != nil {
			rows.Close()
			return nil, nil, err
		}
		r.TitleLocked = titleLocked == 1
		r.CostKnown = costKnown == 1
		fleet = append(fleet, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fleet, nil, err
	}

	msgQ := `SELECT m.id, m.session_id, m.role, m.content, m.ts FROM messages m
	      JOIN sessions s ON s.id = m.session_id
	      WHERE m.content LIKE ? ESCAPE '\' AND m.kind IN ('text')
	      ORDER BY m.id DESC LIMIT ?`
	mrows, err := s.db.Query(msgQ, like, limit)
	if err != nil {
		return fleet, nil, err
	}
	defer mrows.Close()
	var hits []SearchHit
	for mrows.Next() {
		var h SearchHit
		var content string
		var ts int64
		if err := mrows.Scan(&h.MessageID, &h.SessionID, &h.Role, &content, &ts); err != nil {
			return fleet, nil, err
		}
		h.Snippet = snippetAround(content, q, 160)
		hits = append(hits, h)
	}
	return fleet, hits, mrows.Err()
}

func snippetAround(content, query string, width int) string {
	idx := strings.Index(strings.ToLower(content), strings.ToLower(query))
	if idx < 0 {
		return truncate(content, width)
	}
	start := idx - width/3
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(content) {
		end = len(content)
	}
	out := content[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(content) {
		out += "…"
	}
	return out
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Store) ActiveSessionInWorktree(worktreeID, provider string) (*Session, error) {
	row := s.db.QueryRow(
		`SELECT ` + sessionCols + ` FROM sessions
		 WHERE worktree_id = ? AND provider = ? AND status IN ('running','waiting','starting')
		 LIMIT 1`, worktreeID, provider,
	)
	sess, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *Store) InsertMessage(m *Message) (int64, error) {
	m.Ts = now()
	res, err := s.db.Exec(
		`INSERT INTO messages (session_id, role, kind, content, meta, ts) VALUES (?,?,?,?,?,?)`,
		m.SessionID, m.Role, m.Kind, m.Content, m.Meta, m.Ts,
	)
	if err != nil {
		return 0, err
	}
	m.ID, err = res.LastInsertId()
	return m.ID, err
}

func (s *Store) ListMessages(sessionID string, afterID int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, role, kind, content, meta, ts
		 FROM messages WHERE session_id = ? AND id > ? ORDER BY id ASC LIMIT ?`,
		sessionID, afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Kind, &m.Content, &m.Meta, &m.Ts); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?,?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value,
	)
	return err
}

const sessionCols = `id, worktree_id, provider, model, status, provider_session_id,
	error, last_message, title, title_locked, parent_id,
	baseline_head, cost_known, cached_tokens, reasoning_tokens,
	cost, tokens_in, tokens_out, pid, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(row rowScanner) (*Session, error) {
	var s Session
	var titleLocked, costKnown int
	var parentID sql.NullString
	err := row.Scan(
		&s.ID, &s.WorktreeID, &s.Provider, &s.Model, &s.Status, &s.ProviderSessionID,
		&s.Error, &s.LastMessage, &s.Title, &titleLocked, &parentID,
		&s.BaselineHead, &costKnown, &s.CachedTokens, &s.ReasoningTokens,
		&s.Cost, &s.TokensIn, &s.TokensOut, &s.PID,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	s.ParentID = parentID.String
	s.TitleLocked = titleLocked == 1
	s.CostKnown = costKnown == 1
	return &s, nil
}

func collectSessions(rows *sql.Rows) ([]Session, error) {
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
