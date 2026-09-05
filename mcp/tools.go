package mcp

import (
	"context"
	"encoding/json"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
)

type ToolAdapter struct {
	Client Client
	Spec   Tool
}

func (t ToolAdapter) Definition() provider.Tool {
	return provider.Tool{Name: t.Spec.Name, Description: t.Spec.Description, Parameters: t.Spec.InputSchema}
}
func (t ToolAdapter) Call(ctx context.Context, arguments string) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", err
	}
	result, err := t.Client.Call(ctx, t.Spec.Name, args)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	return string(data), err
}
func AgentTools(ctx context.Context, c Client) ([]agent.Tool, error) {
	specs, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]agent.Tool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, ToolAdapter{Client: c, Spec: spec})
	}
	return out, nil
}
