package hermetic

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aquaproj/aqua/v2/pkg/config"
	"github.com/aquaproj/aqua/v2/pkg/policy"
)

// ProjectPolicyPath is the gate. Everything AQUA_POLICY_CONFIG names is trusted
// on sight by the library, and only a policy its own finder locates is validated
// against the record `policy allow` wrote; our walk replaces the finder, so the
// validation is done here, with the library's validator: the project's policy
// is returned, to join AQUA_POLICY_CONFIG, only when its record under
// <aqua root>/policies matches byte for byte, and is dropped with the library's
// own warning when the record is missing or stale. A cloned repository thus
// cannot allow a registry of its choosing; `policy allow` stays the one-time
// gate per machine.
func ProjectPolicyPath(logger *slog.Logger, aquaRoot, projectDir string) (string, error) {
	if projectDir == "" {
		return "", nil
	}

	policyPath, err := filepath.Abs(filepath.Join(projectDir, ProjectPolicy))
	if err != nil {
		return "", fmt.Errorf("resolve the project policy: %w", err)
	}

	if _, err := os.Stat(policyPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("look for the project policy: %w", err)
	}

	validator := policy.NewValidator(&config.Param{RootDir: aquaRoot})
	if err := validator.Validate(policyPath); err != nil {
		// The library's errors are unexported; its reader tells "updated" from
		// "not found" the same way, by the record's presence.
		updated := recordExists(aquaRoot, policyPath)
		if werr := validator.Warn(logger, policyPath, updated); werr != nil {
			return "", fmt.Errorf("warn about the project policy: %w", werr)
		}

		return "", nil
	}

	return policyPath, nil
}

// recordExists reports whether `policy allow` recorded the policy: the record is
// its copy under <aqua root>/policies at the normalized absolute path (on
// Windows the drive's colon becomes an underscore), as aqua keys it.
func recordExists(aquaRoot, policyPath string) bool {
	name := policyPath
	if runtime.GOOS == windows {
		name = strings.ReplaceAll(policyPath, ":", "_")
	}

	_, err := os.Stat(filepath.Join(aquaRoot, "policies", name))

	return err == nil
}
