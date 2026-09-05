package protocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gorilla/websocket"
	provider "github.com/zhengjiarui/gaia-ai-provider"
)

// NativeHandler serves the wire protocol used by @earendil-works/pi-protocol:
// length-prefixed CBOR messages over a bidirectional byte transport. WebSocket
// is only the HTTP upgrade used to establish that byte transport.
func (s *Server) NativeHandler() http.Handler {
	if s.CWD == "" {
		s.CWD, _ = os.Getwd()
	}
	if s.states == nil {
		s.states = map[string]sessionState{}
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := s.nativeConnection(r.Context(), conn); err != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()), time.Now().Add(time.Second))
		}
	})
}

type nativeClientMessage struct {
	Type    string        `cbor:"type"`
	Version int           `cbor:"version,omitempty"`
	ID      string        `cbor:"id,omitempty"`
	Request nativeCommand `cbor:"request,omitempty"`
}

type nativeCommand struct {
	Command       string    `cbor:"command"`
	SessionID     string    `cbor:"sessionId,omitempty"`
	Text          string    `cbor:"text,omitempty"`
	CWD           string    `cbor:"cwd,omitempty"`
	Name          string    `cbor:"name,omitempty"`
	Model         *ModelRef `cbor:"model,omitempty"`
	ThinkingLevel string    `cbor:"thinkingLevel,omitempty"`
}

type nativeServerMessage struct {
	Type         string                `cbor:"type"`
	Version      int                   `cbor:"version,omitempty"`
	ConnectionID string                `cbor:"connectionId,omitempty"`
	Snapshot     *nativeServerSnapshot `cbor:"snapshot,omitempty"`
	ID           string                `cbor:"id,omitempty"`
	OK           *bool                 `cbor:"ok,omitempty"`
	Result       any                   `cbor:"result,omitempty"`
	Error        *ProtocolError        `cbor:"error,omitempty"`
	Event        any                   `cbor:"event,omitempty"`
}

func (s *Server) nativeConnection(ctx context.Context, conn *websocket.Conn) error {
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	if messageType != websocket.BinaryMessage {
		return fmt.Errorf("pi protocol requires binary websocket frames")
	}
	frames, err := decodeNativeFrames(payload)
	if err != nil || len(frames) != 1 {
		return fmt.Errorf("invalid pi protocol frame: %w", err)
	}
	var hello nativeClientMessage
	if err := cbor.Unmarshal(frames[0], &hello); err != nil {
		return err
	}
	if hello.Type != "hello" || hello.Version != Version {
		return s.writeNative(conn, nativeServerMessage{Type: "hello_error", Error: &ProtocolError{Code: "version", Message: "unsupported protocol version"}})
	}
	if err := s.writeNative(conn, nativeServerMessage{Type: "hello", Version: Version, ConnectionID: fmt.Sprintf("ws-%d", time.Now().UnixNano()), Snapshot: nativeSnapshot(s.snapshot(ctx))}); err != nil {
		return err
	}
	for {
		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.BinaryMessage {
			return fmt.Errorf("pi protocol requires binary websocket frames")
		}
		frames, err = decodeNativeFrames(payload)
		if err != nil {
			return err
		}
		for _, frame := range frames {
			var message nativeClientMessage
			if err := cbor.Unmarshal(frame, &message); err != nil {
				return err
			}
			if message.Type != "request" {
				continue
			}
			result, commandErr := s.commandForNative(ctx, message.Request)
			ok := commandErr == nil
			response := nativeServerMessage{Type: "response", ID: message.ID, OK: &ok}
			if commandErr == nil {
				response.Result = result
			}
			if commandErr != nil {
				response.Error = &ProtocolError{Code: "internal_error", Message: commandErr.Error()}
			}
			if commandErr == nil {
				if snapshot, ok := nativeSessionFromResult(result); ok {
					if err := s.writeNative(conn, nativeServerMessage{Type: "event", Event: map[string]any{"type": "session_snapshot", "snapshot": snapshot}}); err != nil {
						return err
					}
				}
			}
			if err := s.writeNative(conn, response); err != nil {
				return err
			}
		}
	}
}

func nativeSessionFromResult(result any) (nativeSessionSnapshot, bool) {
	value, ok := result.(map[string]any)
	if !ok {
		return nativeSessionSnapshot{}, false
	}
	snapshot, ok := value["session"].(nativeSessionSnapshot)
	return snapshot, ok
}

