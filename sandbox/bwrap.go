package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Bwrap struct{ cfg Config }

func NewBwrap(cfg Config) (*Bwrap, error) {
	if cfg.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if err := os.MkdirAll(cfg.Workspace, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	cfg.Workspace = resolved
	if cfg.BwrapPath == "" {
		cfg.BwrapPath = "bwrap"
	}
	return &Bwrap{cfg: cfg}, nil
}
func (b *Bwrap) Workspace() string { return b.cfg.Workspace }
func (b *Bwrap) Read(ctx context.Context, path string) ([]byte, error) {
	p, err := securePath(b.cfg.Workspace, path, false)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}
func (b *Bwrap) Write(ctx context.Context, path string, data []byte) error {
	p, err := securePath(b.cfg.Workspace, path, true)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}
func (b *Bwrap) Execute(ctx context.Context, c Command) (Result, error) {
	if len(c.Program) == 0 {
		return Result{}, fmt.Errorf("program is required")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = b.cfg.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	workdir := "/workspace"
	if c.Dir != "" {
		resolved, err := securePath(b.cfg.Workspace, c.Dir, false)
		if err != nil {
			return Result{}, err
		}
		rel, err := filepath.Rel(b.cfg.Workspace, resolved)
		if err != nil || rel == ".." || len(rel) >= 2 && rel[:2] == ".." {
			return Result{}, fmt.Errorf("command directory escapes workspace: %s", c.Dir)
		}
		if rel != "." {
			workdir = filepath.Join("/workspace", rel)
		}
	}
	args := []string{"--die-with-parent", "--unshare-pid", "--ro-bind", "/usr", "/usr", "--ro-bind", "/bin", "/bin", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64", "--ro-bind", "/sbin", "/sbin", "--ro-bind", "/etc", "/etc", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--bind", b.cfg.Workspace, "/workspace", "--chdir", workdir}
	if !b.cfg.Network {
		args = append(args, "--unshare-net")
	}
	args = append(args, c.Program)
	args = append(args, c.Args...)
	cmd := exec.CommandContext(ctx, b.cfg.BwrapPath, args...)
	cmd.Stdin = c.Stdin
	cmd.Env = append(os.Environ(), "HOME=/workspace", "PATH=/usr/local/bin:/usr/bin:/bin")
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errOut.String(), ExitCode: 0}
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			res.ExitCode = e.ExitCode()
		}
		return res, err
	}
	return res, nil
}
func (b *Bwrap) Python(ctx context.Context, code string, args []string) (Result, error) {
	return b.Execute(ctx, Command{Program: "python3", Args: append([]string{"-c", code}, args...)})
}
func (b *Bwrap) ListSkills(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(b.cfg.Workspace, "skills"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
