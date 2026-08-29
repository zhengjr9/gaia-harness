package skills

import (
	"context"
	"os"
	"path/filepath"
)

type Skill struct{ Name, Description, Path, Instructions string }
type Loader interface {
	Load(context.Context, string) (Skill, error)
	List(context.Context) ([]Skill, error)
}
type Filesystem struct{ Root string }

func (f Filesystem) List(_ context.Context) ([]Skill, error) {
	entries, err := os.ReadDir(f.Root)
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
		data, err := os.ReadFile(filepath.Join(f.Root, e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: e.Name(), Path: filepath.Join(f.Root, e.Name()), Instructions: string(data)})
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
