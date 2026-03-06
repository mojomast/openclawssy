package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/memory"

	_ "modernc.org/sqlite"
)

const (
	defaultDirMode  = 0o755
	defaultFileMode = 0o600
)

var ErrNotFound = errors.New("memory store: item not found")

type SQLiteStore struct {
	path    string
	agentID string
	db      *sql.DB
}

func OpenSQLite(path, agentID string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	agentID = strings.TrimSpace(agentID)
	if path == "" {
		return nil, errors.New("memory store: db path is required")
	}
	if agentID == "" {
		return nil, errors.New("memory store: agent id is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), defaultDirMode); err != nil {
		return nil, fmt.Errorf("memory store: create dir: %w", err)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("memory store: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &SQLiteStore{path: path, agentID: agentID, db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, defaultFileMode); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) Upsert(ctx context.Context, item memory.MemoryItem) (memory.MemoryItem, error) {
	if s == nil || s.db == nil {
		return memory.MemoryItem{}, errors.New("memory store: nil database")
	}
	item = memory.NormalizeItem(item)
	if item.AgentID == "" {
		item.AgentID = s.agentID
	}
	if item.AgentID != s.agentID {
		return memory.MemoryItem{}, errors.New("memory store: cross-agent write denied")
	}
	if item.ID == "" {
		item.ID = fmt.Sprintf("mem_%d", time.Now().UTC().UnixNano())
	}
	if item.Title == "" {
		item.Title = item.Kind
	}
	now := time.Now().UTC()
	createdAt := now
	if existingCreatedAt, ok, err := s.createdAtByID(ctx, item.ID); err != nil {
		return memory.MemoryItem{}, err
	} else if ok {
		createdAt = existingCreatedAt
	}
	item.CreatedAt = createdAt
	item.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.MemoryItem{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_items (
			id, agent_id, kind, title, content, importance, confidence, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind=excluded.kind,
			title=excluded.title,
			content=excluded.content,
			importance=excluded.importance,
			confidence=excluded.confidence,
			status=excluded.status,
			updated_at=excluded.updated_at
	`, item.ID, item.AgentID, item.Kind, item.Title, item.Content, item.Importance, item.Confidence, item.Status, item.CreatedAt, item.UpdatedAt); err != nil {
		return memory.MemoryItem{}, err
	}

	if err := syncFTS(ctx, tx, item); err != nil {
		return memory.MemoryItem{}, err
	}

	if err := tx.Commit(); err != nil {
		return memory.MemoryItem{}, err
	}
	return item, nil
}

func (s *SQLiteStore) Update(ctx context.Context, item memory.MemoryItem) (memory.MemoryItem, error) {
	item = memory.NormalizeItem(item)
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return memory.MemoryItem{}, errors.New("memory store: id is required")
	}
	if _, ok, err := s.createdAtByID(ctx, item.ID); err != nil {
		return memory.MemoryItem{}, err
	} else if !ok {
		return memory.MemoryItem{}, ErrNotFound
	}
	return s.Upsert(ctx, item)
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (memory.MemoryItem, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return memory.MemoryItem{}, false, errors.New("memory store: id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, kind, title, content, importance, confidence, status, created_at, updated_at
		FROM memory_items
		WHERE id = ? AND agent_id = ?
		LIMIT 1
	`, id, s.agentID)
	var item memory.MemoryItem
	if err := row.Scan(
		&item.ID,
		&item.AgentID,
		&item.Kind,
		&item.Title,
		&item.Content,
		&item.Importance,
		&item.Confidence,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return memory.MemoryItem{}, false, nil
		}
		return memory.MemoryItem{}, false, err
	}
	return item, true, nil
}

func (s *SQLiteStore) Forget(ctx context.Context, id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, errors.New("memory store: id is required")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE memory_items
		SET status = ?, updated_at = ?
		WHERE id = ? AND agent_id = ?
	`, memory.MemoryStatusForgotten, now, id, s.agentID)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts WHERE id = ?`, id); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE memory_id = ?`, id); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) Archive(ctx context.Context, id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, errors.New("memory store: id is required")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE memory_items
		SET status = ?, updated_at = ?
		WHERE id = ? AND agent_id = ?
	`, memory.MemoryStatusArchived, now, id, s.agentID)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts WHERE id = ?`, id); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE memory_id = ?`, id); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) Search(ctx context.Context, params memory.SearchParams) ([]memory.MemoryItem, error) {
	params = memory.NormalizeSearchParams(params)
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = memory.MemoryStatusActive
	}
	if strings.TrimSpace(params.Query) == "" {
		return s.searchWithoutQuery(ctx, params, status)
	}
	query := buildFTSQuery(params.Query)
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.agent_id, m.kind, m.title, m.content, m.importance, m.confidence, m.status, m.created_at, m.updated_at
		FROM memory_fts f
		JOIN memory_items m ON m.id = f.id
		WHERE m.agent_id = ?
		  AND m.status = ?
		  AND m.importance >= ?
		  AND f.memory_fts MATCH ?
		ORDER BY bm25(memory_fts), m.importance DESC, m.updated_at DESC
		LIMIT ?
	`, s.agentID, status, params.MinImportance, query, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return items, nil
	}
	return s.searchBySubstring(ctx, params, status)
}

func (s *SQLiteStore) Health(ctx context.Context) (memory.Health, error) {
	counts, err := s.countByStatus(ctx)
	if err != nil {
		return memory.Health{}, err
	}
	var sizeBytes int64
	if info, err := os.Stat(s.path); err == nil {
		sizeBytes = info.Size()
	}
	return memory.Health{
		DBPath:         s.path,
		DBSizeBytes:    sizeBytes,
		TotalItems:     counts[memory.MemoryStatusActive] + counts[memory.MemoryStatusForgotten] + counts[memory.MemoryStatusArchived],
		ActiveItems:    counts[memory.MemoryStatusActive],
		ForgottenItems: counts[memory.MemoryStatusForgotten],
		ArchivedItems:  counts[memory.MemoryStatusArchived],
	}, nil
}

func (s *SQLiteStore) List(ctx context.Context, status string, limit int) ([]memory.MemoryItem, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		status = memory.MemoryStatusActive
	}
	if limit <= 0 {
		limit = 1000
	}
	if limit > 20000 {
		limit = 20000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, kind, title, content, importance, confidence, status, created_at, updated_at
		FROM memory_items
		WHERE agent_id = ?
		  AND status = ?
		ORDER BY updated_at DESC
		LIMIT ?
	`, s.agentID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *SQLiteStore) Vacuum(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("memory store: nil database")
	}
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}