func (s *Server) commandForNative(ctx context.Context, command nativeCommand) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://pi.local", nil)
	if err != nil {
		return nil, err
	}
	request = request.WithContext(ctx)
	switch command.Command {
	case "list":
		return s.list(ctx), nil
	case "create":
		snapshot, err := s.create(request, Command{Command: command.Command, CWD: command.CWD, Name: command.Name, Model: command.Model, ThinkingLevel: command.ThinkingLevel})
		return map[string]any{"command": "create", "session": nativeSession(snapshot)}, err
	case "attach":
		snapshot, err := s.get(request, command.SessionID)
		return map[string]any{"command": "attach", "session": nativeSession(snapshot)}, err
	case "prompt", "steer":
		snapshot, err := s.nativePrompt(request, command)
		return map[string]any{"command": command.Command, "session": nativeSession(snapshot)}, err
	case "abort":
		snapshot, err := s.get(request, command.SessionID)
		return map[string]any{"command": "abort", "session": nativeSession(snapshot)}, err
	case "set_model":
		if command.Model == nil || command.Model.Provider == "" || command.Model.ID == "" {
			return nil, fmt.Errorf("model is required")
		}
		if err := s.setModel(request, command.SessionID, *command.Model); err != nil {
			return nil, err
		}
		snapshot, err := s.get(request, command.SessionID)
		return map[string]any{"command": "set_model", "session": nativeSession(snapshot)}, err
	case "set_thinking":
		if err := s.setThinking(request.Context(), command.SessionID, command.ThinkingLevel); err != nil {
			return nil, err
		}
		snapshot, err := s.get(request, command.SessionID)
		return map[string]any{"command": "set_thinking", "session": nativeSession(snapshot)}, err
	case "detach":
		return map[string]any{"command": "detach", "sessionId": command.SessionID}, nil
	default:
		return nil, fmt.Errorf("unsupported command %q", command.Command)
	}
}

func (s *Server) nativePrompt(r *http.Request, command nativeCommand) (SessionSnapshot, error) {
	_, err := s.prompt(r, Command{Command: command.Command, SessionID: command.SessionID, Text: command.Text})
	if err != nil {
		return SessionSnapshot{}, err
	}
	return s.get(r, command.SessionID)
}

func (s *Server) setModel(r *http.Request, id string, model ModelRef) error {
	if s.Registry == nil {
		return fmt.Errorf("model registry is not configured")
	}
	selected, ok := s.Registry.Model(model.Provider, model.ID)
	if !ok {
		return fmt.Errorf("unknown model %s/%s", model.Provider, model.ID)
	}
	if err := s.Sessions.Store.UpdateModel(r.Context(), id, selected); err != nil {
		return err
	}
	s.mu.Lock()
	state := s.states[id]
	state.Model = selected
	state.Metadata.UpdatedAt = time.Now().UnixMilli()
	s.states[id] = state
	s.mu.Unlock()
	return nil
}

func (s *Server) setThinking(ctx context.Context, id, thinking string) error {
	switch thinking {
	case "off", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("invalid thinking level %q", thinking)
	}
	s.mu.Lock()
	state, ok := s.states[id]
	if !ok {
		s.mu.Unlock()
		record, err := s.Sessions.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		state = sessionState{Metadata: SessionMetadata{ID: id, CWD: record.CWD, CreatedAt: record.CreatedAt.UnixMilli(), UpdatedAt: record.UpdatedAt.UnixMilli()}, Model: record.Model}
		s.mu.Lock()
	}
	state.ThinkingLevel = thinking
	state.Metadata.UpdatedAt = time.Now().UnixMilli()
	s.states[id] = state
	s.mu.Unlock()
	return nil
}

