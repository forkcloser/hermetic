package hermetic_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/filesystem/dirs"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

func getenv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRootFrom(t *testing.T) {
	// The default root is created on resolution, so the platform's base
	// directories point into a scratch home for this test (hence no Parallel).
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))

	// The variable wins, made absolute and cleaned.
	got, err := hermetic.RootFrom(getenv(map[string]string{hermetic.RootEnv: filepath.FromSlash("/r/")}))
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.Abs(filepath.FromSlash("/r"))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Fatalf("variable: root = %q, want %q", got, want)
	}

	// A relative value is made absolute, so the derived variables survive a
	// dispatched tool's own working directory.
	got, err = hermetic.RootFrom(getenv(map[string]string{hermetic.RootEnv: "rel"}))
	if err != nil {
		t.Fatal(err)
	}

	if !filepath.IsAbs(string(got)) || filepath.Base(string(got)) != "rel" {
		t.Fatalf("relative root: %q", got)
	}

	// Without the variable, the platform's data directory for the name, as
	// primordium resolves it.
	got, err = hermetic.RootFrom(getenv(nil))
	if err != nil {
		t.Fatal(err)
	}

	dirs.SetAppName(hermetic.Name)

	want, err = dirs.DataDir()
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Fatalf("default: root = %q, want primordium's %q", got, want)
	}

	if filepath.Base(want) != hermetic.Name {
		t.Fatalf("the default root is not named after the binary: %q", want)
	}
}

