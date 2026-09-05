package protocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
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
	if s.runs == nil {
		s.runs = map[string]context.CancelFunc{}
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
	var writeMu sync.Mutex
	write := func(message nativeServerMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return s.writeNative(conn, message)
	}
	if err := write(nativeServerMessage{Type: "hello", Version: Version, ConnectionID: fmt.Sprintf("ws-%d", time.Now().UnixNano()), Snapshot: nativeSnapshot(s.snapshot(ctx))}); err != nil {
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
			go s.handleNativeRequest(ctx, message, write)
		}
	}
}

func (s *Server) handleNativeRequest(ctx context.Context, message nativeClientMessage, write func(nativeServerMessage) error) {
	started := time.Now()
	s.logf("native request id=%s command=%s session=%s", message.ID, message.Request.Command, message.Request.SessionID)
	requestContext := ctx
	finish := func() {}
	if message.Request.Command == "prompt" || message.Request.Command == "steer" {
		var err error
		requestContext, finish, err = s.startRun(ctx, message.Request.SessionID)
		if err != nil {
			s.writeNativeError(message.ID, err, write)
			return
		}
	}
	defer finish()
	progress := s.nativeProgressEmitter(message.Request.SessionID, write)
	result, commandErr := s.commandForNative(requestContext, message.Request, progress)
	ok := commandErr == nil
	response := nativeServerMessage{Type: "response", ID: message.ID, OK: &ok}
	if commandErr == nil {
		response.Result = result
	} else {
		code := "internal_error"
		if errors.Is(commandErr, context.Canceled) {
			code = "aborted"
		}
		response.Error = &ProtocolError{Code: code, Message: commandErr.Error()}
	}
	if commandErr == nil {
		if snapshot, ok := nativeSessionFromResult(result); ok {
			if err := write(nativeServerMessage{Type: "event", Event: map[string]any{"type": "session_snapshot", "snapshot": snapshot}}); err != nil {
				return
			}
		}
	}
	_ = write(response)
	s.logf("native response id=%s command=%s session=%s ok=%t duration=%s", message.ID, message.Request.Command, message.Request.SessionID, ok, time.Since(started))
}

func (s *Server) logf(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, args...)
	}
}

func (s *Server) writeNativeError(id string, commandErr error, write func(nativeServerMessage) error) {
	ok := false
	_ = write(nativeServerMessage{Type: "response", ID: id, OK: &ok, Error: &ProtocolError{Code: "internal_error", Message: commandErr.Error()}})
}

func nativeSessionFromResult(result any) (nativeSessionSnapshot, bool) {
	value, ok := result.(map[string]any)
	if !ok {
		return nativeSessionSnapshot{}, false
	}
	snapshot, ok := value["session"].(nativeSessionSnapshot)
	return snapshot, ok
}

