package projectdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The project directory: a project's pins and policy, under our name, which
// aqua's own finder does not look for; the binary finds it and names the config
// to the library through AQUA_CONFIG.
const (
	Dir    = ".hermetic"
	Config = "hermetic.yaml"
	Policy = "policy.yaml"
)

// Find walks upward from wd for Dir/Config and returns the project directory's
// absolute path; "" when there is none. The walk stops at the repository
// boundary, a directory holding .git (a directory, or the file a worktree or a
// submodule has): a repository's project is its own manifest or none, never an
// enclosing repository's.
func Find(wd string) (string, error) {
	dir, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("resolve the working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, Dir)

		found, err := exists(filepath.Join(candidate, Config))
		if err != nil {
			return "", err
		}

		if found {
			return candidate, nil
		}

		boundary, err := exists(filepath.Join(dir, ".git"))
		if err != nil || boundary {
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}

		dir = parent
	}
}

// exists reports whether path names anything; an error other than its absence
// is returned, since a walk cannot know what it did not see.
func exists(path string) (bool, error) {
	_, err := os.Stat(path)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("look for %s: %w", path, err)
	}
}
