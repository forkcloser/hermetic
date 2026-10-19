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
// <aqua root>/policies matches byte for byte, and is dropped with a warning
// when the record is missing or stale, unless `policy deny` recorded the
// human's choice to ignore it. A cloned repository thus cannot allow a registry
// of its choosing; `policy allow` stays the one-time gate per machine.
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
		warnUnallowed(logger, aquaRoot, policyPath, recordExists(aquaRoot, "policies", policyPath))

		return "", nil //nolint:nilerr // the validator's error is the gate holding: the policy is dropped, warned about, and the run goes on.
	}

	return policyPath, nil
}

// warnUnallowed says the project's policy is ignored and how to allow or deny
// it, by this binary's commands. The library's own warning names aqua's, which
// a hermetic machine does not have. Silent when `policy deny` recorded the
// human's choice, the way the library's warning is.
func warnUnallowed(logger *slog.Logger, aquaRoot, policyPath string, updated bool) {
	if recordExists(aquaRoot, "policy-warnings", policyPath) {
		return
	}

	how := []any{
		"policy", policyPath,
		"allow", Name + " policy allow " + policyPath,
		"deny", Name + " policy deny " + policyPath,
	}

	if updated {
		logger.Warn("the project policy changed since it was allowed and is ignored until allowed again", how...)

		return
	}

	logger.Warn("the project policy is ignored until allowed", how...)
}

// recordExists reports whether the library holds a record for the policy under
// the directory named in its root: `policies`, which `policy allow` writes, or
// `policy-warnings`, which `policy deny` writes. A record is keyed by the
// policy's normalized absolute path (on Windows the drive's colon becomes an
// underscore), as aqua keys it.
func recordExists(aquaRoot, dir, policyPath string) bool {
	name := policyPath
	if runtime.GOOS == windows {
		name = strings.ReplaceAll(policyPath, ":", "_")
	}

	_, err := os.Stat(filepath.Join(aquaRoot, dir, name))

	return err == nil
}
