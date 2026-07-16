package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id            TEXT PRIMARY KEY,
	current_stage TEXT NOT NULL,
	holder_agent  TEXT NOT NULL,
	updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS handoffs (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id    TEXT NOT NULL,
	source_agent  TEXT NOT NULL,
	target_agent  TEXT NOT NULL,
	stage         TEXT NOT NULL,
	task          TEXT NOT NULL,
	payload_file  TEXT NOT NULL,
	created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_handoffs_project ON handoffs(project_id, created_at);
`

// SQLiteStore is a Store backed by a local SQLite file (pure-Go driver,
// no CGO). It is a rebuildable index over .ao/ — never the source of truth.
type SQLiteStore struct {
	db *sql.DB
}

// OpenSQLite opens (creating if necessary) the SQLite index at path and
// ensures its schema is up to date. Pass ":memory:" for an ephemeral store.
func OpenSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc.org/sqlite does not support concurrent writers well; a single
	// connection avoids "database is locked" errors for this CLI/MCP tool.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate sqlite schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) SaveHandoff(entry Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertHandoff(tx, entry); err != nil {
		return err
	}
	if err := upsertProject(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

func insertHandoff(tx *sql.Tx, entry Entry) error {
	env := entry.Envelope
	_, err := tx.Exec(
		`INSERT INTO handoffs (project_id, source_agent, target_agent, stage, task, payload_file, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		env.ProjectID, env.SourceAgent, env.TargetAgent, env.CurrentStage,
		env.Payload.Task, entry.PayloadFile, env.Metadata.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert handoff: %w", err)
	}
	return nil
}

func upsertProject(tx *sql.Tx, entry Entry) error {
	env := entry.Envelope
	_, err := tx.Exec(
		`INSERT INTO projects (id, current_stage, holder_agent, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   current_stage = excluded.current_stage,
		   holder_agent  = excluded.holder_agent,
		   updated_at    = excluded.updated_at`,
		env.ProjectID, env.CurrentStage, env.TargetAgent, env.Metadata.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert project: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListHandoffs(projectID string, limit int) ([]HandoffRecord, error) {
	query := `SELECT id, project_id, source_agent, target_agent, stage, task, payload_file, created_at
	          FROM handoffs WHERE project_id = ? ORDER BY id DESC`
	args := []any{projectID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list handoffs: %w", err)
	}
	defer rows.Close()

	var out []HandoffRecord
	for rows.Next() {
		var r HandoffRecord
		var createdAt string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.SourceAgent, &r.TargetAgent, &r.Stage, &r.Task, &r.PayloadFile, &createdAt); err != nil {
			return nil, fmt.Errorf("scan handoff: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ProjectState(projectID string) (ProjectRecord, error) {
	var r ProjectRecord
	var updatedAt string
	err := s.db.QueryRow(
		`SELECT id, current_stage, holder_agent, updated_at FROM projects WHERE id = ?`,
		projectID,
	).Scan(&r.ID, &r.CurrentStage, &r.HolderAgent, &updatedAt)
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("project %q not found in index: %w", projectID, err)
	}
	if err != nil {
		return r, fmt.Errorf("query project state: %w", err)
	}
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return r, nil
}

func (s *SQLiteStore) Reindex(projectID string, entries []Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM handoffs WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("clear handoffs: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE id = ?`, projectID); err != nil {
		return fmt.Errorf("clear project: %w", err)
	}
	for _, entry := range entries {
		if err := insertHandoff(tx, entry); err != nil {
			return err
		}
		if err := upsertProject(tx, entry); err != nil {
			return err
		}
	}
	return tx.Commit()
}
