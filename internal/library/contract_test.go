package library_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/forkcloser/hermetic/internal/library"
	"github.com/forkcloser/hermetic/internal/rootdir"
)

// The library's contract, from docs/design.md ("It controls the library's
// environment") and aqua v2.65.2's sources:
//
//   - ReadOptions maps the five HERMETIC_* variables and refuses any other name
//     of the prefix; NO_GLOBAL_CONFIG is a boolean as strconv.ParseBool reads
//     one, LOG_LEVEL one of debug, info, warn, error. The last value of a
//     repeated name wins; an empty value is none.
//   - New leaves the process environment as the verifiers it runs must see it:
//     the shell's minus every AQUA_*, the cosign trust knobs (TUF_*, SIGSTORE_*,
//     COSIGN_*), the CI and cloud identities, and every GitHub token; plus
//     TUF_ROOT in the root, and the token, when one is given, as
//     AQUA_GITHUB_TOKEN and GITHUB_TOKEN. aqua's library reads no other AQUA_*
//     variable that matters here (its settings file and the rest are its command
//     layer's, which is not linked), so nothing else is set.
//   - New's parameters are the root's and the options', whatever the shell
//     said: aqua's root under the root, the root's pins unless NoGlobalConfig,
//     the policies that exist (the root's, then the project's after the gate),
//     checksums verified and required, tracking off.

// hostile is a shell set against the library: aqua's variables pointing
// elsewhere, cosign's trust knobs and CI identities set, tokens of its own.
//
//nolint:gochecknoglobals // immutable data.
var hostile = map[string]string{
	"AQUA_ROOT_DIR": "/theirs", "AQUA_GLOBAL_CONFIG": "/theirs.yaml", "AQUA_POLICY_CONFIG": "/theirs-policy.yaml",
	"AQUA_DISABLE_POLICY": "true", "AQUA_CONFIG": "/theirs-project.yaml", "AQUA_GITHUB_TOKEN": "theirs",
	"AQUA_GOOS": "plan9", "GITHUB_TOKEN": "shell", "GH_TOKEN": "ci", "GH_ENTERPRISE_TOKEN": "ci",
	"TUF_ROOT": "/theirs/tuf", "TUF_MIRROR": "https://evil", "SIGSTORE_ROOT_FILE": "/evil/root.json",
	"COSIGN_EXPERIMENTAL": "1", "ACTIONS_ID_TOKEN_REQUEST_URL": "https://oidc", "GITLAB_TOKEN": "x",
	"BUILDKITE_AGENT_ACCESS_TOKEN": "x", "GITHUB_HOST": "evil.example", "SPIFFE_ENDPOINT_SOCKET": "/s",
	"GOOGLE_SERVICE_ACCOUNT_NAME": "x",
}

// kept is what the library's run keeps of the shell.
//
//nolint:gochecknoglobals // immutable data.
var kept = map[string]string{"GH_CONFIG_DIR": "/h/.gh", "SOURCE_DATE_EPOCH": "1", "AQUA": "x", "GITHUB_TOKEN2": "x"}

// newIn runs New with opts from an empty working directory, under the hostile
// shell and what it keeps, and returns the library.
func newIn(t *testing.T, opts library.Options) *library.Library {
	t.Helper()

	t.Chdir(t.TempDir())

	for key, value := range hostile {
		t.Setenv(key, value)
	}

	for key, value := range kept {
		t.Setenv(key, value)
	}

	lib, err := library.New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}

	return lib
}

