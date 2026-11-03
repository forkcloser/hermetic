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
)

// RefusedPolicyError is the gate holding: the project's policy has no matching
// `policy allow` record on this machine. The cli answers it with the command
// that allows the policy, the whole of what a run says before it stops.
type RefusedPolicyError struct {
	// Policy is the policy's absolute path.
	Policy string
	// Denied is `policy deny` having recorded the human's choice: the
	// project's config is ignored, the root's pins answer, nothing is said.
	Denied bool
}

func (e *RefusedPolicyError) Error() string {
	return "the project policy is not allowed: " + e.Policy
}

// PolicyPath is the gate. Everything AQUA_POLICY_CONFIG names is trusted
// on sight by the library, and only a policy its own finder locates is validated
// against the record `policy allow` wrote; our walk replaces the finder, so the
// validation is done here, with the library's validator. The project's policy
// is returned, to join AQUA_POLICY_CONFIG, when its record under
// <aqua root>/policies matches byte for byte; a project without a policy has
// nothing to gate; otherwise the policy is refused, Denied when `policy deny`
// recorded the human's choice. A cloned repository thus cannot allow a registry
// of its choosing; `policy allow` is the one-time gate per machine. projectDir
// is absolute, as Find returns it.
func PolicyPath(aquaRoot, projectDir string) (string, error) {
	policyPath := filepath.Join(projectDir, Policy)

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
			Denied: denied(aquaRoot, policyPath),
		}
	}

	return policyPath, nil
}

// denied reports whether `policy deny` recorded the policy: a record under the
// library's policy-warnings, keyed by the policy's absolute path (on Windows
// the drive's colon becomes an underscore), as aqua keys it.
func denied(aquaRoot, policyPath string) bool {
	name := policyPath
	if runtime.GOOS == "windows" {
		name = strings.ReplaceAll(policyPath, ":", "_")
	}

	_, err := os.Stat(filepath.Join(aquaRoot, "policy-warnings", name))

	return err == nil
}
