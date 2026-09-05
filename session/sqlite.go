package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	s := &SQLiteStore{db: db}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, cwd TEXT NOT NULL DEFAULT '', model_json BLOB NOT NULL, system_prompt TEXT NOT NULL, messages_json BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	if err != nil {
		return s, err
	}
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN cwd TEXT NOT NULL DEFAULT ''`)
	return s, nil
}
func (s *SQLiteStore) Create(ctx context.Context, r Record) error {
	m, _ := json.Marshal(r.Messages)
	model, _ := json.Marshal(r.Model)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,workspace_id,cwd,model_json,system_prompt,messages_json,created_at,updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.WorkspaceID, r.CWD, model, r.System, m, r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano))
	return err
}
func (s *SQLiteStore) Get(ctx context.Context, id string) (Record, error) {
	var r Record
	var model, messages []byte
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, cwd, model_json, system_prompt, messages_json, created_at, updated_at FROM sessions WHERE id = ?`, id).Scan(&r.ID, &r.WorkspaceID, &r.CWD, &model, &r.System, &messages, &created, &updated)
	if err == sql.ErrNoRows {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(model, &r.Model); err != nil {
		return r, err
	}
	if err = json.Unmarshal(messages, &r.Messages); err != nil {
		return r, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}
func (s *SQLiteStore) UpdateModel(ctx context.Context, id string, model provider.Model) error {
	encoded, err := json.Marshal(model)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET model_json=?, updated_at=? WHERE id=?`, encoded, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *SQLiteStore) List(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		r, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) Append(ctx context.Context, id string, m provider.Message) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	r.Messages = append(r.Messages, m)
	return s.Replace(ctx, r)
}
func (s *SQLiteStore) ReplaceMessages(ctx context.Context, id string, m []provider.Message) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	r.Messages = m
	return s.Replace(ctx, r)
}
func (s *SQLiteStore) Replace(ctx context.Context, r Record) error {
	m, _ := json.Marshal(r.Messages)
	model, _ := json.Marshal(r.Model)
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET workspace_id=?, model_json=?, system_prompt=?, messages_json=?, updated_at=? WHERE id=?`, r.WorkspaceID, model, r.System, m, r.UpdatedAt.Format(time.RFC3339Nano), r.ID)
	return err
}
