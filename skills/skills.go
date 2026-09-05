package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
)

type Skill struct{ Name, Description, Path, Instructions string }
type Loader interface {
	Load(context.Context, string) (Skill, error)
	List(context.Context) ([]Skill, error)
}
type Filesystem struct{ Root string }

func (f Filesystem) List(_ context.Context) ([]Skill, error) {
	root, err := filepath.EvalSymlinks(f.Root)
	if os.IsNotExist(err) {
		return []Skill{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []Skill{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Skill{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name(), "SKILL.md")
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: e.Name(), Path: filepath.Join(root, e.Name()), Instructions: string(data)})
	}
	return out, nil
}
func (f Filesystem) Load(ctx context.Context, name string) (Skill, error) {
	all, err := f.List(ctx)
	if err != nil {
		return Skill{}, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
	}
	return Skill{}, os.ErrNotExist
}

// Tools exposes skills through the same tool interface as the sandbox. The
// agent can discover a skill first and load its instructions only when needed.
type Tool struct {
	Loader Loader
	Name   string
}

func (t Tool) Definition() provider.Tool {
	if t.Name == "list_skills" {
		return provider.Tool{Name: t.Name, Description: "List skills available in the session workspace.", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}
	}
	return provider.Tool{
		Name:        t.Name,
		Description: "Load the instructions for a named skill from the session workspace.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
	}
}

func (t Tool) Call(ctx context.Context, arguments string) (string, error) {
	if t.Name == "list_skills" {
		items, err := t.Loader.List(ctx)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(items)
		return string(data), err
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", err
	}
	if input.Name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	skill, err := t.Loader.Load(ctx, input.Name)
	if err != nil {
		return "", err
	}
	return skill.Instructions, nil
}

func Tools(loader Loader) []agent.Tool {
	return []agent.Tool{Tool{Loader: loader, Name: "list_skills"}, Tool{Loader: loader, Name: "load_skill"}}
}
