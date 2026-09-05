package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalRejectsSymlinkEscapeAndCreatesNestedWrites(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	sb, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Read(context.Background(), "escape/secret.txt"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if err := os.Symlink(filepath.Join(outside, "write.txt"), filepath.Join(root, "write-link")); err != nil {
		t.Fatal(err)
	}
	if err := sb.Write(context.Background(), "write-link", []byte("must not escape")); err == nil {
		t.Fatal("expected symlink target write to be rejected")
	}
	if err := sb.Write(context.Background(), "nested/file.txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
}

func TestLocalCommandTimeout(t *testing.T) {
	sb, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = sb.Execute(context.Background(), Command{Program: "sh", Args: []string{"-c", "sleep 1"}, Timeout: 10 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
