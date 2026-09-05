package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	provider "github.com/zhengjiarui/gaia-ai-provider"
)

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) (*PostgresStore, error) {
	s := &PostgresStore{db: db}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, cwd TEXT NOT NULL DEFAULT '', model_json JSONB NOT NULL, thinking_level TEXT NOT NULL DEFAULT 'off', system_prompt TEXT NOT NULL, messages_json JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`)
	if err != nil {
		return s, err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS thinking_level TEXT NOT NULL DEFAULT 'off'`)
	return s, err
}
func (s *PostgresStore) Create(ctx context.Context, r Record) error {
	model, _ := json.Marshal(r.Model)
	messages, _ := json.Marshal(r.Messages)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,workspace_id,cwd,model_json,thinking_level,system_prompt,messages_json,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ID, r.WorkspaceID, r.CWD, model, r.ThinkingLevel, r.System, messages, r.CreatedAt, r.UpdatedAt)
	return err
}
func (s *PostgresStore) Get(ctx context.Context, id string) (Record, error) {
	var r Record
	var model, messages []byte
	var thinking string
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,cwd,model_json,thinking_level,system_prompt,messages_json,created_at,updated_at FROM sessions WHERE id=$1`, id).Scan(&r.ID, &r.WorkspaceID, &r.CWD, &model, &thinking, &r.System, &messages, &r.CreatedAt, &r.UpdatedAt)
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
	r.ThinkingLevel = thinking
	return r, nil
}
func (s *PostgresStore) UpdateModel(ctx context.Context, id string, model provider.Model) error {
	encoded, err := json.Marshal(model)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET model_json=$1, updated_at=$2 WHERE id=$3`, encoded, time.Now().UTC(), id)
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
func (s *PostgresStore) UpdateThinking(ctx context.Context, id, thinking string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET thinking_level=$1, updated_at=$2 WHERE id=$3`, thinking, time.Now().UTC(), id)
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
func (s *PostgresStore) List(ctx context.Context) ([]Record, error) {
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
func (s *PostgresStore) Append(ctx context.Context, id string, m provider.Message) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	r.Messages = append(r.Messages, m)
	r.UpdatedAt = time.Now().UTC()
	return s.replace(ctx, r)
}
func (s *PostgresStore) ReplaceMessages(ctx context.Context, id string, m []provider.Message) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	r.Messages = m
	r.UpdatedAt = time.Now().UTC()
	return s.replace(ctx, r)
}
func (s *PostgresStore) replace(ctx context.Context, r Record) error {
	model, _ := json.Marshal(r.Model)
	messages, _ := json.Marshal(r.Messages)
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET workspace_id=$1,cwd=$2,model_json=$3,thinking_level=$4,system_prompt=$5,messages_json=$6,updated_at=$7 WHERE id=$8`, r.WorkspaceID, r.CWD, model, r.ThinkingLevel, r.System, messages, r.UpdatedAt, r.ID)
	return err
}