func TestLibraryEnvironment(t *testing.T) {
	t.Parallel()

	root := hermetic.Root(filepath.FromSlash("/r"))
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

	set, unset, err := hermetic.LibraryEnvironment(root, environ, "")
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
	set, unset, err = hermetic.LibraryEnvironment(root, []string{"GITHUB_TOKEN=shell"}, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := set["GITHUB_TOKEN"]; ok || !slices.Contains(unset, "GITHUB_TOKEN") {
		t.Errorf("the shell's token reached the library: set=%v unset=%v", set, unset)
	}

	// No global config, by the boolean; and without a log level of ours, the
	// library's default is set outright rather than left to the settings file.
	none, _, err := hermetic.LibraryEnvironment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=true"}, "")
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
	stocked := hermetic.Root(t.TempDir())

	projectOnly, _, err := hermetic.LibraryEnvironment(stocked, nil, policy)
	if err != nil {
		t.Fatal(err)
	}

	if projectOnly["AQUA_POLICY_CONFIG"] != policy {
		t.Errorf("policy list without a root policy = %q, want %q", projectOnly["AQUA_POLICY_CONFIG"], policy)
	}

	if writeErr := os.WriteFile(stocked.Policy(), []byte("registries: []\npackages: []\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	withPolicy, _, err := hermetic.LibraryEnvironment(stocked, nil, policy)
	if err != nil {
		t.Fatal(err)
	}

	if withPolicy["AQUA_POLICY_CONFIG"] != stocked.Policy()+sep+policy {
		t.Errorf("policy list = %q, want %q", withPolicy["AQUA_POLICY_CONFIG"], stocked.Policy()+sep+policy)
	}

	rootOnly, _, err := hermetic.LibraryEnvironment(stocked, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if rootOnly["AQUA_POLICY_CONFIG"] != stocked.Policy() {
		t.Errorf("policy list without a project policy = %q, want %q", rootOnly["AQUA_POLICY_CONFIG"], stocked.Policy())
	}

	// A HERMETIC_* the binary does not know is refused, named; so is a value
	// that is not the boolean asked for.
	_, _, err = hermetic.LibraryEnvironment(root, []string{"HERMETIC_DISABLE_POLICY=true"}, "")
	if err == nil || !strings.Contains(err.Error(), "HERMETIC_DISABLE_POLICY") {
		t.Errorf("an unknown variable was accepted: %v", err)
	}

	_, _, err = hermetic.LibraryEnvironment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=yes"}, "")
	if err == nil || !strings.Contains(err.Error(), "yes") {
		t.Errorf("an unparsable boolean was accepted: %v", err)
	}
}

func TestToolEnvironment(t *testing.T) {
	t.Parallel()

	got := hermetic.ToolEnvironment([]string{
		"AQUA_ROOT_DIR=/theirs", "COSIGN_PASSWORD=secret", "GITHUB_TOKEN=shell", "HERMETIC_ROOT_DIR=/r", "PATH=/bin",
	})
	want := []string{"COSIGN_PASSWORD=secret", "GITHUB_TOKEN=shell", "HERMETIC_ROOT_DIR=/r", "PATH=/bin"}

	if !slices.Equal(got, want) {
		t.Fatalf("tool environment = %v, want %v", got, want)
	}
}

func TestDispatched(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		args     []string
		goos     string
		tool     string
		toolArgs []string
	}{
		{name: "own name", args: []string{"/r/bin/hermetic", "i", "-a"}, goos: "linux"},
		{
			name:     "a tool",
			args:     []string{"/r/bin/limen", "check", "."},
			goos:     "linux",
			tool:     "limen",
			toolArgs: []string{"check", "."},
		},
		{name: "a tool, no args", args: []string{"ssh"}, goos: "darwin", tool: "ssh", toolArgs: []string{}},
		{name: "windows own name", args: []string{`C:\r\bin\Hermetic.exe`, "which", "limen"}, goos: "windows"},
		{
			name: "windows tool", args: []string{`C:\r\bin\limen.exe`, "version"}, goos: "windows",
			tool: "limen", toolArgs: []string{"version"},
		},
		{name: "empty", args: nil, goos: "linux"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			tool, toolArgs := hermetic.Dispatched(c.args, c.goos)
			if tool != c.tool {
				t.Fatalf("tool = %q, want %q", tool, c.tool)
			}

			if tool != "" && !slices.Equal(toolArgs, c.toolArgs) {
				t.Fatalf("args = %q, want %q", toolArgs, c.toolArgs)
			}
		})
	}
}

func TestFindProject(t *testing.T) {
	t.Parallel()

	top := t.TempDir()
	project := filepath.Join(top, "repo")
	deep := filepath.Join(project, "a", "b")

	if err := os.MkdirAll(filepath.Join(project, hermetic.ProjectDir), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}

	config := filepath.Join(project, hermetic.ProjectDir, hermetic.ProjectConfig)
	if err := os.WriteFile(config, []byte("packages: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := hermetic.FindProject(deep)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(project, hermetic.ProjectDir); got != want {
		t.Fatalf("from a subdirectory: %q, want %q", got, want)
	}

	// The directory alone, without the config inside, is not a project.
	bare := filepath.Join(top, "bare")
	if mkErr := os.MkdirAll(filepath.Join(bare, hermetic.ProjectDir), 0o750); mkErr != nil {
		t.Fatal(mkErr)
	}

	got, err = hermetic.FindProject(bare)
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Fatalf("a bare directory found %q, want none", got)
	}
}

func TestProjectPolicyPath(t *testing.T) {
	t.Parallel()

	// JSON, decoded, rather than text grepped: the text handler quotes a value
	// with spaces and escapes its backslashes, so a Windows path in the command
	// never matches the plain string.
	var logged bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logged, nil))
	top := t.TempDir()
	aquaRoot := filepath.Join(top, "store")
	project := filepath.Join(top, "repo", hermetic.ProjectDir)

	if err := os.MkdirAll(project, 0o750); err != nil {
		t.Fatal(err)
	}

	policy := filepath.Join(project, hermetic.ProjectPolicy)
	body := []byte("registries:\n  - type: standard\n")

	if err := os.WriteFile(policy, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// No project: nothing.
	if got, err := hermetic.ProjectPolicyPath(logger, aquaRoot, ""); err != nil || got != "" {
		t.Fatalf("no project: %q, %v", got, err)
	}
	// No record: the gate holds, the policy is dropped.
	if got, err := hermetic.ProjectPolicyPath(logger, aquaRoot, project); err != nil || got != "" {
		t.Fatalf("without a record: %q, %v; want dropped", got, err)
	}
	// The warning names this binary's commands, never aqua's, which a hermetic
	// machine does not have.
	warning := lastRecord(t, &logged)
	for key, want := range map[string]string{
		"msg":    "the project policy is ignored until allowed",
		"policy": policy,
		"allow":  "hermetic policy allow " + policy,
		"deny":   "hermetic policy deny " + policy,
	} {
		if warning[key] != want {
			t.Fatalf("warning %s = %q, want %q", key, warning[key], want)
		}
	}

	if strings.Contains(logged.String(), "aqua policy") {
		t.Fatalf("the warning names aqua's command:\n%s", logged.String())
	}

	logged.Reset()
	// The record `policy allow` writes: the policy's copy under policies/ at
	// its absolute path (the drive's colon an underscore on Windows).
	record := filepath.Join(aquaRoot, "policies", strings.ReplaceAll(policy, ":", "_"))
	if err := os.MkdirAll(filepath.Dir(record), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(record, body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := hermetic.ProjectPolicyPath(logger, aquaRoot, project)
	if err != nil {
		t.Fatal(err)
	}

	if got != policy {
		t.Fatalf("with a matching record: %q, want %q", got, policy)
	}
	// A changed policy: the record is stale, the policy is dropped again.
	changed := append(slices.Clone(body), []byte("  - name: evil\n    type: local\n    path: x.yaml\n")...)
	if err := os.WriteFile(policy, changed, 0o600); err != nil {
		t.Fatal(err)
	}

	if stale, staleErr := hermetic.ProjectPolicyPath(logger, aquaRoot, project); staleErr != nil || stale != "" {
		t.Fatalf("with a stale record: %q, %v; want dropped", stale, staleErr)
	}

	if msg := lastRecord(t, &logged)["msg"]; msg != "the project policy changed since it was allowed and is ignored until allowed again" {
		t.Fatalf("a stale record is not said to be stale: msg = %q", msg)
	}

	logged.Reset()
	// The record `policy deny` writes: the human chose to ignore the policy,
	// and the warning goes quiet.
	denied := filepath.Join(aquaRoot, "policy-warnings", strings.ReplaceAll(policy, ":", "_"))
	if err := os.MkdirAll(filepath.Dir(denied), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(denied, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if quiet, quietErr := hermetic.ProjectPolicyPath(logger, aquaRoot, project); quietErr != nil || quiet != "" {
		t.Fatalf("with a deny record: %q, %v; want dropped", quiet, quietErr)
	}

	if logged.Len() != 0 {
		t.Fatalf("a denied policy still warns:\n%s", logged.String())
	}
	// A project without a policy file has nothing to gate.
	if err := os.Remove(policy); err != nil {
		t.Fatal(err)
	}

	if none, noneErr := hermetic.ProjectPolicyPath(logger, aquaRoot, project); noneErr != nil || none != "" {
		t.Fatalf("without a policy: %q, %v", none, noneErr)
	}
}

// mirrorRoot is a scratch root with a binary for the platform named and the
// commands aqua exposes, as symlinks to its proxy the way aqua links them.
func mirrorRoot(t *testing.T, goos string, exposed ...string) (hermetic.Root, string) {
	t.Helper()

	root := hermetic.Root(t.TempDir())
	aquaBin := filepath.Join(root.AquaRoot(), "bin")

	if err := os.MkdirAll(aquaBin, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(root.Bin(), 0o750); err != nil {
		t.Fatal(err)
	}

	binary := root.Binary(goos)
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, name := range exposed {
		if err := os.Symlink("../aqua-proxy", filepath.Join(aquaBin, name)); err != nil {
			t.Fatal(err)
		}
	}

	return root, binary
}

func binNames(t *testing.T, root hermetic.Root) []string {
	t.Helper()

	entries, err := os.ReadDir(root.Bin())
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestMirror(t *testing.T) {
	t.Parallel()

	root, binary := mirrorRoot(t, "linux", "aqua", "limen", "ssh")
	// A stale link of ours, a wrong link under a wanted name, a foreign
	// symlink, and a foreign file.
	if err := os.Symlink(hermetic.Name, filepath.Join(root.Bin(), "gone")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("/usr/bin/true", filepath.Join(root.Bin(), "ssh")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("/usr/bin/false", filepath.Join(root.Bin(), "theirs")); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root.Bin(), "keep"), []byte("theirs"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := hermetic.Mirror(root, "linux"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"limen", "ssh"} {
		target, err := os.Readlink(filepath.Join(root.Bin(), name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if target != hermetic.Name {
			t.Fatalf("%s -> %q, want %q", name, target, hermetic.Name)
		}
	}

	if want := []string{hermetic.Name, "keep", "limen", "ssh", "theirs"}; !slices.Equal(binNames(t, root), want) {
		t.Fatalf(
			"bin = %v, want %v (aqua not mirrored, the stale link gone, the foreign entries kept)",
			binNames(t, root),
			want,
		)
	}

	if body, err := os.ReadFile(filepath.Join(root.Bin(), "keep")); err != nil || string(body) != "theirs" {
		t.Fatalf("the foreign file was touched: %q, %v", body, err)
	}

	// Idempotent, and aqua dropping a command drops the link.
	if err := os.Remove(filepath.Join(root.AquaRoot(), "bin", "ssh")); err != nil {
		t.Fatal(err)
	}

	if err := hermetic.Mirror(root, "linux"); err != nil {
		t.Fatal(err)
	}

	if want := []string{hermetic.Name, "keep", "limen", "theirs"}; !slices.Equal(binNames(t, root), want) {
		t.Fatalf("bin = %v, want %v", binNames(t, root), want)
	}

	// No binary: the mirror refuses rather than linking to nothing.
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}

	if err := hermetic.Mirror(root, "linux"); err == nil || !strings.Contains(err.Error(), "the binary") {
		t.Fatalf("without the binary: %v", err)
	}
}

// TestMirrorHardLinks is the Windows shape, which hard links let every
// platform exercise: a hard link is a second name for the file as linked, so a
// replaced binary leaves the old names on the old file, and the mirror must
// relink them.
func TestMirrorHardLinks(t *testing.T) {
	t.Parallel()

	root, binary := mirrorRoot(t, "windows", "aqua.exe", "limen.exe")

	if err := hermetic.Mirror(root, "windows"); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root.Bin(), "limen.exe")

	same, err := sameFile(link, binary)
	if err != nil {
		t.Fatal(err)
	}

	if !same {
		t.Fatal("limen.exe is not a hard link to the binary")
	}

	// The binary replaced by rename-aside and a new write: the old link now
	// names the old file.
	if err := os.Rename(binary, binary+".old"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho new\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	if same, err := sameFile(link, binary); err != nil || same {
		t.Fatalf("after the replacement the link should be stale: same=%v, %v", same, err)
	}

	// A regular file under an unwanted name is nobody's: it stays.
	if err := os.WriteFile(filepath.Join(root.Bin(), "notes.txt"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := hermetic.Mirror(root, "windows"); err != nil {
		t.Fatal(err)
	}

	if same, err := sameFile(link, binary); err != nil || !same {
		t.Fatalf("the stale hard link was not relinked: same=%v, %v", same, err)
	}

	want := []string{"hermetic.exe", "hermetic.exe.old", "limen.exe", "notes.txt"}
	if got := binNames(t, root); !slices.Equal(got, want) {
		t.Fatalf("bin = %v, want %v", got, want)
	}
}

func sameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}

	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}

	return os.SameFile(ia, ib), nil
}

// lastRecord is the last JSON record a logger wrote into logged, decoded, so
// an attribute compares as the value logged whatever the handler's quoting.
func lastRecord(t *testing.T, logged *bytes.Buffer) map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(logged.String()), "\n")

	var record map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &record); err != nil {
		t.Fatalf("decode the log record %q: %v", lines[len(lines)-1], err)
	}

	return record
}
