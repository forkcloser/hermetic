package hermetic_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/app/shutdown"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// TestSurface holds the binary to the commands and global flags it exposes, as
// its own help lists them, and refuses what is cut: a command, a verifier
// switch, a profiling flag, a deprecated flag, a policy subcommand.
func TestSurface(t *testing.T) {
	// Run resolves the root, applies the library's environment to the process
	// and walks the working directory for a project: a scratch root, an empty
	// working directory, and no Parallel.
	t.Setenv(hermetic.RootEnv, t.TempDir())
	t.Chdir(t.TempDir())

	help, err := invoke(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v\n%s", err, help)
	}

	commands := []string{
		"generate",
		"install",
		"policy",
		"root-dir",
		"update",
		"update-checksum",
		"version",
		"which",
	}
	if got := section(help, "Commands:", 0); !slices.Equal(got, commands) {
		t.Fatalf("commands = %v, want %v\n%s", got, commands, help)
	}

	flags := []string{"--config", "--help", "--log-level", "--version"}
	if got := section(help, "Flags:", 0); !slices.Equal(got, flags) {
		t.Fatalf("global flags = %v, want %v\n%s", got, flags, help)
	}

	policy, err := invoke(t, "policy", "--help")
	if err != nil {
		t.Fatalf("policy --help: %v\n%s", err, policy)
	}

	subcommands := []string{"allow", "deny"}
	if got := section(policy, "Commands:", 1); !slices.Equal(got, subcommands) {
		t.Fatalf("policy subcommands = %v, want %v\n%s", got, subcommands, policy)
	}

	for _, args := range [][]string{
		{"exec", "--", "true"},
		{"--disable-cosign", "--help"},
		{"--trace", "t", "--help"},
		{"install", "--test"},
		{"update-checksum", "--deep"},
		{"policy", "init"},
		{"help"},
	} {
		out, err := invoke(t, args...)

		var asked shutdown.ExitError
		if !errors.As(err, &asked) || asked != 1 {
			t.Fatalf("%q: err = %v, want exit status 1\n%s", args, err, out)
		}
	}

	// A version is answered by the flag and by the command alike, as the
	// toolset's scripts pass one or the other.
	for _, args := range [][]string{{"--version"}, {"version"}} {
		out, err := invoke(t, args...)
		if err != nil || strings.TrimSpace(out) != "test" {
			t.Fatalf("%q: out = %q, err = %v, want \"test\"", args, out, err)
		}
	}
}

// invoke runs the binary's Run with the arguments and returns what it wrote
// on its stdout and stderr.
func invoke(t *testing.T, args ...string) (string, error) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	captured := make(chan string, 1)

	go func() {
		out, _ := io.ReadAll(reader)
		captured <- string(out)
	}()

	env := &hermetic.Env{
		Version: "test",
		Stdout:  writer,
		Stderr:  writer,
		Getenv:  os.Getenv,
		Args:    append([]string{hermetic.Name}, args...),
	}

	runErr := hermetic.Run(t.Context(), env)

	_ = writer.Close()
	out := <-captured
	_ = reader.Close()

	return out, runErr
}

// section is the sorted, distinct names a help section lists, up to the next
// section or the footer: from each entry line (a flag, which may sit under a
// short-flag column; or a command, indented two spaces where a description is
// indented four; a blank line separates entries) the long flag when it has
// one, else the word at index word of its command path, so a subcommand's own
// help, which prints the full path, reads its second word.
func section(help, title string, word int) []string {
	var names []string

	in := false

	for line := range strings.SplitSeq(help, "\n") {
		switch {
		case strings.HasPrefix(line, title):
			in = true
		case in && line != "" && !strings.HasPrefix(line, " "):
			return distinct(names)
		case in && strings.HasPrefix(line, "  ") && (strings.HasPrefix(strings.TrimSpace(line), "-") || !strings.HasPrefix(line, "    ")):
			names = append(names, entryName(line, word))
		}
	}

	return distinct(names)
}

// entryName is the name a help entry line carries: its long flag when it has
// one, else the word at index word of its command path.
func entryName(line string, word int) string {
	fields := strings.Fields(line)
	for _, field := range fields {
		if strings.HasPrefix(field, "--") {
			return strings.TrimSuffix(strings.SplitN(field, "=", 2)[0], ",")
		}
	}

	return strings.TrimSuffix(fields[word], ",")
}

func distinct(names []string) []string {
	slices.Sort(names)

	return slices.Compact(names)
}

// TestNoRootForVersionAndUsage holds the version, the usage and a usage error
// to needing no root: with the default root unmakeable, the home (and
// LOCALAPPDATA, which Windows reads first) a file, each answers, where a
// library command, which needs the root, fails on it.
func TestNoRootForVersionAndUsage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(hermetic.RootEnv, "")
	t.Setenv("HOME", file)
	t.Setenv("LOCALAPPDATA", file)
	// Linux reads this ahead of the home when it is set and absolute.
	t.Setenv("XDG_DATA_HOME", "")
	t.Chdir(t.TempDir())

	for _, args := range [][]string{{"--version"}, {"version"}, {"--help"}, {"bogus"}} {
		out, err := invoke(t, args...)

		var asked shutdown.ExitError

		switch {
		case args[0] == "bogus" && (!errors.As(err, &asked) || asked != 1):
			t.Fatalf("%q: err = %v, want exit status 1\n%s", args, err, out)
		case args[0] != "bogus" && err != nil:
			t.Fatalf("%q: %v\n%s", args, err, out)
		}
	}

	// The control: the root is still what a library command needs.
	if out, err := invoke(t, "root-dir"); err == nil {
		t.Fatalf("root-dir answered without a root: %s", out)
	}
}

