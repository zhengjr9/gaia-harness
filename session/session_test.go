package session

import (
	"context"
	"testing"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

func TestMemoryStoreAndCompression(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	r := Record{ID: "s1", WorkspaceID: "w1", Messages: []provider.Message{{Role: provider.RoleUser}}}
	if err := (Service{Store: store}).Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := (Service{Store: store}).Append(ctx, "s1", provider.Message{Role: provider.RoleAssistant}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages=%d", len(got.Messages))
	}
}
