package sandbox

import (
	"context"
	"encoding/json"
	"fmt"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
)

type Tool struct {
	Sandbox Sandbox
	Name    string
}

func (t Tool) Definition() provider.Tool {
	properties := map[string]any{}
	required := []string{}
	description := "Execute an operation inside the session workspace."
	switch t.Name {
	case "read_file":
		description = "Read a UTF-8 file from the session workspace."
		properties["path"] = map[string]any{"type": "string"}
		required = []string{"path"}
	case "write_file":
		description = "Write UTF-8 content to a file in the session workspace."
		properties["path"] = map[string]any{"type": "string"}
		properties["content"] = map[string]any{"type": "string"}
		required = []string{"path", "content"}
	case "bash":
		description = "Run a shell command in the session workspace."
		properties["command"] = map[string]any{"type": "string"}
		required = []string{"command"}
	case "python":
		description = "Run Python 3 code in the session workspace."
		properties["content"] = map[string]any{"type": "string"}
		required = []string{"content"}
	}
	return provider.Tool{Name: t.Name, Description: description, Parameters: map[string]any{"type": "object", "properties": properties, "required": required}}
}
func (t Tool) Call(ctx context.Context, arguments string) (string, error) {
	var v struct{ Path, Content, Command string }
	if err := json.Unmarshal([]byte(arguments), &v); err != nil {
		return "", err
	}
	switch t.Name {
	case "read_file":
		data, err := t.Sandbox.Read(ctx, v.Path)
		return string(data), err
	case "write_file":
		return "", t.Sandbox.Write(ctx, v.Path, []byte(v.Content))
	case "bash":
		res, err := t.Sandbox.Execute(ctx, Command{Program: "/bin/sh", Args: []string{"-lc", v.Command}})
		return res.Stdout + res.Stderr, err
	case "python":
		res, err := t.Sandbox.Python(ctx, v.Content, nil)
		return res.Stdout + res.Stderr, err
	default:
		return "", fmt.Errorf("unknown sandbox tool %q", t.Name)
	}
}
func Tools(s Sandbox) []agent.Tool {
	return []agent.Tool{Tool{Sandbox: s, Name: "read_file"}, Tool{Sandbox: s, Name: "write_file"}, Tool{Sandbox: s, Name: "bash"}, Tool{Sandbox: s, Name: "python"}}
}