//nolint:paralleltest // New writes the process environment, which t.Setenv restores after each subtest.
func TestNew(t *testing.T) {
	t.Run("the environment, with a token of ours", func(t *testing.T) {
		root := t.TempDir()

		newIn(t, library.Options{RootDir: root, Token: "ours", LogLevel: "info"})

		for key := range hostile {
			value, set := os.LookupEnv(key)

			switch key {
			case "TUF_ROOT":
				if value != filepath.Join(root, "sigstore") {
					t.Errorf("TUF_ROOT = %q, want the root's", value)
				}
			case "AQUA_GITHUB_TOKEN", "GITHUB_TOKEN":
				if value != "ours" {
					t.Errorf("%s = %q, want ours", key, value)
				}
			default:
				if set {
					t.Errorf("%s=%q reached the library", key, value)
				}
			}
		}

		for key, want := range kept {
			if got := os.Getenv(key); got != want {
				t.Errorf("%s = %q, want the shell's %q", key, got, want)
			}
		}
	})

	t.Run("the environment, without a token", func(t *testing.T) {
		newIn(t, library.Options{RootDir: t.TempDir(), LogLevel: "info"})

		for _, key := range []string{"AQUA_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
			if value, set := os.LookupEnv(key); set {
				t.Errorf("without a token of ours, %s=%q reached the library", key, value)
			}
		}
	})

	t.Run("the parameters", func(t *testing.T) {
		root := rootdir.Root(t.TempDir())

		lib := newIn(t, library.Options{RootDir: string(root), LogLevel: "debug", Version: "v1.2.3"})
		param := lib.Param()

		if lib.Root != root || param.RootDir != root.AquaRoot() {
			t.Errorf("root = %q, aqua's = %q, want %q under %q", lib.Root, param.RootDir, root.AquaRoot(), root)
		}

		if !slices.Equal(param.GlobalConfigFilePaths, []string{root.GlobalConfig()}) {
			t.Errorf("global configs = %q, want the root's pins", param.GlobalConfigFilePaths)
		}

		// No policy in the root, no project: none listed, since the library
		// refuses a listed file that is not there.
		if len(param.PolicyConfigFilePaths) != 0 {
			t.Errorf("policies = %q, want none", param.PolicyConfigFilePaths)
		}

		if param.ConfigFilePath != "" || param.LogLevel != "debug" || param.AQUAVersion != "v1.2.3" {
			t.Errorf("config %q, level %q, version %q", param.ConfigFilePath, param.LogLevel, param.AQUAVersion)
		}

		if !param.Checksum || !param.RequireChecksum || !param.EnforceChecksum || !param.EnforceRequireChecksum ||
			param.DisablePolicy || param.DisableLazyInstall || !param.DisableTracking || param.ProgressBar {
			t.Errorf("knobs: %+v", param)
		}

		if param.MaxParallelism <= 0 {
			t.Errorf("parallelism = %d", param.MaxParallelism)
		}

		// Param is a copy: a command filling it in leaves the next one alone.
		param.All = true
		if lib.Param().All {
			t.Error("Param is not a copy")
		}
	})

	t.Run("without the global config", func(t *testing.T) {
		lib := newIn(t, library.Options{RootDir: t.TempDir(), NoGlobalConfig: true, LogLevel: "info"})

		if paths := lib.Param().GlobalConfigFilePaths; len(paths) != 0 {
			t.Errorf("global configs = %q, want none", paths)
		}
	})

	t.Run("the root policy, listed", func(t *testing.T) {
		root := rootdir.Root(t.TempDir())
		if err := os.WriteFile(root.Policy(), []byte("registries: []\npackages: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		lib := newIn(t, library.Options{RootDir: string(root), LogLevel: "info"})

		if paths := lib.Param().PolicyConfigFilePaths; !slices.Equal(paths, []string{root.Policy()}) {
			t.Errorf("policies = %q, want the root's", paths)
		}
	})

	t.Run("a named config", func(t *testing.T) {
		// A named config with no policy beside it carries no project to gate.
		named := filepath.Join(t.TempDir(), "hermetic.yaml")

		lib := newIn(t, library.Options{RootDir: t.TempDir(), Config: named, LogLevel: "info"})

		if got := lib.Param().ConfigFilePath; got != named {
			t.Errorf("config = %q, want %q", got, named)
		}
	})
}

func TestReadOptions(t *testing.T) {
	t.Parallel()

	opts, err := library.ReadOptions([]string{
		"HERMETIC_ROOT_DIR=/r",
		"HERMETIC_CONFIG=/c.yaml",
		"HERMETIC_NO_GLOBAL_CONFIG=true",
		"HERMETIC_GITHUB_TOKEN=t",
		"HERMETIC_LOG_LEVEL=warn",
		"AQUA_ROOT_DIR=/theirs",
		"PATH=/bin",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := library.Options{RootDir: "/r", Config: "/c.yaml", NoGlobalConfig: true, Token: "t", LogLevel: "warn"}
	if opts != want {
		t.Fatalf("options = %+v, want %+v", opts, want)
	}
}
