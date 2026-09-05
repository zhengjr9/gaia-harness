package protocol

import (
	"context"
	"encoding/binary"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gorilla/websocket"
	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
	"github.com/zhengjiarui/gaia-harness/session"
)

type nativeIntegrationProvider struct{}

func (nativeIntegrationProvider) ID() string   { return "fake" }
func (nativeIntegrationProvider) Name() string { return "fake" }
func (nativeIntegrationProvider) Models() []provider.Model {
	return []provider.Model{{ID: "model", Name: "Fake", Provider: "fake", API: provider.ProtocolOpenAICompletions, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}}
}
func (nativeIntegrationProvider) Complete(context.Context, provider.Request) (*provider.Response, error) {
	return &provider.Response{Provider: "fake", Model: "model", Content: []provider.Content{{Text: "complete"}}}, nil
}
func (nativeIntegrationProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	out := make(chan provider.Event, 3)
	go func() {
		defer close(out)
		out <- provider.Event{Type: provider.EventStart, Response: &provider.Response{Provider: "fake", Model: req.Model.ID}}
		out <- provider.Event{Type: provider.EventTextDelta, Delta: "streamed"}
		out <- provider.Event{Type: provider.EventDone, Response: &provider.Response{Provider: "fake", Model: req.Model.ID, Content: []provider.Content{{Text: "streamed"}}, StopReason: provider.StopReasonStop}}
	}()
	return out, nil
}

func TestNativeWebSocketPromptStreamsAndPersists(t *testing.T) {
	store := session.NewMemoryStore()
	registry := provider.NewRegistry(nativeIntegrationProvider{})
	service := session.Service{Store: store}
	runner := &session.Runner{Store: store, Service: service, NewAgent: func(record session.Record) (*agent.Agent, error) {
		return agent.New(agent.Config{Registry: registry, Model: record.Model, ThinkingLevel: record.ThinkingLevel})
	}}
	server := &Server{Sessions: service, Runner: runner, Registry: registry, CWD: t.TempDir()}
	ts := httptest.NewServer(server.NativeHandler())
	defer ts.Close()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "ws"
	u.Path = "/v1/pi"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeFrame := func(value any) {
		payload, err := cbor.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		frame := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
		copy(frame[4:], payload)
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Fatal(err)
		}
	}
	read := func() nativeServerMessage {
		t.Helper()
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		_, frame, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if len(frame) < 4 {
			t.Fatalf("short frame: %d", len(frame))
		}
		var message nativeServerMessage
		if err := cbor.Unmarshal(frame[4:], &message); err != nil {
			t.Fatal(err)
		}
		return message
	}

	writeFrame(nativeClientMessage{Type: "hello", Version: Version})
	if message := read(); message.Type != "hello" {
		t.Fatalf("hello=%+v", message)
	}
	writeFrame(nativeClientMessage{Type: "request", ID: "create", Request: nativeCommand{Command: "create", Model: &ModelRef{Provider: "fake", ID: "model"}}})
	created := read()
	for created.Type == "event" {
		created = read()
	}
	if created.Type != "response" || created.OK == nil || !*created.OK {
		t.Fatalf("create=%+v", created)
	}
	var createResult map[any]any
	encoded, _ := cbor.Marshal(created.Result)
	if err := cbor.Unmarshal(encoded, &createResult); err != nil {
		t.Fatal(err)
	}
	resultSession, ok := createResult["session"].(map[any]any)
	if !ok {
		t.Fatalf("create result=%#v", createResult)
	}
	sessionID, ok := resultSession["id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("session id=%#v", resultSession["id"])
	}
	writeFrame(nativeClientMessage{Type: "request", ID: "prompt", Request: nativeCommand{Command: "prompt", SessionID: sessionID, Text: "hi"}})
	seenProgress := false
	for {
		message := read()
		if message.Type == "event" {
			seenProgress = true
			continue
		}
		if message.Type != "response" || message.ID != "prompt" {
			continue
		}
		if message.OK == nil || !*message.OK {
			t.Fatalf("prompt=%+v", message)
		}
		break
	}
	if !seenProgress {
		t.Fatal("expected session_progress events")
	}
	record, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Messages) != 2 || record.Messages[1].Content[0].Text != "streamed" {
		t.Fatalf("persisted messages=%+v", record.Messages)
	}
}
