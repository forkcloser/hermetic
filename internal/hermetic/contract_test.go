package hermetic_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// The binary's contract at the process boundary, as docs/design.md states
// it: the five HERMETIC_* variables and the refusal of any other, the root
// the shell cannot move, the policy records `policy allow` and `policy
// deny` keep and the warning that follows them, and the environment a
// dispatched tool gets. Each case runs the binary in a child under a
// scratch root; nothing reaches a registry.

// binary runs the test binary as hermetic in dir with env, and returns its
// combined output and the error, which carries the exit status.
func binary(t *testing.T, path, dir string, env []string, args ...string) (string, error) {
	t.Helper()

	run := exec.CommandContext(t.Context(), path, args...)
	run.Dir, run.Env = dir, env

	out, err := run.CombinedOutput()

	return string(out), err
}

// exitStatus is the child's exit status, or -1 when it did not run.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}

	return -1
}

func TestEnvironmentAtTheBinary(t *testing.T) {
	t.Parallel()

	path := binaryNamed(t)
	root := t.TempDir()
	// Canonical: the child resolves its working directory through the
	// filesystem, and a temporary directory may sit behind a symlink.
	outside := canonical(t, t.TempDir())

	base := append(os.Environ(), asBinary+"=1", hermetic.RootEnv+"="+root)

	cases := []struct {
		name   string
		env    []string
		args   []string
		status int
		stdout string // exact, trimmed, when set
		says   string // a substring of the output, when set
	}{
		{
			name: "an unknown variable is refused, named",
			env:  []string{"HERMETIC_DISABLE_POLICY=true"}, args: []string{"root-dir"},
			status: 1, says: "HERMETIC_DISABLE_POLICY",
		},
		{
			name: "a misspelling is refused, named",
			env:  []string{"HERMETIC_ROOTDIR=" + root}, args: []string{"root-dir"},
			status: 1, says: "HERMETIC_ROOTDIR",
		},
		{
			name: "no-global-config must be a boolean",
			env:  []string{"HERMETIC_NO_GLOBAL_CONFIG=maybe"}, args: []string{"root-dir"},
			status: 1, says: "maybe",
		},
		{
			name: "the log level must be one of four, under the binary's own name",
			env:  []string{"HERMETIC_LOG_LEVEL=loud"}, args: []string{"root-dir"},
			status: 1, says: "loud",
		},
		{
			name: "the root is the binary's, whatever AQUA_ROOT_DIR says",
			env:  []string{"AQUA_ROOT_DIR=" + filepath.Join(outside, "theirs")}, args: []string{"root-dir"},
			status: 0, stdout: filepath.Join(root, "store"),
		},
		{
			name: "the root follows HERMETIC_ROOT_DIR, made absolute",
			env:  []string{hermetic.RootEnv + "=rel"}, args: []string{"root-dir"},
			status: 0, stdout: filepath.Join(outside, "rel", "store"),
		},
		{
			name: "a version needs none of it, and is the build's",
			env: []string{
				"HERMETIC_DISABLE_POLICY=true",
				"HERMETIC_NO_GLOBAL_CONFIG=maybe",
			},
			args:   []string{"version"},
			status: 0,
			stdout: buildVersion(),
		},
		{
			name:   "the flag answers the same",
			args:   []string{"--version"},
			status: 0,
			stdout: buildVersion(),
		},
		{
			name: "the usage needs none of it",
			env:  []string{"HERMETIC_DISABLE_POLICY=true"}, args: []string{"--help"},
			status: 0, says: "Commands:",
		},
		{
			name: "which falls back to the PATH for a name no config has",
			args: []string{"which", "sh"}, status: 0, says: string(os.PathSeparator) + "sh",
		},
		{
			name: "which has no version for a name the library does not manage",
			args: []string{"which", "--show-version", "sh"}, status: 1, says: "not managed",
		},
		{
			name: "which refuses a name nothing has",
			args: []string{"which", "no-such-tool-anywhere"}, status: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			out, err := binary(t, path, outside, append(slices.Clone(base), c.env...), c.args...)
			if got := exitStatus(err); got != c.status {
				t.Fatalf("exit status %d, want %d\n%s", got, c.status, out)
			}

			if c.stdout != "" && strings.TrimSpace(lastLine(out)) != c.stdout {
				t.Fatalf("stdout %q, want %q", strings.TrimSpace(out), c.stdout)
			}

			if c.says != "" && !strings.Contains(out, c.says) {
				t.Fatalf("output lacks %q:\n%s", c.says, out)
			}
		})
	}
}

// buildVersion is what the test binary, run as hermetic with nothing
// stamped, must report: the module version its build recorded, else
// "unknown".
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}

	return "unknown"
}

// canonical is path with every symlink resolved.
func canonical(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	return resolved
}

// lastLine is the last non-empty line of an output: a command's answer,
// after any log record the run wrote first.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")

	return lines[len(lines)-1]
}

