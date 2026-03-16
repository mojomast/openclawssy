package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	evalStoreDirMode  = 0o755
	evalStoreFileMode = 0o600
)

var ErrInvalidStorePath = errors.New("eval store: invalid sqlite path")

type Store struct {
	path  string
	db    *sql.DB
	nowFn func() time.Time
}

func OpenStore(path string) (*Store, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil, ErrInvalidStorePath
	}

	if err := os.MkdirAll(filepath.Dir(trimmedPath), evalStoreDirMode); err != nil {
		return nil, fmt.Errorf("eval store: create directory: %w", err)
	}

	dsn := trimmedPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("eval store: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{path: trimmedPath, db: db, nowFn: time.Now}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(trimmedPath, evalStoreFileMode); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = db.Close()
		return nil, fmt.Errorf("eval store: chmod db file: %w", err)
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) SaveRun(ctx context.Context, run SuiteRun) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("eval store: nil database")
	}
	run.Suite = strings.TrimSpace(run.Suite)
	if run.Suite == "" {
		return 0, errors.New("eval store: suite is required")
	}
	if run.Timestamp.IsZero() {
		run.Timestamp = s.nowFn().UTC()
	}

	resultsJSON, err := json.Marshal(run.Results)
	if err != nil {
		return 0, fmt.Errorf("eval store: encode results: %w", err)
	}
	metricsJSON, err := json.Marshal(run.Metrics)
	if err != nil {
		return 0, fmt.Errorf("eval store: encode metrics: %w", err)
	}
	identityJSON, err := json.Marshal(run.Identity)
	if err != nil {
		return 0, fmt.Errorf("eval store: encode identity: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO eval_results (suite, timestamp, results_json, metrics_json, identity_json)
		VALUES (?, ?, ?, ?, ?)
	`, run.Suite, run.Timestamp.UTC(), string(resultsJSON), string(metricsJSON), string(identityJSON))
	if err != nil {
		return 0, fmt.Errorf("eval store: insert run: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("eval store: read insert id: %w", err)
	}
	return id, nil
}

func (s *Store) LatestRun(ctx context.Context, suite string) (SuiteRun, bool, error) {
	if s == nil || s.db == nil {
		return SuiteRun{}, false, errors.New("eval store: nil database")
	}
	suite = strings.TrimSpace(suite)
	if suite == "" {
		return SuiteRun{}, false, errors.New("eval store: suite is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, suite, timestamp, results_json, metrics_json, identity_json
		FROM eval_results
		WHERE suite = ?
		ORDER BY timestamp DESC, id DESC
		LIMIT 1
	`, suite)

	run, err := scanSuiteRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SuiteRun{}, false, nil
		}
		return SuiteRun{}, false, err
	}
	return run, true, nil
}

func (s *Store) ListRuns(ctx context.Context, suite string, limit int) ([]SuiteRun, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("eval store: nil database")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 20000 {
		limit = 20000
	}

	suite = strings.TrimSpace(suite)

	query := `
		SELECT id, suite, timestamp, results_json, metrics_json, identity_json
		FROM eval_results
	`
	args := make([]any, 0, 2)
	if suite != "" {
		query += " WHERE suite = ?"
		args = append(args, suite)
	}
	query += " ORDER BY timestamp DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval store: list runs query: %w", err)
	}
	defer rows.Close()

	runs := make([]SuiteRun, 0)
	for rows.Next() {
		run, err := scanSuiteRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eval store: list runs iteration: %w", err)
	}

	return runs, nil
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS eval_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			suite TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			results_json TEXT NOT NULL,
			metrics_json TEXT NOT NULL,
			identity_json TEXT NOT NULL DEFAULT '{}'
		)`,
		`ALTER TABLE eval_results ADD COLUMN identity_json TEXT NOT NULL DEFAULT '{}'`,
		`CREATE INDEX IF NOT EXISTS idx_eval_results_suite_timestamp
			ON eval_results(suite, timestamp DESC, id DESC)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(stmt, "ALTER TABLE") && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return fmt.Errorf("eval store: migrate: %w", err)
		}
	}
	return nil
}

type suiteRunScanner interface {
	Scan(dest ...any) error
}

func scanSuiteRun(scanner suiteRunScanner) (SuiteRun, error) {
	var (
		run          SuiteRun
		resultsJSON  string
		metricsJSON  string
		identityJSON string
	)

	if err := scanner.Scan(&run.ID, &run.Suite, &run.Timestamp, &resultsJSON, &metricsJSON, &identityJSON); err != nil {
		return SuiteRun{}, err
	}
	if err := json.Unmarshal([]byte(resultsJSON), &run.Results); err != nil {
		return SuiteRun{}, fmt.Errorf("eval store: decode results: %w", err)
	}
	if err := json.Unmarshal([]byte(metricsJSON), &run.Metrics); err != nil {
		return SuiteRun{}, fmt.Errorf("eval store: decode metrics: %w", err)
	}
	if strings.TrimSpace(identityJSON) != "" {
		if err := json.Unmarshal([]byte(identityJSON), &run.Identity); err != nil {
			return SuiteRun{}, fmt.Errorf("eval store: decode identity: %w", err)
		}
	}
	return run, nil
}
