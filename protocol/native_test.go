package protocol

import (
	"testing"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

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