func (s *Server) commandForNative(ctx context.Context, command nativeCommand, observer func(provider.Event)) (any, error) {
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
		snapshot, err := s.nativePrompt(request, command, observer)
		return map[string]any{"command": command.Command, "session": nativeSession(snapshot)}, err
	case "abort":
		s.abortRun(command.SessionID)
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

func (s *Server) nativePrompt(r *http.Request, command nativeCommand, observer func(provider.Event)) (SessionSnapshot, error) {
	if s.Runner == nil {
		return SessionSnapshot{}, fmt.Errorf("agent runner is not configured")
	}
	if command.SessionID == "" {
		return SessionSnapshot{}, fmt.Errorf("sessionId is required")
	}
	_, err := s.Runner.RunWithEvents(r.Context(), command.SessionID, provider.Message{Role: provider.RoleUser, Content: []provider.Content{{Text: command.Text}}}, observer)
	if err != nil {
		return SessionSnapshot{}, err
	}
	return s.get(r, command.SessionID)
}

func (s *Server) nativeProgressEmitter(sessionID string, write func(nativeServerMessage) error) func(provider.Event) {
	state := nativeProgressState{sessionID: sessionID}
	return func(event provider.Event) {
		if sessionID == "" {
			return
		}
		state.apply(event)
		if state.item.ID == "" {
			return
		}
		_ = write(nativeServerMessage{Type: "event", Event: map[string]any{
			"type": "session_progress", "sessionId": sessionID, "progress": state.progress,
		}})
	}
}

type nativeProgressState struct {
	sessionID string
	item      nativeTranscriptItem
	progress  map[string]any
	sequence  int
}

func (s *nativeProgressState) apply(event provider.Event) {
	if event.Type == provider.EventToolStart && event.ToolCall != nil {
		s.sequence++
		input := map[string]any{}
		_ = json.Unmarshal([]byte(event.ToolCall.Arguments), &input)
		s.item = nativeTranscriptItem{ID: fmt.Sprintf("tool-%s-%d", s.sessionID, s.sequence), Role: string(provider.RoleTool), Content: []map[string]any{}, ToolCallID: event.ToolCall.ID, ToolName: event.ToolCall.Name, Input: input, Status: "running", Timestamp: time.Now().UnixMilli()}
		s.progress = map[string]any{"type": "item_started", "item": s.item}
		return
	}
	if event.Type == provider.EventToolEnd && event.ToolCall != nil && event.ToolResult != nil {
		s.item.ToolCallID = event.ToolCall.ID
		s.item.ToolName = event.ToolCall.Name
		s.item.Content = []map[string]any{{"type": "text", "text": event.ToolResult.Content}}
		s.item.IsError = event.ToolResult.IsError
		if event.ToolResult.IsError {
			s.item.Status = "error"
		} else {
			s.item.Status = "complete"
		}
		s.progress = map[string]any{"type": "item_finished", "item": s.item}
		return
	}
	if event.Type == provider.EventStart {
		s.sequence++
		id := fmt.Sprintf("stream-%s-%d", s.sessionID, s.sequence)
		model := ModelRef{}
		if event.Response != nil {
			model = ModelRef{Provider: event.Response.Provider, ID: event.Response.Model}
		}
		s.item = nativeTranscriptItem{ID: id, Role: string(provider.RoleAssistant), Content: []map[string]any{}, Model: &model, Status: "streaming", Timestamp: time.Now().UnixMilli()}
		s.progress = map[string]any{"type": "item_started", "item": s.item}
		return
	}
	if s.item.ID == "" {
		return
	}
	if event.Type == provider.EventTextDelta {
		s.appendText(event.Delta)
	} else if event.Type == provider.EventThinkingDelta {
		s.appendThinking(event.Delta)
	} else if event.Type == provider.EventToolCallDelta && event.ToolCall != nil {
		s.appendToolCall(event.ToolCall, event.Delta)
	} else if event.Type == provider.EventDone && event.Response != nil {
		s.item.Content = nativeContent(event.Response.Content)
		s.item.Status = "complete"
		s.item.StopReason = nativeStopReason(event.Response.StopReason)
		s.progress = map[string]any{"type": "item_finished", "item": s.item}
		return
	} else if event.Type == provider.EventError {
		s.item.Status = "error"
		s.item.StopReason = "error"
		if event.Err != nil {
			s.item.ErrorMessage = event.Err.Error()
		}
		s.progress = map[string]any{"type": "item_finished", "item": s.item}
		return
	} else {
		return
	}
	s.progress = map[string]any{"type": "item_updated", "item": s.item}
}

func (s *nativeProgressState) appendText(delta string) {
	if len(s.item.Content) == 0 || s.item.Content[len(s.item.Content)-1]["type"] != "text" {
		s.item.Content = append(s.item.Content, map[string]any{"type": "text", "text": ""})
	}
	part := s.item.Content[len(s.item.Content)-1]
	part["text"] = part["text"].(string) + delta
}

func (s *nativeProgressState) appendThinking(delta string) {
	if len(s.item.Content) == 0 || s.item.Content[len(s.item.Content)-1]["type"] != "thinking" {
		s.item.Content = append(s.item.Content, map[string]any{"type": "thinking", "thinking": ""})
	}
	part := s.item.Content[len(s.item.Content)-1]
	part["thinking"] = part["thinking"].(string) + delta
}

func (s *nativeProgressState) appendToolCall(call *provider.ToolCall, delta string) {
	if len(s.item.Content) == 0 || s.item.Content[len(s.item.Content)-1]["type"] != "toolCall" {
		s.item.Content = append(s.item.Content, map[string]any{"type": "toolCall", "toolCallId": call.ID, "toolName": call.Name, "input": ""})
	}
	part := s.item.Content[len(s.item.Content)-1]
	part["toolCallId"], part["toolName"] = call.ID, call.Name
	part["input"] = part["input"].(string) + delta
}

func nativeStopReason(reason provider.StopReason) string {
	switch reason {
	case provider.StopReasonLength:
		return "length"
	case provider.StopReasonToolUse:
		return "toolUse"
	default:
		return "stop"
	}
}

func nativeContent(content []provider.Content) []map[string]any {
	out := make([]map[string]any, 0, len(content))
	for _, item := range content {
		switch {
		case item.Text != "":
			out = append(out, map[string]any{"type": "text", "text": item.Text})
		case item.Thinking != "":
			out = append(out, map[string]any{"type": "thinking", "thinking": item.Thinking})
		case item.ToolCall != nil:
			input := any(item.ToolCall.Arguments)
			var decoded map[string]any
			if json.Unmarshal([]byte(item.ToolCall.Arguments), &decoded) == nil {
				input = decoded
			}
			out = append(out, map[string]any{"type": "toolCall", "toolCallId": item.ToolCall.ID, "toolName": item.ToolCall.Name, "input": input})
		case item.ToolResult != nil:
			out = append(out, map[string]any{"type": "text", "text": item.ToolResult.Content})
		}
	}
	return out
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
	if err := s.Sessions.Store.UpdateThinking(ctx, id, thinking); err != nil {
		return err
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
	ErrorMessage  string           `cbor:"errorMessage,omitempty"`
	ToolCallID    string           `cbor:"toolCallId,omitempty"`
	ToolName      string           `cbor:"toolName,omitempty"`
	Input         map[string]any   `cbor:"input,omitempty"`
	// isError is required by pi-protocol for every tool transcript item,
	// including successful and running items where its value is false.
	IsError   bool  `cbor:"isError"`
	Timestamp int64 `cbor:"timestamp"`
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
		contextWindow := model.ContextWindow
		if contextWindow < 1 {
			contextWindow = 128000
		}
		maxTokens := model.MaxTokens
		if maxTokens < 1 {
			maxTokens = 8192
		}
		models = append(models, nativeModel{Provider: model.Provider, ID: model.ID, Name: model.Name, API: api, Reasoning: model.Reasoning, Input: input, ContextWindow: contextWindow, MaxTokens: maxTokens, Cost: map[string]float64{"input": nonNegative(model.Cost.Input), "output": nonNegative(model.Cost.Output), "cacheRead": nonNegative(model.Cost.CacheRead), "cacheWrite": nonNegative(model.Cost.CacheWrite)}, SupportedThinkingLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}, Authenticated: true})
	}
	return &nativeServerSnapshot{ServerID: snapshot.ServerID, ProtocolVersion: snapshot.ProtocolVersion, Revision: snapshot.Revision, Sessions: snapshot.Sessions, Models: models}
}

func nonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
