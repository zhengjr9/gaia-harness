package agent

import (
	"context"
	"testing"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

type loopProvider struct{}

func (loopProvider) ID() string               { return "fake" }
func (loopProvider) Name() string             { return "fake" }
func (loopProvider) Models() []provider.Model { return []provider.Model{{ID: "m", Provider: "fake"}} }
func (loopProvider) Complete(_ context.Context, req provider.Request) (*provider.Response, error) {
	if len(req.Messages) == 1 {
		return &provider.Response{Provider: "fake", Model: "m", Content: []provider.Content{{ToolCall: &provider.ToolCall{ID: "1", Name: "echo", Arguments: `{"value":"ok"}`}}}}, nil
	}
	return &provider.Response{Provider: "fake", Model: "m", Content: []provider.Content{{Text: "finished"}}}, nil
}
func (p loopProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	response, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventStart, Response: response}
	ch <- provider.Event{Type: provider.EventDone, Response: response}
	close(ch)
	return ch, nil
}

type echoTool struct{}

func (echoTool) Definition() provider.Tool {
	return provider.Tool{Name: "echo", Parameters: map[string]any{"type": "object"}}
}
func (echoTool) Call(context.Context, string) (string, error) { return "tool-output", nil }

type countMiddleware struct{ before, after int }

func (m *countMiddleware) Before(context.Context, *provider.Request) error { m.before++; return nil }
func (m *countMiddleware) After(context.Context, *provider.Response) error { m.after++; return nil }
func TestCompleteRunsToolAndMiddleware(t *testing.T) {
	mw := &countMiddleware{}
	a, err := New(Config{Registry: provider.NewRegistry(loopProvider{}), Model: provider.Model{Provider: "fake", ID: "m"}, Tools: []Tool{echoTool{}}, Middleware: []Middleware{mw}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := a.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: []provider.Content{{Text: "run"}}}})
	if err != nil {
		t.Fatal(err)
	}
	res := run.Response
	if res.Content[0].Text != "finished" || mw.before != 2 || mw.after != 2 {
		t.Fatalf("response=%+v middleware=%d/%d", res, mw.before, mw.after)
	}
	if len(run.Messages) != 3 || run.Messages[1].Content[0].ToolResult == nil {
		t.Fatalf("missing persisted tool trace: %+v", run.Messages)
	}
}

func TestRunWithEventsReportsToolLifecycle(t *testing.T) {
	a, err := New(Config{Registry: provider.NewRegistry(loopProvider{}), Model: provider.Model{Provider: "fake", ID: "m"}, Tools: []Tool{echoTool{}}})
	if err != nil {
		t.Fatal(err)
	}
	events := []provider.EventType{}
	_, err = a.RunWithEvents(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: []provider.Content{{Text: "run"}}}}, func(event provider.Event) {
		events = append(events, event.Type)
	})
	if err != nil {
		t.Fatal(err)
	}
	toolStart, toolEnd := -1, -1
	for index, event := range events {
		if event == provider.EventToolStart {
			toolStart = index
		}
		if event == provider.EventToolEnd {
			toolEnd = index
		}
	}
	if toolStart < 0 || toolEnd <= toolStart {
		t.Fatalf("tool lifecycle events=%v", events)
	}
}

type requestCaptureProvider struct{ request provider.Request }

func (p *requestCaptureProvider) ID() string   { return "capture" }
func (p *requestCaptureProvider) Name() string { return "capture" }
func (p *requestCaptureProvider) Models() []provider.Model {
	return []provider.Model{{ID: "m", Provider: "capture", ContextWindow: 1000, MaxTokens: 100, Reasoning: true}}
}
func (p *requestCaptureProvider) Complete(_ context.Context, req provider.Request) (*provider.Response, error) {
	p.request = req
	return &provider.Response{Provider: "capture", Model: "m", Content: []provider.Content{{Text: "ok"}}}, nil
}
func (p *requestCaptureProvider) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	return nil, nil
}

func TestNewMergesModelMetadataAndReasoning(t *testing.T) {
	p := &requestCaptureProvider{}
	a, err := New(Config{
		Registry:      provider.NewRegistry(p),
		Model:         provider.Model{Provider: "capture", ID: "m"},
		ThinkingLevel: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: []provider.Content{{Text: "hi"}}}}); err != nil {
		t.Fatal(err)
	}
	if p.request.Reasoning != "high" || p.request.Model.ContextWindow != 1000 || p.request.Model.MaxTokens != 100 {
		t.Fatalf("request metadata not propagated: %+v", p.request)
	}
}
