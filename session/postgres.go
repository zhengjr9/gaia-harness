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
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, cwd TEXT NOT NULL DEFAULT '', model_json JSONB NOT NULL, system_prompt TEXT NOT NULL, messages_json JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`)
	return s, err
}
func (s *PostgresStore) Create(ctx context.Context, r Record) error {
	model, _ := json.Marshal(r.Model)
	messages, _ := json.Marshal(r.Messages)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,workspace_id,cwd,model_json,system_prompt,messages_json,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, r.ID, r.WorkspaceID, r.CWD, model, r.System, messages, r.CreatedAt, r.UpdatedAt)
	return err
}
func (s *PostgresStore) Get(ctx context.Context, id string) (Record, error) {
	var r Record
	var model, messages []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,cwd,model_json,system_prompt,messages_json,created_at,updated_at FROM sessions WHERE id=$1`, id).Scan(&r.ID, &r.WorkspaceID, &r.CWD, &model, &r.System, &messages, &r.CreatedAt, &r.UpdatedAt)
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
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET workspace_id=$1,cwd=$2,model_json=$3,system_prompt=$4,messages_json=$5,updated_at=$6 WHERE id=$7`, r.WorkspaceID, r.CWD, model, r.System, messages, r.UpdatedAt, r.ID)
	return err
}
