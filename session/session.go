package session

import (
	"context"
	"sync"
	"time"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

type ID string

type Record struct {
	ID, WorkspaceID string
	Model           provider.Model
	System          string
	Messages        []provider.Message
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Store interface {
	Create(context.Context, Record) error
	Get(context.Context, string) (Record, error)
	Append(context.Context, string, provider.Message) error
	ReplaceMessages(context.Context, string, []provider.Message) error
}

type Compressor interface {
	Compress(context.Context, Record) ([]provider.Message, error)
}

type Service struct {
	Store      Store
	Compressor Compressor
}

func (s Service) Create(ctx context.Context, record Record) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = record.CreatedAt
	return s.Store.Create(ctx, record)
}

func (s Service) Append(ctx context.Context, id string, message provider.Message) error {
	if err := s.Store.Append(ctx, id, message); err != nil {
		return err
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
