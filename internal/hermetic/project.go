package hermetic

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
	ProjectDir    = ".hermetic"
	ProjectConfig = "hermetic.yaml"
	ProjectPolicy = "policy.yaml"
)

// FindProject walks upward from wd for ProjectDir/ProjectConfig and returns the
// project directory's absolute path, "" when no directory up to the filesystem
// root carries one.
func FindProject(wd string) (string, error) {
	dir, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("resolve the working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, ProjectDir)

		_, err := os.Stat(filepath.Join(candidate, ProjectConfig))
		switch {
		case err == nil:
			return candidate, nil
		case errors.Is(err, fs.ErrNotExist):
		default:
			return "", fmt.Errorf("look for %s: %w", filepath.Join(candidate, ProjectConfig), err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}

		dir = parent
	}
}