func (s *SQLiteStore) searchWithoutQuery(ctx context.Context, params memory.SearchParams, status string) ([]memory.MemoryItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, kind, title, content, importance, confidence, status, created_at, updated_at
		FROM memory_items
		WHERE agent_id = ?
		  AND status = ?
		  AND importance >= ?
		ORDER BY importance DESC, updated_at DESC
		LIMIT ?
	`, s.agentID, status, params.MinImportance, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *SQLiteStore) searchBySubstring(ctx context.Context, params memory.SearchParams, status string) ([]memory.MemoryItem, error) {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(params.Query)))
	if len(tokens) == 0 {
		return nil, nil
	}
	clauses := make([]string, 0, len(tokens))
	args := make([]any, 0, 3+(len(tokens)*2))
	args = append(args, s.agentID, status, params.MinImportance)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		clauses = append(clauses, `(lower(m.title) LIKE ? OR lower(m.content) LIKE ?)`)
		pattern := "%" + token + "%"
		args = append(args, pattern, pattern)
	}
	if len(clauses) == 0 {
		return nil, nil
	}
	args = append(args, params.Limit)
	query := fmt.Sprintf(`
		SELECT m.id, m.agent_id, m.kind, m.title, m.content, m.importance, m.confidence, m.status, m.created_at, m.updated_at
		FROM memory_items m
		WHERE m.agent_id = ?
		  AND m.status = ?
		  AND m.importance >= ?
		  AND %s
		ORDER BY m.importance DESC, m.updated_at DESC
		LIMIT ?
	`, strings.Join(clauses, " AND "))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *SQLiteStore) createdAtByID(ctx context.Context, id string) (time.Time, bool, error) {
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM memory_items WHERE id = ? AND agent_id = ?`, id, s.agentID).Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return createdAt, true, nil
}

func (s *SQLiteStore) countByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*)
		FROM memory_items
		WHERE agent_id = ?
		GROUP BY status
	`, s.agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memory_items (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			importance INTEGER NOT NULL,
			confidence REAL NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_items_agent_status_importance_updated
			ON memory_items(agent_id, status, importance DESC, updated_at DESC)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
			id UNINDEXED,
			title,
			content
		)`,
		`CREATE TABLE IF NOT EXISTS memory_embeddings (
			memory_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			model TEXT NOT NULL,
			vector_json TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_embeddings_agent_updated
			ON memory_embeddings(agent_id, updated_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("memory store: migrate: %w", err)
		}
	}
	return nil
}

func syncFTS(ctx context.Context, tx *sql.Tx, item memory.MemoryItem) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts WHERE id = ?`, item.ID); err != nil {
		return err
	}
	if item.Status != memory.MemoryStatusActive {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO memory_fts(id, title, content) VALUES (?, ?, ?)`, item.ID, item.Title, item.Content)
	return err
}

func scanItems(rows *sql.Rows) ([]memory.MemoryItem, error) {
	items := []memory.MemoryItem{}
	for rows.Next() {
		var item memory.MemoryItem
		if err := rows.Scan(
			&item.ID,
			&item.AgentID,
			&item.Kind,
			&item.Title,
			&item.Content,
			&item.Importance,
			&item.Confidence,
			&item.Status,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func buildFTSQuery(raw string) string {
	tokens := strings.Fields(strings.TrimSpace(raw))
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ReplaceAll(token, `"`, "")
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		parts = append(parts, `"`+token+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " AND ")
}
