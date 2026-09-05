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

// EventObserver receives provider stream events while a turn is running.
// A nil observer keeps the synchronous completion path for callers that only
// need the final response.
type EventObserver func(provider.Event)
type Agent struct {
	cfg   Config
	tools map[string]Tool
}

type RunResult struct {
	Response *provider.Response
	Messages []provider.Message
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
	result, err := a.Run(ctx, messages)
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

// Run executes a turn loop and returns every generated assistant/tool message.
// Persisting this trace is required for correct continuation after a restart.
func (a *Agent) Run(ctx context.Context, messages []provider.Message) (RunResult, error) {
	return a.RunWithEvents(ctx, messages, nil)
}

// RunWithEvents executes the same durable tool loop as Run and optionally
// exposes each provider stream event before the final trace is persisted.
func (a *Agent) RunWithEvents(ctx context.Context, messages []provider.Message, observer EventObserver) (RunResult, error) {
	trace := []provider.Message{}
	for turn := 0; turn < a.cfg.MaxTurns; turn++ {
		req := provider.Request{Model: a.cfg.Model, System: a.cfg.System, Messages: messages, Tools: a.definitions()}
		for _, middleware := range a.cfg.Middleware {
			if err := middleware.Before(ctx, &req); err != nil {
				return RunResult{}, err
			}
		}
		res, err := a.completeWithRetry(ctx, req, observer)
		if err != nil {
			return RunResult{}, err
		}
		for _, middleware := range a.cfg.Middleware {
			if err := middleware.After(ctx, res); err != nil {
				return RunResult{}, err
			}
		}
		assistant := provider.Message{Role: provider.RoleAssistant, Content: res.Content}
		messages = append(messages, assistant)
		trace = append(trace, assistant)
		calls := toolCalls(res.Content)
		if len(calls) == 0 {
			return RunResult{Response: res, Messages: trace}, nil
		}
		for _, call := range calls {
			tool, ok := a.tools[call.Name]
			if !ok {
				message := toolMessage(call, "unknown tool: "+call.Name, true)
				messages = append(messages, message)
				trace = append(trace, message)
				continue
			}
			output, callErr := tool.Call(ctx, call.Arguments)
			if callErr != nil {
				output = callErr.Error()
			}
			message := toolMessage(call, output, callErr != nil)
			messages = append(messages, message)
			trace = append(trace, message)
		}
	}
	return RunResult{Messages: trace}, fmt.Errorf("agent exceeded max turns")
}

func (a *Agent) completeWithRetry(ctx context.Context, req provider.Request, observer EventObserver) (*provider.Response, error) {
	var err error
	for attempt := 0; attempt <= a.cfg.Retries; attempt++ {
		res, callErr := a.complete(ctx, req, observer)
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

func (a *Agent) complete(ctx context.Context, req provider.Request, observer EventObserver) (*provider.Response, error) {
	if observer == nil {
		return a.cfg.Registry.Complete(ctx, req)
	}
	providerClient, ok := a.cfg.Registry.Get(req.Model.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", req.Model.Provider)
	}
	events, err := providerClient.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var response *provider.Response
	for event := range events {
		observer(event)
		if event.Response != nil && (event.Type == provider.EventDone || event.Type == provider.EventError) {
			response = event.Response
		}
	}
	if response == nil {
		return nil, fmt.Errorf("provider stream ended without a response")
	}
	if response.Error != nil || response.StopReason == provider.StopReasonError {
		if response.Error != nil {
			return nil, response.Error
		}
		return nil, fmt.Errorf("provider stream failed")
	}
	return response, nil
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
