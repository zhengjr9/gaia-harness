// Package protocol implements the JSON-over-HTTP/SSE transport for pi-tui's
// protocol vocabulary. It keeps the same hello, request, response and event
// envelopes as @earendil-works/pi-protocol; HTTP is the transport adapter.
package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/session"
)

const Version = 1

type ModelRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}
type Command struct {
	Command       string    `json:"command"`
	SessionID     string    `json:"sessionId,omitempty"`
	Text          string    `json:"text,omitempty"`
	CWD           string    `json:"cwd,omitempty"`
	Name          string    `json:"name,omitempty"`
	Model         *ModelRef `json:"model,omitempty"`
	ThinkingLevel string    `json:"thinkingLevel,omitempty"`
}
type RequestEnvelope struct {
	Type    string  `json:"type"`
	ID      string  `json:"id"`
	Request Command `json:"request"`
}
type ClientMessage struct {
	Type    string  `json:"type"`
	Version int     `json:"version,omitempty"`
	ID      string  `json:"id,omitempty"`
	Request Command `json:"request,omitempty"`
}
type SessionMetadata struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	CWD       string `json:"cwd"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}
type SessionSnapshot struct {
	ID            string             `json:"id"`
	Name          string             `json:"name,omitempty"`
	CWD           string             `json:"cwd"`
	CreatedAt     int64              `json:"createdAt"`
	UpdatedAt     int64              `json:"updatedAt"`
	Phase         string             `json:"phase"`
	Model         ModelRef           `json:"model"`
	ThinkingLevel string             `json:"thinkingLevel"`
	Attached      bool               `json:"attached"`
	Locked        bool               `json:"locked"`
	Revision      int                `json:"revision"`
	Transcript    []provider.Message `json:"transcript"`
}
type ServerSnapshot struct {
	ServerID        string            `json:"serverId"`
	ProtocolVersion int               `json:"protocolVersion"`
	Revision        int               `json:"revision"`
	Sessions        []SessionMetadata `json:"sessions"`
	Models          []provider.Model  `json:"models"`
}
type ServerHello struct {
	Type         string         `json:"type"`
	Version      int            `json:"version"`
	ConnectionID string         `json:"connectionId"`
	Snapshot     ServerSnapshot `json:"snapshot"`
}
type ResponseEnvelope struct {
	Type   string         `json:"type"`
	ID     string         `json:"id"`
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *ProtocolError `json:"error,omitempty"`
}
type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type EventEnvelope struct {
	Type  string `json:"type"`
	Event any    `json:"event"`
}
type sessionState struct {
	Metadata SessionMetadata
	Model    provider.Model
	Name     string
}

type Server struct {
	Sessions session.Service
	Runner   *session.Runner
	Registry *provider.Registry
	CWD      string
	mu       sync.RWMutex
	states   map[string]sessionState
}

func (s *Server) Handler() http.Handler {
	if s.CWD == "" {
		s.CWD, _ = os.Getwd()
	}
	if s.states == nil {
		s.states = map[string]sessionState{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pi/hello", s.hello)
	mux.HandleFunc("POST /v1/pi", s.message)
	mux.HandleFunc("POST /v1/pi/stream", s.stream)
	return mux
}
func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	write(w, ServerHello{Type: "hello", Version: Version, ConnectionID: fmt.Sprintf("http-%d", time.Now().UnixNano()), Snapshot: s.snapshot(r.Context())})
}
func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	var m ClientMessage
	if json.NewDecoder(r.Body).Decode(&m) != nil {
		writeError(w, "", "invalid_request", "invalid JSON")
		return
	}
	if m.Type == "hello" {
		if m.Version != Version {
			writeError(w, "", "version", "unsupported protocol version")
			return
		}
		write(w, ServerHello{Type: "hello", Version: Version, ConnectionID: fmt.Sprintf("http-%d", time.Now().UnixNano()), Snapshot: s.snapshot(r.Context())})
		return
	}
	if m.Type != "request" {
		writeError(w, m.ID, "invalid_request", "expected hello or request")
		return
	}
	result, err := s.command(r, m.Request)
	if err != nil {
		writeError(w, m.ID, "internal_error", err.Error())
		return
	}
	write(w, ResponseEnvelope{Type: "response", ID: m.ID, OK: true, Result: result})
}
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	var m ClientMessage
	if json.NewDecoder(r.Body).Decode(&m) != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	if m.Type != "request" {
		http.Error(w, "expected request", 400)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	writeSSE(w, EventEnvelope{Type: "event", Event: map[string]any{"type": "server_snapshot", "snapshot": s.snapshot(r.Context())}})
	flusher.Flush()
	result, err := s.command(r, m.Request)
	if err != nil {
		writeSSE(w, EventEnvelope{Type: "event", Event: map[string]any{"type": "error", "error": err.Error()}})
		flusher.Flush()
		return
	}
	writeSSE(w, EventEnvelope{Type: "event", Event: map[string]any{"type": "command_result", "result": result}})
	flusher.Flush()
}
func (s *Server) command(r *http.Request, c Command) (any, error) {
	switch c.Command {
	case "list":
		return s.list(r.Context()), nil
	case "create":
		return s.create(r, c)
	case "attach":
		return s.get(r, c.SessionID)
	case "prompt", "steer":
		return s.prompt(r, c)
	case "abort":
		return s.get(r, c.SessionID)
	default:
		return nil, fmt.Errorf("unsupported command %q", c.Command)
	}
}
func (s *Server) create(r *http.Request, c Command) (SessionSnapshot, error) {
	id := fmt.Sprintf("s-%d", time.Now().UnixNano())
	cwd := c.CWD
	if cwd == "" {
		cwd = s.CWD
	}
	cwd, _ = filepath.Abs(cwd)
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return SessionSnapshot{}, fmt.Errorf("invalid cwd")
	}
	model := provider.Model{}
	if c.Model != nil {
		model.Provider = c.Model.Provider
		model.ID = c.Model.ID
	}
	if model.Provider == "" || model.ID == "" {
		model = s.defaultModel()
	}
	record := session.Record{ID: id, WorkspaceID: id, CWD: cwd, Model: model, System: "You are a reliable coding agent. Use the available sandbox tools when needed."}
	if err := s.Sessions.Create(r.Context(), record); err != nil {
		return SessionSnapshot{}, err
	}
	now := time.Now().UnixMilli()
	s.mu.Lock()
	s.states[id] = sessionState{Metadata: SessionMetadata{ID: id, CWD: cwd, CreatedAt: now, UpdatedAt: now}, Model: model, Name: c.Name}
	s.mu.Unlock()
	return s.get(r, id)
}
func (s *Server) prompt(r *http.Request, c Command) (any, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("agent runner is not configured")
	}
	if c.SessionID == "" {
		return nil, fmt.Errorf("sessionId is required")
	}
	res, err := s.Runner.Run(r.Context(), c.SessionID, provider.Message{Role: provider.RoleUser, Content: []provider.Content{{Text: c.Text}}})
	if err != nil {
		return nil, err
	}
	return res, nil
}
func (s *Server) get(r *http.Request, id string) (SessionSnapshot, error) {
	record, err := s.Sessions.Store.Get(r.Context(), id)
	if err != nil {
		return SessionSnapshot{}, err
	}
	s.mu.RLock()
	state := s.states[id]
	s.mu.RUnlock()
	return SessionSnapshot{ID: id, Name: state.Name, CWD: record.CWD, CreatedAt: record.CreatedAt.UnixMilli(), UpdatedAt: record.UpdatedAt.UnixMilli(), Phase: "idle", Model: ModelRef{Provider: record.Model.Provider, ID: record.Model.ID}, ThinkingLevel: "off", Attached: true, Transcript: record.Messages}, nil
}
func (s *Server) list(ctx context.Context) map[string]any {
	s.mu.RLock()
	states := make(map[string]sessionState, len(s.states))
	for id, state := range s.states {
		states[id] = state
	}
	s.mu.RUnlock()
	out := []SessionMetadata{}
	if lister, ok := s.Sessions.Store.(session.Lister); ok {
		if records, err := lister.List(ctx); err == nil {
			for _, record := range records {
				state := states[record.ID]
				metadata := state.Metadata
				metadata.ID = record.ID
				metadata.CWD = record.CWD
				metadata.CreatedAt = record.CreatedAt.UnixMilli()
				metadata.UpdatedAt = record.UpdatedAt.UnixMilli()
				out = append(out, metadata)
				delete(states, record.ID)
			}
		}
	}
	for _, v := range states {
		out = append(out, v.Metadata)
	}
	return map[string]any{"command": "list", "sessions": out}
}
func (s *Server) defaultModel() provider.Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Registry != nil {
		models := s.Registry.Models()
		if len(models) > 0 {
			return models[0]
		}
	}
	return provider.Model{Provider: "ark", ID: "deepseek-v4-flash", ContextWindow: 128000, MaxTokens: 8192}
}
func (s *Server) snapshot(ctx context.Context) ServerSnapshot {
	models := []provider.Model{}
	if s.Registry != nil {
		models = s.Registry.Models()
	}
	return ServerSnapshot{ServerID: "gaia-harness", ProtocolVersion: Version, Sessions: s.list(ctx)["sessions"].([]SessionMetadata), Models: models}
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, id, code, message string) {
	write(w, ResponseEnvelope{Type: "response", ID: id, OK: false, Error: &ProtocolError{Code: code, Message: message}})
}
func writeSSE(w http.ResponseWriter, v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
