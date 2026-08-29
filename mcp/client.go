package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type Tool struct {
	Name, Description string
	InputSchema       map[string]any
}
type Client interface {
	ListTools(context.Context) ([]Tool, error)
	Call(context.Context, string, map[string]any) (any, error)
	Close() error
}
type StdioClient struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	mu   sync.Mutex
	next int
}

func NewStdio(ctx context.Context, command string, args ...string) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	return &StdioClient{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}
func (c *StdioClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	body, _ := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", id, method, params})
	if _, err := c.in.Write(append(body, '\n')); err != nil {
		return nil, err
	}
	type response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	for {
		lineCh := make(chan []byte, 1)
		errCh := make(chan error, 1)
		go func() {
			line, err := c.out.ReadBytes('\n')
			if err != nil {
				errCh <- err
			} else {
				lineCh <- line
			}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			return nil, err
		case line := <-lineCh:
			var r response
			if err := json.Unmarshal(line, &r); err != nil {
				continue
			}
			if r.Error != nil {
				return nil, fmt.Errorf("mcp: %s", r.Error.Message)
			}
			return r.Result, nil
		}
	}
}
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var v struct {
		Tools []Tool `json:"tools"`
	}
	err = json.Unmarshal(raw, &v)
	return v.Tools, err
}
func (c *StdioClient) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	raw, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var v any
	err = json.Unmarshal(raw, &v)
	return v, err
}
func (c *StdioClient) Close() error { return c.cmd.Process.Kill() }
