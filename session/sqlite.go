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
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, model_json BLOB NOT NULL, system_prompt TEXT NOT NULL, messages_json BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	return s, err
}
func (s *SQLiteStore) Create(ctx context.Context, r Record) error {
	m, _ := json.Marshal(r.Messages)
	model, _ := json.Marshal(r.Model)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions VALUES (?, ?, ?, ?, ?, ?, ?)`, r.ID, r.WorkspaceID, model, r.System, m, r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano))
	return err
}
func (s *SQLiteStore) Get(ctx context.Context, id string) (Record, error) {
	var r Record
	var model, messages []byte
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, model_json, system_prompt, messages_json, created_at, updated_at FROM sessions WHERE id = ?`, id).Scan(&r.ID, &r.WorkspaceID, &model, &r.System, &messages, &created, &updated)
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
