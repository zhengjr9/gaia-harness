package sandbox

import (
	"context"
	"io"
	"time"
)

type Config struct {
	Workspace string
	BwrapPath string
	Network   bool
	Timeout   time.Duration
}
type Command struct {
	Program string
	Args    []string
	Env     map[string]string
	Dir     string
	Stdin   io.Reader
	Timeout time.Duration
}
type Result struct {
	ExitCode       int
	Stdout, Stderr string
}

type Sandbox interface {
	Workspace() string
	Read(context.Context, string) ([]byte, error)
	Write(context.Context, string, []byte) error
	Execute(context.Context, Command) (Result, error)
	Python(context.Context, string, []string) (Result, error)
	ListSkills(context.Context) ([]string, error)
}
