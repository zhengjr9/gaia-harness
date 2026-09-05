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

func TestCompressionAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	compressor := TokenCompressor{ReserveOutput: 8, SummaryPrefix: "compacted"}
	service := Service{Store: store, Compressor: compressor}
	for _, id := range []string{"a", "b"} {
		if err := service.Create(ctx, Record{ID: id, WorkspaceID: id, Model: provider.Model{ContextWindow: 20}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		if err := service.Append(ctx, "a", provider.Message{Role: provider.RoleUser, Content: []provider.Content{{Text: "0123456789"}}}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := store.Get(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Get(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Messages) >= 8 || len(b.Messages) != 0 {
		t.Fatalf("compression/isolation failed: a=%d b=%d", len(a.Messages), len(b.Messages))
	}
}

func TestWorkspaceIDCannotEscapeRoot(t *testing.T) {
	err := (Service{Store: NewMemoryStore()}).Create(context.Background(), Record{ID: "x", WorkspaceID: "../outside"})
	if err == nil {
		t.Fatal("expected workspace traversal to be rejected")
	}
}
