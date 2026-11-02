package library_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/library"
	"github.com/forkcloser/hermetic/internal/rootdir"
)

func TestLibraryEnvironment(t *testing.T) {
	t.Parallel()

	root := rootdir.Root(filepath.FromSlash("/r"))
	sep := string(os.PathListSeparator)

	// A hostile shell: aqua's variables set to everything the root owns and
	// more, cosign's trust knobs and CI identities set, a token of its own;
	// every one of them scrubbed. Ours set: the exposed ones mapped, nothing
	// else read.
	environ := []string{
		"AQUA_ROOT_DIR=/theirs",
		"AQUA_GLOBAL_CONFIG=/theirs.yaml",
		"AQUA_POLICY_CONFIG=/theirs-policy.yaml",
		"AQUA_DISABLE_POLICY=true",
		"AQUA_CONFIG=/theirs-project.yaml",
		"AQUA_GITHUB_TOKEN=theirs",
		"GITHUB_TOKEN=shell",
		"GH_TOKEN=ci",
		"TUF_ROOT=/theirs/tuf",
		"TUF_MIRROR=https://evil",
		"SIGSTORE_ROOT_FILE=/evil/root.json",
		"COSIGN_EXPERIMENTAL=1",
		"ACTIONS_ID_TOKEN_REQUEST_URL=https://oidc",
		"GITLAB_TOKEN=x",
		"GITHUB_HOST=evil.example",
		"HERMETIC_GITHUB_TOKEN=ours",
		"HERMETIC_LOG_LEVEL=debug",
		"HERMETIC_CONFIG=/ours.yaml",
		"HERMETIC_ROOT_DIR=/elsewhere",
		"HOME=/h",
		"PATH=/bin",
		"GH_CONFIG_DIR=/h/.gh",
		"SOURCE_DATE_EPOCH=1",
	}

	set, unset, err := library.Environment(root, environ, "")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"AQUA_ROOT_DIR":      filepath.FromSlash("/r/store"),
		"AQUA_GLOBAL_CONFIG": filepath.FromSlash("/r/hermetic.yaml"),
		// A root with no policy in it names none: the library refuses a listed
		// file that is not there.
		"AQUA_POLICY_CONFIG": "",
		"TUF_ROOT":           filepath.FromSlash("/r/sigstore"),
		"AQUA_GITHUB_TOKEN":  "ours",
		"GITHUB_TOKEN":       "ours",
		"AQUA_LOG_LEVEL":     "debug",
		"AQUA_CONFIG":        "/ours.yaml",
		// The knobs the settings file would otherwise decide, pinned.
		"AQUA_DISABLE_POLICY":           "false",
		"AQUA_DISABLE_LAZY_INSTALL":     "false",
		"AQUA_CHECKSUM":                 "true",
		"AQUA_REQUIRE_CHECKSUM":         "true",
		"AQUA_ENFORCE_CHECKSUM":         "true",
		"AQUA_ENFORCE_REQUIRE_CHECKSUM": "true",
		"AQUA_DISABLE_TRACKING":         "true",
		"AQUA_PROGRESS_BAR":             "false",
		"AQUA_MAX_PARALLELISM":          "5",
		"AQUA_LOG_COLOR":                "auto",
	}

	for k, v := range want {
		if set[k] != v {
			t.Errorf("%s = %q, want %q", k, set[k], v)
		}
	}

	if len(set) != len(want) {
		t.Errorf("set = %v, want exactly %v", set, want)
	}

	wantUnset := []string{
		"ACTIONS_ID_TOKEN_REQUEST_URL", "AQUA_CONFIG", "AQUA_DISABLE_POLICY", "AQUA_GITHUB_TOKEN",
		"AQUA_GLOBAL_CONFIG", "AQUA_POLICY_CONFIG", "AQUA_ROOT_DIR", "COSIGN_EXPERIMENTAL",
		"GH_TOKEN", "GITHUB_HOST", "GITHUB_TOKEN", "GITLAB_TOKEN", "SIGSTORE_ROOT_FILE", "TUF_MIRROR", "TUF_ROOT",
	}
	if !slices.Equal(unset, wantUnset) {
		t.Errorf("unset = %v, want %v", unset, wantUnset)
	}

	// Without a token of ours, the shell's never reaches the library.
	set, unset, err = library.Environment(root, []string{"GITHUB_TOKEN=shell"}, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := set["GITHUB_TOKEN"]; ok || !slices.Contains(unset, "GITHUB_TOKEN") {
		t.Errorf("the shell's token reached the library: set=%v unset=%v", set, unset)
	}

	// No global config, by the boolean; and without a log level of ours, the
	// library's default is set outright rather than left to the settings file.
	none, _, err := library.Environment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=true"}, "")
	if err != nil {
		t.Fatal(err)
	}

	if none["AQUA_GLOBAL_CONFIG"] != sep {
		t.Errorf("no global config: AQUA_GLOBAL_CONFIG = %q, want a lone list separator", none["AQUA_GLOBAL_CONFIG"])
	}

	if none["AQUA_LOG_LEVEL"] != "info" {
		t.Errorf("default log level: AQUA_LOG_LEVEL = %q, want info", none["AQUA_LOG_LEVEL"])
	}

	// The policies are what exists: the project's, after the gate, alone
	// while the root holds none; after the root's once the root holds one.
	policy := filepath.FromSlash("/p/.hermetic/policy.yaml")
	stocked := rootdir.Root(t.TempDir())

	projectOnly, _, err := library.Environment(stocked, nil, policy)
	if err != nil {
		t.Fatal(err)
	}

	if projectOnly["AQUA_POLICY_CONFIG"] != policy {
		t.Errorf("policy list without a root policy = %q, want %q", projectOnly["AQUA_POLICY_CONFIG"], policy)
	}

	if writeErr := os.WriteFile(stocked.Policy(), []byte("registries: []\npackages: []\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	withPolicy, _, err := library.Environment(stocked, nil, policy)
	if err != nil {
		t.Fatal(err)
	}

	if withPolicy["AQUA_POLICY_CONFIG"] != stocked.Policy()+sep+policy {
		t.Errorf("policy list = %q, want %q", withPolicy["AQUA_POLICY_CONFIG"], stocked.Policy()+sep+policy)
	}

	rootOnly, _, err := library.Environment(stocked, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if rootOnly["AQUA_POLICY_CONFIG"] != stocked.Policy() {
		t.Errorf("policy list without a project policy = %q, want %q", rootOnly["AQUA_POLICY_CONFIG"], stocked.Policy())
	}

	// A HERMETIC_* the binary does not know is refused, named; so is a value
	// that is not the boolean asked for.
	_, _, err = library.Environment(root, []string{"HERMETIC_DISABLE_POLICY=true"}, "")
	if err == nil || !strings.Contains(err.Error(), "HERMETIC_DISABLE_POLICY") {
		t.Errorf("an unknown variable was accepted: %v", err)
	}

	_, _, err = library.Environment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=yes"}, "")
	if err == nil || !strings.Contains(err.Error(), "yes") {
		t.Errorf("an unparsable boolean was accepted: %v", err)
	}
}

// Every entry shape a process environment can carry, and what the design
// says of each: scrubbed, mapped, refused, or passed by.
func TestLibraryEnvironmentEveryEntryShape(t *testing.T) {
	t.Parallel()

	root := rootdir.Root(t.TempDir())

	refused := []string{
		"HERMETIC_=x",              // the bare prefix is not a known name
		"HERMETIC_ROOT=x",          // a prefix of a known name
		"HERMETIC_ROOT_DIR_X=x",    // a known name as a prefix
		"HERMETIC_GITHUB_TOKEN2=x", // a known name with a suffix
		"HERMETIC_X",               // no "=": the key is the whole entry
	}

	for _, entry := range refused {
		if _, _, err := library.Environment(root, []string{entry}, ""); err == nil {
			t.Errorf("%q was accepted", entry)
		}
	}

	passedBy := []string{
		"hermetic_root_dir=x", // lower case is another name
		"XHERMETIC_ROOT_DIR=x",
		"AQUA=x", // no underscore
		"aqua_root_dir=x",
		"COSIGN=x",
		"GITHUB_TOKEN2=x",
		"=x", // an empty key
		"",   // an empty entry
		"NOEQUALS",
		"PATH=/a=b", // the first "=" splits
	}

	for _, entry := range passedBy {
		set, unset, err := library.Environment(root, []string{entry}, "")
		if err != nil {
			t.Errorf("%q: %v", entry, err)

			continue
		}

		if len(unset) != 0 {
			t.Errorf("%q was scrubbed: %v", entry, unset)
		}

		key, _, _ := strings.Cut(entry, "=")
		if _, ok := set[key]; ok {
			t.Errorf("%q reached the set: %v", entry, set)
		}
	}

	scrubbed := []string{
		"AQUA_=x", "AQUA_ANYTHING=x", "COSIGN_=x", "SIGSTORE_=x", "TUF_=x", "GITLAB_=x", "BUILDKITE_=x",
		"ACTIONS_ID_TOKEN_REQUEST_=x", "GITHUB_TOKEN=x", "GITHUB_HOST=x", "SPIFFE_ENDPOINT_SOCKET=x",
		"GOOGLE_SERVICE_ACCOUNT_NAME=x", "GH_TOKEN=x", "GH_ENTERPRISE_TOKEN=x",
		"GITHUB_TOKEN", // scrubbed by name even without a value
	}

	for _, entry := range scrubbed {
		_, unset, err := library.Environment(root, []string{entry}, "")
		if err != nil {
			t.Errorf("%q: %v", entry, err)

			continue
		}

		key, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(unset, key) {
			t.Errorf("%q was not scrubbed: %v", entry, unset)
		}
	}

	// The last value of a repeated key wins, as a process environment reads;
	// the token reaches both names, whatever its bytes; an empty value is as
	// good as none.
	set, _, err := library.Environment(root, []string{
		"HERMETIC_GITHUB_TOKEN=first",
		"HERMETIC_GITHUB_TOKEN=second=with\nnoise",
		"HERMETIC_LOG_LEVEL=",
		"HERMETIC_CONFIG=",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	if set["AQUA_GITHUB_TOKEN"] != "second=with\nnoise" || set["GITHUB_TOKEN"] != "second=with\nnoise" {
		t.Errorf("token = %q / %q, want the last value, whole", set["AQUA_GITHUB_TOKEN"], set["GITHUB_TOKEN"])
	}

	if set["AQUA_LOG_LEVEL"] != "info" {
		t.Errorf("an empty log level did not fall to the default: %q", set["AQUA_LOG_LEVEL"])
	}

	if _, ok := set["AQUA_CONFIG"]; ok {
		t.Errorf("an empty config named one: %q", set["AQUA_CONFIG"])
	}

	// Every spelling strconv.ParseBool takes, and the design's "true".
	for _, v := range []string{"true", "TRUE", "True", "t", "T", "1"} {
		set, _, err := library.Environment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=" + v}, "")
		if err != nil || set["AQUA_GLOBAL_CONFIG"] != string(os.PathListSeparator) {
			t.Errorf("NO_GLOBAL_CONFIG=%s: %v %q", v, err, set["AQUA_GLOBAL_CONFIG"])
		}
	}

	for _, v := range []string{"false", "0", "F"} {
		set, _, err := library.Environment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=" + v}, "")
		if err != nil || set["AQUA_GLOBAL_CONFIG"] != root.GlobalConfig() {
			t.Errorf("NO_GLOBAL_CONFIG=%s: %v %q", v, err, set["AQUA_GLOBAL_CONFIG"])
		}
	}

	for _, v := range []string{"yes", "no", "on", " true", "true "} {
		if _, _, err := library.Environment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=" + v}, ""); err == nil {
			t.Errorf("NO_GLOBAL_CONFIG=%q was accepted", v)
		}
	}
}

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

func FuzzLibraryEnvironment(f *testing.F) {
	f.Add("AQUA_ROOT_DIR=/theirs\nHERMETIC_GITHUB_TOKEN=t\nPATH=/bin", true)
	f.Add("HERMETIC_NO_GLOBAL_CONFIG=true\nGH_TOKEN=x\n=\n\nNOEQ", false)
	f.Add("HERMETIC_TYPO=1", true)

	f.Fuzz(func(t *testing.T, raw string, withPolicy bool) {
		root := rootdir.Root(filepath.Join(string(os.PathSeparator), "r"))
		environ := strings.Split(raw, "\n")

		policy := ""
		if withPolicy {
			policy = filepath.Join(string(os.PathSeparator), "p", "policy.yaml")
		}

		set, unset, err := library.Environment(root, environ, policy)

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
