package hermetic

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The library's own variables that hermetic sets. Nothing named AQUA_* reaches
// the library from the environment.
const (
	aquaPrefix          = "AQUA_"
	ourPrefix           = "HERMETIC_"
	aquaRootEnv         = aquaPrefix + "ROOT_DIR"
	aquaGlobalConfigEnv = aquaPrefix + "GLOBAL_CONFIG"
	aquaPolicyConfigEnv = aquaPrefix + "POLICY_CONFIG"
	aquaConfigEnv       = aquaPrefix + "CONFIG"
	aquaTokenEnv        = aquaPrefix + "GITHUB_TOKEN"
	aquaLogLevelEnv     = aquaPrefix + "LOG_LEVEL"
	githubTokenEnv      = "GITHUB_TOKEN" // #nosec G101 -- a variable's name, not a credential.
	tufRootEnv          = "TUF_ROOT"

	// ConfigEnv names the project config outright, like -c; the walk stays out,
	// and the named config carries its project: the policy beside it, through
	// the gate, as a walked one does.
	ConfigEnv = ourPrefix + "CONFIG"
	// NoGlobalConfigEnv, true, gives the run no global config: the recipe that
	// runs on a project's own pins alone sets it.
	NoGlobalConfigEnv = ourPrefix + "NO_GLOBAL_CONFIG"
	// TokenEnv is the GitHub token the library and the verifiers use: the one
	// token read, the shell's GITHUB_TOKEN never.
	TokenEnv = ourPrefix + "GITHUB_TOKEN"
	// LogLevelEnv is the library's log level (debug, info, warn, error).
	LogLevelEnv = ourPrefix + "LOG_LEVEL"
)

// known is every HERMETIC_* variable the binary reads; any other is refused,
// so a misspelling or a knob the binary does not expose fails loud rather
// than silently doing nothing.
//
//nolint:gochecknoglobals // immutable data.
var known = map[string]bool{
	RootEnv: true, ConfigEnv: true, NoGlobalConfigEnv: true, TokenEnv: true, LogLevelEnv: true,
}

// pinned are the library's knobs its settings file would otherwise decide,
// each set to the one value the toolset runs with: the policy gate and the
// lazy install on, checksums verified and required whatever a config says,
// the vacuum's use tracking off (vacuum is not exposed), no progress bar, and
// the library's own defaults for parallelism and log colour. The environment
// beats the file in the library, so with these the file has nothing left to
// say.
//
//nolint:gochecknoglobals // immutable data.
var pinned = map[string]string{
	aquaPrefix + "DISABLE_POLICY":           off,
	aquaPrefix + "DISABLE_LAZY_INSTALL":     off,
	aquaPrefix + "CHECKSUM":                 on,
	aquaPrefix + "REQUIRE_CHECKSUM":         on,
	aquaPrefix + "ENFORCE_CHECKSUM":         on,
	aquaPrefix + "ENFORCE_REQUIRE_CHECKSUM": on,
	aquaPrefix + "DISABLE_TRACKING":         on,
	aquaPrefix + "PROGRESS_BAR":             off,
	aquaPrefix + "MAX_PARALLELISM":          "5",
	aquaPrefix + "LOG_COLOR":                "auto",
}

// The library parses its booleans with strconv.ParseBool.
const (
	on  = "true"
	off = "false"
)

// defaultLogLevel is the library's own, set outright so the settings file's
// log level never applies.
const defaultLogLevel = "info"

// scrubbedPrefixes name what the library's run never inherits from the shell:
// aqua's own variables, and everything cosign reads that steers trust (its
// TUF mirror and roots, the SIGSTORE_* key and root overrides, its own knobs)
// or names a CI or cloud identity it has no business holding during a
// verification. The tool a dispatch runs gets the shell's values back.
//
//nolint:gochecknoglobals // immutable data.
var scrubbedPrefixes = []string{
	aquaPrefix, "COSIGN_", "SIGSTORE_", "TUF_", "GITLAB_", "BUILDKITE_", "ACTIONS_ID_TOKEN_REQUEST_",
}

