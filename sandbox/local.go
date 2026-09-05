package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Local is a development fallback for hosts without bubblewrap (for example macOS).
// Production Linux sessions should use Bwrap.
type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}
func (l *Local) Workspace() string { return l.root }
func (l *Local) Read(_ context.Context, p string) ([]byte, error) {
	full, err := l.safePath(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}
func (l *Local) Write(_ context.Context, p string, b []byte) error {
	full, err := l.safePath(p)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		return err
	}
	return os.WriteFile(full, b, 0600)
}
func (l *Local) Execute(ctx context.Context, c Command) (Result, error) {
	if c.Dir != "" {
		var err error
		c.Dir, err = l.safePath(c.Dir)
		if err != nil {
			return Result{}, err
		}
	} else {
		c.Dir = l.root
	}
	cmd := exec.CommandContext(ctx, c.Program, c.Args...)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	cmd.Env = append(os.Environ(), "HOME="+l.root)
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: stderr.String()}
	if e, ok := err.(*exec.ExitError); ok {
		res.ExitCode = e.ExitCode()
	}
	return res, err
}
func (l *Local) Python(ctx context.Context, code string, args []string) (Result, error) {
	return l.Execute(ctx, Command{Program: "python3", Args: append([]string{"-c", code}, args...)})
}
func (l *Local) ListSkills(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(l.root, "skills"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
func (l *Local) safePath(p string) (string, error) {
	clean := filepath.Clean("/" + p)
	full := filepath.Join(l.root, strings.TrimPrefix(clean, "/"))
	rel, err := filepath.Rel(l.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return full, nil
}
