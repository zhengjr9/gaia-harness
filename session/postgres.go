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
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, model_json JSONB NOT NULL, system_prompt TEXT NOT NULL, messages_json JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`)
	return s, err
}
func (s *PostgresStore) Create(ctx context.Context, r Record) error {
	model, _ := json.Marshal(r.Model)
	messages, _ := json.Marshal(r.Messages)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,workspace_id,model_json,system_prompt,messages_json,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, r.ID, r.WorkspaceID, model, r.System, messages, r.CreatedAt, r.UpdatedAt)
	return err
}
func (s *PostgresStore) Get(ctx context.Context, id string) (Record, error) {
	var r Record
	var model, messages []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,model_json,system_prompt,messages_json,created_at,updated_at FROM sessions WHERE id=$1`, id).Scan(&r.ID, &r.WorkspaceID, &model, &r.System, &messages, &r.CreatedAt, &r.UpdatedAt)
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
	return r, nil
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
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET workspace_id=$1,model_json=$2,system_prompt=$3,messages_json=$4,updated_at=$5 WHERE id=$6`, r.WorkspaceID, model, r.System, messages, r.UpdatedAt, r.ID)
	return err
}
