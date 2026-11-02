package projectdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aquaproj/aqua/v2/pkg/config"
	"github.com/aquaproj/aqua/v2/pkg/policy"

	"github.com/forkcloser/hermetic/internal/rootdir"
)

// RefusedPolicyError is the gate holding: the project's policy has no matching
// `policy allow` record on this machine. Its text is the command that allows
// the policy, the whole of what a run says before it stops.
type RefusedPolicyError struct {
	// Policy is the policy's absolute path.
	Policy string
	// Denied is `policy deny` having recorded the human's choice: the
	// project's config is ignored, the root's pins answer, nothing is said.
	Denied bool
}

func (e *RefusedPolicyError) Error() string {
	return rootdir.Name + " policy allow " + e.Policy
}

// PolicyPath is the gate. Everything AQUA_POLICY_CONFIG names is trusted
// on sight by the library, and only a policy its own finder locates is validated
// against the record `policy allow` wrote; our walk replaces the finder, so the
// validation is done here, with the library's validator. The project's policy
// is returned, to join AQUA_POLICY_CONFIG, when its record under
// <aqua root>/policies matches byte for byte; a project without a policy has
// nothing to gate; otherwise the policy is refused, Denied when `policy deny`
// recorded the human's choice. A cloned repository thus cannot allow a registry
// of its choosing; `policy allow` is the one-time gate per machine.
func PolicyPath(aquaRoot, projectDir string) (string, error) {
	if projectDir == "" {
		return "", nil
	}

	policyPath, err := filepath.Abs(filepath.Join(projectDir, Policy))
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
		return "", &RefusedPolicyError{
			Policy: policyPath,
			Denied: recordExists(aquaRoot, "policy-warnings", policyPath),
		}
	}

	return policyPath, nil
}

// recordExists reports whether the library holds a record for the policy under
// the directory named in its root: `policies`, which `policy allow` writes, or
// `policy-warnings`, which `policy deny` writes. A record is keyed by the
// policy's normalized absolute path (on Windows the drive's colon becomes an
// underscore), as aqua keys it.
func recordExists(aquaRoot, dir, policyPath string) bool {
	name := policyPath
	if runtime.GOOS == "windows" {
		name = strings.ReplaceAll(policyPath, ":", "_")
	}

	_, err := os.Stat(filepath.Join(aquaRoot, dir, name))

	return err == nil
}
