package hermetic_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// Past the bounded walks: the invariants docs/design.md states for the
// dispatch and the two environments, over arbitrary names and entries.

// The AQUA_* names the binary sets itself; nothing else of that prefix
// reaches the library.
//
//nolint:gochecknoglobals // immutable data.
var ours = []string{
	"AQUA_ROOT_DIR", "AQUA_GLOBAL_CONFIG", "AQUA_POLICY_CONFIG", "AQUA_CONFIG", "AQUA_GITHUB_TOKEN",
	"AQUA_LOG_LEVEL", "AQUA_DISABLE_POLICY", "AQUA_DISABLE_LAZY_INSTALL", "AQUA_CHECKSUM",
	"AQUA_REQUIRE_CHECKSUM", "AQUA_ENFORCE_CHECKSUM", "AQUA_ENFORCE_REQUIRE_CHECKSUM",
	"AQUA_DISABLE_TRACKING", "AQUA_PROGRESS_BAR", "AQUA_MAX_PARALLELISM", "AQUA_LOG_COLOR",
	"GITHUB_TOKEN", "TUF_ROOT",
}

func FuzzDispatched(f *testing.F) {
	f.Add("/r/bin/hermetic", "a", true)
	f.Add(`C:\r\bin\limen.exe`, "", false)
	f.Add("hermetic/", "x", true)

	f.Fuzz(func(t *testing.T, argv0, arg string, unix bool) {
		goos := "linux"
		if !unix {
			goos = "windows"
		}

		tool, args := hermetic.Dispatched([]string{argv0, arg}, goos)

		// A tool is the last path element, never the binary's name, never
		// empty, never a path; its arguments are the rest, untouched.
		if tool == "" {
			return
		}

		if tool == hermetic.Name || strings.ContainsAny(tool, "/") ||
			(goos == "windows" && strings.ContainsAny(tool, `\`)) {
			t.Fatalf("Dispatched(%q, %s) = %q", argv0, goos, tool)
		}

		if !strings.HasSuffix(argv0, tool) && !(goos == "windows" && strings.HasSuffix(argv0, tool+".exe")) {
			t.Fatalf("Dispatched(%q, %s) = %q, not the name invoked", argv0, goos, tool)
		}

		if !slices.Equal(args, []string{arg}) {
			t.Fatalf("args = %q, want %q", args, []string{arg})
		}
	})
}

func FuzzLibraryEnvironment(f *testing.F) {
	f.Add("AQUA_ROOT_DIR=/theirs\nHERMETIC_GITHUB_TOKEN=t\nPATH=/bin", true)
	f.Add("HERMETIC_NO_GLOBAL_CONFIG=true\nGH_TOKEN=x\n=\n\nNOEQ", false)
	f.Add("HERMETIC_TYPO=1", true)

	f.Fuzz(func(t *testing.T, raw string, withPolicy bool) {
		root := hermetic.Root(filepath.Join(string(os.PathSeparator), "r"))
		environ := strings.Split(raw, "\n")

		policy := ""
		if withPolicy {
			policy = filepath.Join(string(os.PathSeparator), "p", "policy.yaml")
		}

		set, unset, err := hermetic.LibraryEnvironment(root, environ, policy)

		// The one refusal: an unknown HERMETIC_* name, or NO_GLOBAL_CONFIG
		// that is not a boolean; everything else is accepted.
		unknown, badBool := false, false

		for _, entry := range environ {
			key, value, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(key, "HERMETIC_") {
				switch key {
				case "HERMETIC_ROOT_DIR", "HERMETIC_CONFIG", "HERMETIC_GITHUB_TOKEN", "HERMETIC_LOG_LEVEL":
				case "HERMETIC_NO_GLOBAL_CONFIG":
					// The last value is the one parsed.
					badBool = value != "" && !slices.Contains([]string{
						"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False",
					}, value)
				default:
					unknown = true
				}
			}
		}

		if unknown {
			if err == nil {
				t.Fatalf("an unknown variable was accepted: %q", raw)
			}

			return
		}

		if err != nil {
			if badBool {
				return
			}

			t.Fatalf("refused: %v for %q", err, raw)
		}

		// Nothing of the shell's reaches the set under an AQUA_* name, or
		// under any name but the ones the binary sets.
		for key, value := range set {
			if !slices.Contains(ours, key) {
				t.Fatalf("set carries %s=%q", key, value)
			}
		}

		// Every scrubbed name is in unset, sorted, and nothing else is.
		for _, entry := range environ {
			key, _, _ := strings.Cut(entry, "=")
			scrubbed := slices.ContainsFunc(
				[]string{"AQUA_", "COSIGN_", "SIGSTORE_", "TUF_", "GITLAB_", "BUILDKITE_", "ACTIONS_ID_TOKEN_REQUEST_"},
				func(p string) bool { return strings.HasPrefix(key, p) },
			) || slices.Contains(
				[]string{
					"GITHUB_TOKEN",
					"GITHUB_HOST",
					"SPIFFE_ENDPOINT_SOCKET",
					"GOOGLE_SERVICE_ACCOUNT_NAME",
					"GH_TOKEN",
					"GH_ENTERPRISE_TOKEN",
				},
				key,
			)

			if scrubbed != slices.Contains(unset, key) {
				t.Fatalf("%q: scrubbed=%v, unset=%v", key, scrubbed, unset)
			}
		}

		if !slices.IsSorted(unset) {
			t.Fatalf("unset is not sorted: %v", unset)
		}

		// The three paths are the root's, whatever the shell said; the
		// policy list is the project's when given.
		if set["AQUA_ROOT_DIR"] != root.AquaRoot() || set["TUF_ROOT"] != filepath.Join(string(root), "sigstore") {
			t.Fatalf("paths: %v", set)
		}

		if set["AQUA_GLOBAL_CONFIG"] != root.GlobalConfig() &&
			set["AQUA_GLOBAL_CONFIG"] != string(os.PathListSeparator) {
			t.Fatalf("global config: %q", set["AQUA_GLOBAL_CONFIG"])
		}

		if set["AQUA_POLICY_CONFIG"] != policy {
			t.Fatalf("policy list = %q, want %q", set["AQUA_POLICY_CONFIG"], policy)
		}

		// The token reaches both names or neither; GITHUB_TOKEN in the set
		// is only ever ours.
		if set["AQUA_GITHUB_TOKEN"] != set["GITHUB_TOKEN"] {
			t.Fatalf("token: %q / %q", set["AQUA_GITHUB_TOKEN"], set["GITHUB_TOKEN"])
		}

		if set["AQUA_LOG_LEVEL"] == "" {
			t.Fatal("no log level set")
		}
	})
}

func FuzzToolEnvironment(f *testing.F) {
	f.Add("AQUA_ROOT_DIR=/theirs\nPATH=/bin\nCOSIGN_X=1")
	f.Add("\n=\nNOEQ\nAQUA_=x\nAQUA=kept")

	f.Fuzz(func(t *testing.T, raw string) {
		environ := strings.Split(raw, "\n")
		got := hermetic.ToolEnvironment(environ)

		want := make([]string, 0, len(environ))

		for _, entry := range environ {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(key, "AQUA_") {
				want = append(want, entry)
			}
		}

		if !slices.Equal(got, want) {
			t.Fatalf("tool environment = %q, want %q", got, want)
		}
	})
}