// TestNamedConfigCarriesItsProject holds a run that names its config, through
// HERMETIC_CONFIG or -c, to the named config's project: the policy beside the
// manifest goes through the gate as a walked one does, so from a working
// directory outside the project the gate's warning names that policy; a
// manifest with no policy beside it, a scratch copy, carries none; the root's
// own manifest names no project, its policy being the global one the library
// reads already, so nothing is gated; and without a named config a working
// directory outside any project sees no gate at all.
func TestNamedConfigCarriesItsProject(t *testing.T) {
	root := hermetic.Root(t.TempDir())
	t.Setenv(hermetic.RootEnv, string(root))
	write(t, root.GlobalConfig(), "packages: []\n")
	write(t, root.Policy(), "registries:\n  - type: standard\n")

	project := filepath.Join(t.TempDir(), "repo", hermetic.ProjectDir)
	write(t, filepath.Join(project, hermetic.ProjectConfig), "packages: []\n")
	write(t, filepath.Join(project, hermetic.ProjectPolicy), "registries:\n  - type: standard\n")

	scratch := filepath.Join(t.TempDir(), "scratch", hermetic.ProjectConfig)
	write(t, scratch, "packages: []\n")

	t.Chdir(t.TempDir())

	type run struct {
		name   string
		env    string
		args   []string
		policy string
	}

	runs := []run{
		{
			"named by the variable",
			filepath.Join(project, hermetic.ProjectConfig),
			[]string{"root-dir"},
			filepath.Join(project, hermetic.ProjectPolicy),
		},
		{
			"named by the flag",
			"",
			[]string{"-c", filepath.Join(project, hermetic.ProjectConfig), "root-dir"},
			filepath.Join(project, hermetic.ProjectPolicy),
		},
		{"a scratch copy", scratch, []string{"root-dir"}, ""},
		{"the root's own manifest, by the variable", root.GlobalConfig(), []string{"root-dir"}, ""},
		{"the root's own manifest, by the flag", "", []string{"-c", root.GlobalConfig(), "root-dir"}, ""},
		{"nothing named, outside any project", "", []string{"root-dir"}, ""},
	}

	// The root's manifest under another spelling of the root, as a root under
	// macOS's /tmp is reached: the same file, so still no project. Windows
	// runners may lack the privilege a symlink needs.
	if runtime.GOOS != "windows" {
		linked := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(string(root), linked); err != nil {
			t.Fatal(err)
		}

		runs = append(
			runs,
			run{
				"the root's own manifest, through a symlinked root",
				filepath.Join(linked, hermetic.ProjectConfig),
				[]string{"root-dir"},
				"",
			},
		)
	}

	for _, c := range runs {
		t.Setenv(hermetic.ConfigEnv, c.env)

		warned := stderrOf(t, func() {
			if out, err := invoke(t, c.args...); err != nil {
				t.Fatalf("%s: %v\n%s", c.name, err, out)
			}
		})

		gated, found := gatedPolicy(t, warned)

		switch {
		case c.policy == "" && found:
			t.Fatalf("%s: a policy was gated where none is carried: %q\n%s", c.name, gated, warned)
		case c.policy != "" && !found:
			t.Fatalf("%s: the named config's policy was not gated:\n%s", c.name, warned)
		case c.policy != "" && gated != c.policy:
			t.Fatalf("%s: the gate names %q, want %q", c.name, gated, c.policy)
		}
	}
}

// stderrOf runs function with the process's stderr swapped for a pipe and
// returns what was written: the process-wide log handler Run installs writes
// there, not on the Env's stream.
func stderrOf(t *testing.T, function func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	captured := make(chan string, 1)

	go func() {
		out, _ := io.ReadAll(reader)
		captured <- string(out)
	}()

	stderr := os.Stderr
	os.Stderr = writer //nolint:reassign // the handler Run installs writes on os.Stderr; the swap is how a test reads it.

	function()

	os.Stderr = stderr //nolint:reassign // restored.

	_ = writer.Close()
	out := <-captured
	_ = reader.Close()

	return out
}

// gatedPolicy is the policy the gate's warning names among the JSON records
// on logged, decoded so a Windows path compares as logged, and whether there
// is one.
func gatedPolicy(t *testing.T, logged string) (string, bool) {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(logged), "\n") {
		if line == "" {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode the log record %q: %v", line, err)
		}

		msg, isText := record["msg"].(string)
		if !isText || !strings.Contains(msg, "ignored until allowed") {
			continue
		}

		policy, isPath := record["policy"].(string)
		if !isPath {
			t.Fatalf("the gate's warning carries no policy path: %s", line)
		}

		return policy, true
	}

	return "", false
}
