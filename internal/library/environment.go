package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/forkcloser/hermetic/internal/rootdir"
)

// scrubbedPrefixes name what the library's run never inherits from the shell:
// aqua's own variables (it reads AQUA_GITHUB_TOKEN and the AQUA_GOOS family
// itself), and everything cosign reads that steers trust (its TUF mirror and
// roots, the SIGSTORE_* key and root overrides, its own knobs) or names a CI or
// cloud identity it has no business holding during a verification. The tool a
// dispatch runs gets the shell's values back.
//
//nolint:gochecknoglobals // immutable data.
var scrubbedPrefixes = []string{
	"AQUA_", "COSIGN_", "SIGSTORE_", "TUF_", "GITLAB_", "BUILDKITE_", "ACTIONS_ID_TOKEN_REQUEST_",
}

// scrubbedNames are the exact names of the same kind.
//
//nolint:gochecknoglobals // immutable data.
var scrubbedNames = map[string]bool{
	"GITHUB_TOKEN": true, "GITHUB_HOST": true, "SPIFFE_ENDPOINT_SOCKET": true, "GOOGLE_SERVICE_ACCOUNT_NAME": true,
	// gh reads these ahead of GITHUB_TOKEN, and aqua adds GITHUB_TOKEN for its
	// `gh attestation verify` only when neither is set; so a CI shell's GH_TOKEN
	// would be the verifier's credential. GH_CONFIG_DIR stays: a laptop's gh
	// login is the right fallback when no token of ours is set.
	"GH_TOKEN": true, "GH_ENTERPRISE_TOKEN": true,
}

// setEnvironment makes the process's environment the one the library and the
// verifiers it runs inherit: environ, the shell's, minus what is scrubbed, plus
// the verifiers' TUF cache in the root and the token under the two names its
// readers take, aqua's and gh's. Nothing else: the library takes the rest of
// a run from its parameters.
func setEnvironment(root rootdir.Root, token string, environ []string) error {
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if !scrubbed(key) {
			continue
		}

		if err := os.Unsetenv(key); err != nil {
			return fmt.Errorf("unset %s: %w", key, err)
		}
	}

	set := map[string]string{"TUF_ROOT": filepath.Join(string(root), "sigstore")}
	if token != "" {
		set["AQUA_GITHUB_TOKEN"] = token
		set["GITHUB_TOKEN"] = token
	}

	for key, value := range set {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}

	return nil
}

func scrubbed(key string) bool {
	if scrubbedNames[key] {
		return true
	}

	for _, prefix := range scrubbedPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}