// The records the gate keeps, through the binary's own commands: allowed, the
// policy is used and nothing is said; changed, it is dropped and said to have
// changed; denied, it is dropped and nothing is said; the deny record gone,
// the warning returns; and `policy allow` itself never warns about the
// record it is about to write.
func TestPolicyRecordsAtTheBinary(t *testing.T) {
	t.Parallel()

	path := binaryNamed(t)
	root := t.TempDir()
	// Canonical: the record is keyed by the path the child resolves.
	project := filepath.Join(canonical(t, t.TempDir()), "repo")
	policy := filepath.Join(project, hermetic.ProjectDir, hermetic.ProjectPolicy)
	write(t, filepath.Join(project, hermetic.ProjectDir, hermetic.ProjectConfig), "packages: []\n")
	write(t, policy, "registries:\n  - type: standard\n")

	env := append(os.Environ(), asBinary+"=1", hermetic.RootEnv+"="+root)
	record := filepath.Join(root, "store", "policies", strings.ReplaceAll(policy, ":", "_"))
	denied := filepath.Join(root, "store", "policy-warnings", strings.ReplaceAll(policy, ":", "_"))

	run := func(args ...string) string {
		t.Helper()

		out, err := binary(t, path, project, env, args...)
		if err != nil {
			t.Fatalf("%q: %v\n%s", args, err, out)
		}

		return out
	}

	ignored := "ignored until allowed"
	changed := "changed since it was allowed"

	if out := run("root-dir"); !strings.Contains(out, ignored) {
		t.Fatalf("no record: the gate did not warn:\n%s", out)
	}

	if out := run(
		"policy",
		"allow",
		filepath.Join(hermetic.ProjectDir, hermetic.ProjectPolicy),
	); strings.Contains(
		out,
		ignored,
	) {
		t.Fatalf("policy allow warned about the record it writes:\n%s", out)
	}

	if body, err := os.ReadFile(record); err != nil || string(body) != "registries:\n  - type: standard\n" {
		t.Fatalf("the allow record: %q, %v", body, err)
	}

	if out := run("root-dir"); strings.Contains(out, ignored) || strings.Contains(out, changed) {
		t.Fatalf("allowed: the gate warned:\n%s", out)
	}

	write(t, policy, "registries:\n  - type: standard\npackages: []\n")

	if out := run("root-dir"); !strings.Contains(out, changed) {
		t.Fatalf("changed: the gate did not say so:\n%s", out)
	}

	if out := run(
		"policy",
		"deny",
		filepath.Join(hermetic.ProjectDir, hermetic.ProjectPolicy),
	); strings.Contains(
		out,
		ignored,
	) {
		t.Fatalf("policy deny warned:\n%s", out)
	}

	if _, err := os.Stat(record); err == nil {
		t.Fatal("the allow record survived the deny")
	}

	if _, err := os.Stat(denied); err != nil {
		t.Fatalf("the deny record: %v", err)
	}

	if out := run("root-dir"); strings.Contains(out, ignored) || strings.Contains(out, changed) {
		t.Fatalf("denied: the gate warned:\n%s", out)
	}

	if err := os.Remove(denied); err != nil {
		t.Fatal(err)
	}

	if out := run("root-dir"); !strings.Contains(out, ignored) {
		t.Fatalf("deny record gone: the gate did not warn:\n%s", out)
	}

	// Outside the project the gate has nothing to say, and the allow record
	// of another project is not consulted.
	if out, err := binary(t, path, t.TempDir(), env, "root-dir"); err != nil || strings.Contains(out, ignored) {
		t.Fatalf("outside any project: %v\n%s", err, out)
	}
}

// A dispatched tool gets the shell's environment minus every AQUA_* entry:
// the cosign and GitHub values a shell holds reach it, nothing of the
// library's does.
func TestDispatchedToolEnvironmentAtTheBinary(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the script that prints the environment is a shell's")
	}

	self, execErr := os.Executable()
	if execErr != nil {
		t.Fatal(execErr)
	}

	root := hermetic.Root(t.TempDir())
	if err := os.MkdirAll(root.Bin(), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(self, root.Binary(runtime.GOOS)); err != nil {
		t.Fatal(err)
	}

	system := t.TempDir()
	write(t, filepath.Join(system, "envdump"), "#!/bin/sh\nenv\n")

	if err := os.Chmod(filepath.Join(system, "envdump"), 0o700); err != nil {
		t.Fatal(err)
	}

	link(t, root, "envdump")

	path := strings.Join([]string{root.Bin(), system, "/bin", "/usr/bin"}, string(os.PathListSeparator))
	env := append(os.Environ(),
		asBinary+"=1", hermetic.RootEnv+"="+string(root), "PATH="+path,
		"AQUA_ROOT_DIR=/theirs", "AQUA_GLOBAL_CONFIG=/theirs.yaml", "AQUA_SOMETHING=x",
		"COSIGN_PASSWORD=kept", "GITHUB_TOKEN=kept", "TUF_ROOT=/kept", "GH_TOKEN=kept",
		"HERMETIC_GITHUB_TOKEN=ours",
	)

	out, err := binary(t, filepath.Join(root.Bin(), "envdump"), t.TempDir(), env, "a", "b")
	if err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out)
	}

	got := map[string]string{}

	for line := range strings.SplitSeq(out, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}

	for key, want := range map[string]string{
		"COSIGN_PASSWORD": "kept", "GITHUB_TOKEN": "kept", "TUF_ROOT": "/kept", "GH_TOKEN": "kept",
		"HERMETIC_GITHUB_TOKEN": "ours", "HERMETIC_ROOT_DIR": string(root),
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}

	for key := range got {
		if strings.HasPrefix(key, "AQUA_") {
			t.Errorf("%s reached the tool", key)
		}
	}
}
