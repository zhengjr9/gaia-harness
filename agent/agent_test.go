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
func (loopProvider) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	return nil, nil
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
