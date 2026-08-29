package agent

import (
	"context"
	"fmt"
	provider "github.com/zhengjiarui/gaia-ai-provider"
	"time"
)

type Tool interface {
	Definition() provider.Tool
	Call(context.Context, string) (string, error)
}
type Middleware interface {
	Before(context.Context, *provider.Request) error
	After(context.Context, *provider.Response) error
}
type Config struct {
	Registry   *provider.Registry
	Model      provider.Model
	System     string
	Tools      []Tool
	Middleware []Middleware
	MaxTurns   int
	Retries    int
	RetryDelay time.Duration
}
type Agent struct {
	cfg   Config
	tools map[string]Tool
}

func New(cfg Config) (*Agent, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if cfg.Model.Provider == "" || cfg.Model.ID == "" {
		return nil, fmt.Errorf("model provider and id are required")
	}
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = 16
	}
	if cfg.Retries == 0 {
		cfg.Retries = 2
	}
	a := &Agent{cfg: cfg, tools: make(map[string]Tool)}
	for _, tool := range cfg.Tools {
		a.tools[tool.Definition().Name] = tool
	}
	return a, nil
}

func (a *Agent) Complete(ctx context.Context, messages []provider.Message) (*provider.Response, error) {
	for turn := 0; turn < a.cfg.MaxTurns; turn++ {
		req := provider.Request{Model: a.cfg.Model, System: a.cfg.System, Messages: messages, Tools: a.definitions()}
		for _, middleware := range a.cfg.Middleware {
			if err := middleware.Before(ctx, &req); err != nil {
				return nil, err
			}
		}
		res, err := a.completeWithRetry(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, middleware := range a.cfg.Middleware {
			if err := middleware.After(ctx, res); err != nil {
				return nil, err
			}
		}
		messages = append(messages, provider.Message{Role: provider.RoleAssistant, Content: res.Content})
		calls := toolCalls(res.Content)
		if len(calls) == 0 {
			return res, nil
		}
		for _, call := range calls {
			tool, ok := a.tools[call.Name]
			if !ok {
				messages = append(messages, toolMessage(call, "unknown tool: "+call.Name, true))
				continue
			}
			output, callErr := tool.Call(ctx, call.Arguments)
			if callErr != nil {
				output = callErr.Error()
			}
			messages = append(messages, toolMessage(call, output, callErr != nil))
		}
	}
	return nil, fmt.Errorf("agent exceeded max turns")
}

func (a *Agent) completeWithRetry(ctx context.Context, req provider.Request) (*provider.Response, error) {
	var err error
	for attempt := 0; attempt <= a.cfg.Retries; attempt++ {
		res, callErr := a.cfg.Registry.Complete(ctx, req)
		err = callErr
		if err == nil {
			return res, nil
		}
		if attempt < a.cfg.Retries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(a.cfg.RetryDelay):
			}
		}
	}
	return nil, err
}
func (a *Agent) definitions() []provider.Tool {
	out := make([]provider.Tool, 0, len(a.tools))
	for _, tool := range a.tools {
		out = append(out, tool.Definition())
	}
	return out
}
func toolCalls(content []provider.Content) []provider.ToolCall {
	out := []provider.ToolCall{}
	for _, item := range content {
		if item.ToolCall != nil {
			out = append(out, *item.ToolCall)
		}
	}
	return out
}
func toolMessage(call provider.ToolCall, output string, isError bool) provider.Message {
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: []provider.Content{{ToolResult: &provider.ToolResult{ToolCallID: call.ID, Content: output, IsError: isError}}}}
}