// scrubbedNames are the exact names of the same kind.
//
//nolint:gochecknoglobals // immutable data.
var scrubbedNames = map[string]bool{
	githubTokenEnv: true, "GITHUB_HOST": true, "SPIFFE_ENDPOINT_SOCKET": true, "GOOGLE_SERVICE_ACCOUNT_NAME": true,
	// gh reads these ahead of GITHUB_TOKEN, and aqua adds GITHUB_TOKEN for its
	// `gh attestation verify` only when neither is set; so a CI shell's GH_TOKEN
	// would be the verifier's credential. GH_CONFIG_DIR stays: a laptop's gh
	// login is the right fallback when no token of ours is set.
	"GH_TOKEN": true, "GH_ENTERPRISE_TOKEN": true,
}

var (
	errUnknownVariable = errors.New("unknown " + ourPrefix + "* variable")
	errNotBoolean      = errors.New(NoGlobalConfigEnv + " is not a boolean")
)

// noGlobalConfig is a value that names no file: aqua parses the list and skips
// its empty entries. Never an empty value, which lets the human's aqua settings
// file (`global_config` in ~/.config/aquaproj-aqua/config.yaml) fall through.
func noGlobalConfig() string { return string(os.PathListSeparator) }

// LibraryEnvironment is the environment the library runs in, for its own
// subcommands and for the resolution half of a dispatch: the shell's, minus
// what is scrubbed (see scrubbedPrefixes), plus ours. aqua's three paths
// derive from the root; the policies are the root's, when the root holds one,
// then the project's, when one passed the gate; the token, the log level and a config path come
// only from their HERMETIC_ counterparts; the verifiers' TUF cache lives in
// the root. set holds what is set, unset what is removed; a HERMETIC_*
// variable the binary does not know is an error.
func LibraryEnvironment(
	root Root,
	environ []string,
	projectPolicy string,
) (set map[string]string, unset []string, err error) {
	set = map[string]string{}
	ours := map[string]string{}

	for _, kv := range environ {
		key, value, _ := strings.Cut(kv, "=")

		switch {
		case strings.HasPrefix(key, ourPrefix):
			if !known[key] {
				return nil, nil, fmt.Errorf("%w: %s", errUnknownVariable, key)
			}

			ours[key] = value
		case scrubbed(key):
			unset = append(unset, key)
		}
	}

	global := root.GlobalConfig()

	if v := ours[NoGlobalConfigEnv]; v != "" {
		noGlobal, parseErr := strconv.ParseBool(v)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("%w: %q", errNotBoolean, v)
		}

		if noGlobal {
			global = noGlobalConfig()
		}
	}

	maps.Copy(set, pinned)

	set[aquaRootEnv] = root.AquaRoot()
	set[aquaGlobalConfigEnv] = global
	set[aquaPolicyConfigEnv] = policyList(root, projectPolicy)
	set[tufRootEnv] = filepath.Join(string(root), "sigstore")
	set[aquaLogLevelEnv] = defaultLogLevel

	if v := ours[ConfigEnv]; v != "" {
		set[aquaConfigEnv] = v
	}

	if v := ours[TokenEnv]; v != "" {
		set[aquaTokenEnv] = v
		set[githubTokenEnv] = v
	}

	if v := ours[LogLevelEnv]; v != "" {
		set[aquaLogLevelEnv] = v
	}

	sort.Strings(unset)

	return set, unset, nil
}

// ToolEnvironment is the environment a dispatched tool runs in: the shell's,
// minus every AQUA_* variable. Nothing of ours is added: the toolset is reached
// through hermetic, and a stock aqua under the tool is the human's own.
func ToolEnvironment(environ []string) []string {
	out := make([]string, 0, len(environ))

	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, aquaPrefix) {
			continue
		}

		out = append(out, entry)
	}

	return out
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

// policyList is what AQUA_POLICY_CONFIG names: the root's policy, the
// toolset's own and trusted on sight, when the root holds one, then the
// project's when one passed the gate. A root with no toolset in it, a
// runner's or a fresh session's, has no policy of its own, and the library
// refuses a listed file that is not there; so the list is of what exists,
// never of what a root should have.
func policyList(root Root, projectPolicy string) string {
	policies := make([]string, 0, 2)

	if _, statErr := os.Stat(root.Policy()); statErr == nil {
		policies = append(policies, root.Policy())
	}

	if projectPolicy != "" {
		policies = append(policies, projectPolicy)
	}

	return strings.Join(policies, string(os.PathListSeparator))
}
