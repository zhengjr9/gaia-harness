package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

type ID string

type Record struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id"`
	CWD         string             `json:"cwd,omitempty"`
	Model       provider.Model     `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []provider.Message `json:"messages,omitempty"`
	CreatedAt   time.Time          `json:"created_at,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at,omitempty"`
}

type Store interface {
	Create(context.Context, Record) error
	Get(context.Context, string) (Record, error)
	UpdateModel(context.Context, string, provider.Model) error
	Append(context.Context, string, provider.Message) error
	ReplaceMessages(context.Context, string, []provider.Message) error
}
type Lister interface {
	List(context.Context) ([]Record, error)
}

type Compressor interface {
	Compress(context.Context, Record) ([]provider.Message, error)
}

type Service struct {
	Store      Store
	Compressor Compressor
}

func (s Service) Create(ctx context.Context, record Record) error {
	if record.ID == "" || record.WorkspaceID == "" {
		return fmt.Errorf("session id and workspace_id are required")
	}
	if filepath.Base(record.WorkspaceID) != record.WorkspaceID || strings.Contains(record.WorkspaceID, string(filepath.Separator)) {
		return fmt.Errorf("invalid workspace_id")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = record.CreatedAt
	return s.Store.Create(ctx, record)
}

func (s Service) Append(ctx context.Context, id string, message provider.Message) error {
	return s.AppendMessages(ctx, id, []provider.Message{message})
}

func (s Service) AppendMessages(ctx context.Context, id string, messages []provider.Message) error {
	for _, message := range messages {
		if err := s.Store.Append(ctx, id, message); err != nil {
			return err
		}
	}
	return s.compact(ctx, id)
}

func (s Service) compact(ctx context.Context, id string) error {
	if s.Compressor == nil {
		return nil
	}
	record, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	messages, err := s.Compressor.Compress(ctx, record)
	if err != nil {
		return err
	}
	return s.Store.ReplaceMessages(ctx, id, messages)
}

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{records: make(map[string]Record)} }
func (s *MemoryStore) Create(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[r.ID] = clone(r)
	return nil
}
func (s *MemoryStore) Get(_ context.Context, id string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return clone(r), nil
}
func (s *MemoryStore) UpdateModel(_ context.Context, id string, model provider.Model) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	r.Model = model
	r.UpdatedAt = time.Now().UTC()
	s.records[id] = r
	return nil
}
func (s *MemoryStore) List(_ context.Context) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, clone(r))
	}
	return out, nil
}
func (s *MemoryStore) Append(_ context.Context, id string, m provider.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	r.Messages = append(r.Messages, m)
	r.UpdatedAt = time.Now().UTC()
	s.records[id] = r
	return nil
}
func (s *MemoryStore) ReplaceMessages(_ context.Context, id string, m []provider.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	r.Messages = append([]provider.Message(nil), m...)
	r.UpdatedAt = time.Now().UTC()
	s.records[id] = r
	return nil
}

var ErrNotFound = &notFoundError{}

type notFoundError struct{}

func (*notFoundError) Error() string { return "session not found" }

func clone(r Record) Record { r.Messages = append([]provider.Message(nil), r.Messages...); return r }
