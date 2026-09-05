package protocol

import (
	"testing"

	"github.com/fxamacker/cbor/v2"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

func TestNativeToolTranscriptAlwaysIncludesIsError(t *testing.T) {
	item := nativeTranscriptItem{
		ID:         "tool-1",
		Role:       "tool",
		ToolCallID: "call-1",
		ToolName:   "write_file",
		Input:      map[string]any{"path": "hello.txt"},
		Content:    []map[string]any{{"type": "text", "text": "ok"}},
		Status:     "complete",
		Timestamp:  1,
	}

	encoded, err := cbor.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := cbor.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	value, ok := decoded["isError"]
	if !ok {
		t.Fatal("tool transcript item omitted required isError field")
	}
	if value != false {
		t.Fatalf("isError=%v, want false", value)
	}
}

func TestNativeSnapshotNormalizesModelMetadata(t *testing.T) {
	snapshot := nativeSnapshot(ServerSnapshot{Models: []provider.Model{{
		Provider: "test",
		ID:       "model",
		Name:     "model",
		Cost:     provider.Cost{Input: -1, Output: -2, CacheRead: -3, CacheWrite: -4},
	}}})
	model := snapshot.Models[0]
	if model.ContextWindow != 128000 || model.MaxTokens != 8192 {
		t.Fatalf("unexpected fallback limits: %+v", model)
	}
	for name, value := range model.Cost {
		if value < 0 {
			t.Fatalf("negative %s cost: %v", name, value)
		}
	}
}
