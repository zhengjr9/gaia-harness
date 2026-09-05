package sandbox

import "testing"

func TestToolSchemasMatchOperations(t *testing.T) {
	want := map[string]string{"read_file": "path", "write_file": "path", "bash": "command", "python": "content"}
	for name, field := range want {
		tool := (Tool{Name: name}).Definition()
		properties := tool.Parameters["properties"].(map[string]any)
		if _, ok := properties[field]; !ok {
			t.Fatalf("%s missing %s property", name, field)
		}
	}
}
