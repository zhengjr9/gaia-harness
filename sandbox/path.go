package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// securePath resolves existing symlinks while keeping the result below root.
// For writes the target may not exist, so its parent is resolved first.
func securePath(root, path string, forWrite bool) (string, error) {
	clean := filepath.Clean("/" + path)
	target := filepath.Join(root, strings.TrimPrefix(clean, "/"))
	check := target
	existingTarget := false
	if forWrite {
		if _, statErr := os.Lstat(target); statErr == nil {
			check = target
			existingTarget = true
		} else {
			check = filepath.Dir(target)
		}
	}
	missing := []string{}
	if forWrite && !existingTarget {
		for {
			if _, statErr := os.Lstat(check); statErr == nil {
				break
			} else if !os.IsNotExist(statErr) {
				return "", statErr
			}
			missing = append(missing, filepath.Base(check))
			parent := filepath.Dir(check)
			if parent == check {
				return "", fmt.Errorf("workspace parent does not exist: %s", path)
			}
			check = parent
		}
	}
	resolved, err := filepath.EvalSymlinks(check)
	if err != nil {
		return "", err
	}
	if forWrite {
		if existingTarget {
			target = resolved
		} else {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			target = filepath.Join(resolved, filepath.Base(target))
		}
	} else {
		target = resolved
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}
	return target, nil
}
