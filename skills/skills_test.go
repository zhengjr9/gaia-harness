package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemRejectsSkillSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "escape"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "SKILL.md"), filepath.Join(root, "escape", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	loaded, err := (Filesystem{Root: root}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded escaped skill: %+v", loaded)
	}
}
