package mcp

import (
	"context"
	"testing"
)

func TestStdioClientInitializesAndCallsTools(t *testing.T) {
	script := `while IFS= read -r line; do
case "$line" in
  *initialize*) echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}' ;;
  *tools/list*) echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"Echo input","inputSchema":{"type":"object"}}]}}' ;;
  *tools/call*) echo '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"ok"}]}}' ;;
esac
done`
	client, err := NewStdio(context.Background(), "sh", "-c", script)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools=%+v", tools)
	}
	result, err := client.Call(context.Background(), "echo", map[string]any{"value": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected call result")
	}
}