func (s *Server) writeNative(conn *websocket.Conn, message nativeServerMessage) error {
	payload, err := cbor.Marshal(message)
	if err != nil {
		return err
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

func decodeNativeFrames(payload []byte) ([][]byte, error) {
	var frames [][]byte
	for len(payload) > 0 {
		if len(payload) < 4 {
			return nil, fmt.Errorf("truncated frame header")
		}
		n := int(binary.BigEndian.Uint32(payload[:4]))
		if n > 16*1024*1024 || len(payload) < 4+n {
			return nil, fmt.Errorf("invalid frame length")
		}
		frames = append(frames, payload[4:4+n])
		payload = payload[4+n:]
	}
	return frames, nil
}

type nativeServerSnapshot struct {
	ServerID        string            `cbor:"serverId"`
	ProtocolVersion int               `cbor:"protocolVersion"`
	Revision        int               `cbor:"revision"`
	Sessions        []SessionMetadata `cbor:"sessions"`
	Models          []nativeModel     `cbor:"models"`
}

type nativeSessionSnapshot struct {
	ID               string                 `cbor:"id"`
	Name             string                 `cbor:"name,omitempty"`
	CWD              string                 `cbor:"cwd"`
	CreatedAt        int64                  `cbor:"createdAt"`
	UpdatedAt        int64                  `cbor:"updatedAt"`
	Phase            string                 `cbor:"phase"`
	Model            ModelRef               `cbor:"model"`
	ThinkingLevel    string                 `cbor:"thinkingLevel"`
	Attached         bool                   `cbor:"attached"`
	Locked           bool                   `cbor:"locked"`
	Revision         int                    `cbor:"revision"`
	Transcript       []nativeTranscriptItem `cbor:"transcript"`
	QueuedSteer      []nativeTranscriptItem `cbor:"queuedSteer"`
	QueuedSteerCount int                    `cbor:"queuedSteerCount"`
}

type nativeTranscriptItem struct {
	ID            string           `cbor:"id"`
	Role          string           `cbor:"role"`
	Content       []map[string]any `cbor:"content"`
	Model         *ModelRef        `cbor:"model,omitempty"`
	ResponseModel string           `cbor:"responseModel,omitempty"`
	Status        string           `cbor:"status,omitempty"`
	StopReason    string           `cbor:"stopReason,omitempty"`
	ToolCallID    string           `cbor:"toolCallId,omitempty"`
	ToolName      string           `cbor:"toolName,omitempty"`
	Input         map[string]any   `cbor:"input,omitempty"`
	IsError       bool             `cbor:"isError,omitempty"`
	Timestamp     int64            `cbor:"timestamp"`
}

func nativeSession(snapshot SessionSnapshot) nativeSessionSnapshot {
	transcript := make([]nativeTranscriptItem, 0, len(snapshot.Transcript))
	for index, message := range snapshot.Transcript {
		content := make([]map[string]any, 0, len(message.Content))
		for _, item := range message.Content {
			if item.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": item.Text})
			}
			if item.Thinking != "" {
				content = append(content, map[string]any{"type": "thinking", "thinking": item.Thinking})
			}
			if item.ToolCall != nil {
				content = append(content, map[string]any{"type": "toolCall", "toolCallId": item.ToolCall.ID, "toolName": item.ToolCall.Name, "input": map[string]any{}})
			}
			if item.ToolResult != nil {
				content = append(content, map[string]any{"type": "text", "text": item.ToolResult.Content})
			}
		}
		if content == nil {
			content = []map[string]any{}
		}
		id := fmt.Sprintf("message-%d", index+1)
		item := nativeTranscriptItem{ID: id, Role: string(message.Role), Content: content, Timestamp: time.Now().UnixMilli()}
		if message.Role == provider.RoleAssistant {
			item.Model = &snapshot.Model
			item.Status = "complete"
			item.StopReason = "stop"
		}
		if message.Role == provider.RoleTool {
			item.ToolCallID = message.ToolCallID
			item.ToolName = message.ToolCallID
			item.Input = map[string]any{}
			item.IsError = false
			item.Status = "complete"
		}
		for _, contentItem := range message.Content {
			if contentItem.ToolResult != nil {
				item.ToolCallID = contentItem.ToolResult.ToolCallID
				item.IsError = contentItem.ToolResult.IsError
			}
			if contentItem.ToolCall != nil {
				_ = json.Unmarshal([]byte(contentItem.ToolCall.Arguments), &item.Input)
				item.ToolCallID = contentItem.ToolCall.ID
				item.ToolName = contentItem.ToolCall.Name
			}
		}
		transcript = append(transcript, item)
	}
	return nativeSessionSnapshot{ID: snapshot.ID, Name: snapshot.Name, CWD: snapshot.CWD, CreatedAt: snapshot.CreatedAt, UpdatedAt: snapshot.UpdatedAt, Phase: snapshot.Phase, Model: snapshot.Model, ThinkingLevel: snapshot.ThinkingLevel, Attached: snapshot.Attached, Locked: snapshot.Locked, Revision: snapshot.Revision, Transcript: transcript, QueuedSteer: []nativeTranscriptItem{}, QueuedSteerCount: 0}
}

type nativeModel struct {
	Provider                string             `cbor:"provider"`
	ID                      string             `cbor:"id"`
	Name                    string             `cbor:"name"`
	API                     string             `cbor:"api"`
	Reasoning               bool               `cbor:"reasoning"`
	Input                   []string           `cbor:"input"`
	ContextWindow           int                `cbor:"contextWindow"`
	MaxTokens               int                `cbor:"maxTokens"`
	Cost                    map[string]float64 `cbor:"cost"`
	SupportedThinkingLevels []string           `cbor:"supportedThinkingLevels"`
	Authenticated           bool               `cbor:"authenticated"`
}

func nativeSnapshot(snapshot ServerSnapshot) *nativeServerSnapshot {
	models := make([]nativeModel, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		input := model.Input
		if input == nil {
			input = []string{"text"}
		}
		api := string(model.API)
		if api == "" {
			api = "openai-completions"
		}
		models = append(models, nativeModel{Provider: model.Provider, ID: model.ID, Name: model.Name, API: api, Reasoning: model.Reasoning, Input: input, ContextWindow: model.ContextWindow, MaxTokens: model.MaxTokens, Cost: map[string]float64{"input": model.Cost.Input, "output": model.Cost.Output, "cacheRead": model.Cost.CacheRead, "cacheWrite": model.Cost.CacheWrite}, SupportedThinkingLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}, Authenticated: true})
	}
	return &nativeServerSnapshot{ServerID: snapshot.ServerID, ProtocolVersion: snapshot.ProtocolVersion, Revision: snapshot.Revision, Sessions: snapshot.Sessions, Models: models}
}
