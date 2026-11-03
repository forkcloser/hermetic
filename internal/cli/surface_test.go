package cli_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/app/shutdown"

	"github.com/forkcloser/hermetic/internal/cli"
	"github.com/forkcloser/hermetic/internal/projectdir"
	"github.com/forkcloser/hermetic/internal/rootdir"
)

// binName is the name cmd/hermetic gives the binary.
const binName = "hermetic"

// TestSurface holds the binary to the commands and global flags it exposes, as
// its own help lists them, and refuses what is cut: a command, a verifier
// switch, a profiling flag, a deprecated flag, a policy subcommand.
func TestSurface(t *testing.T) {
	// Run resolves the root, applies the library's environment to the process
	// and walks the working directory for a project: a scratch root, an empty
	// working directory, and no Parallel.
	t.Setenv(rootDirVar, t.TempDir())
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

	env := &cli.Env{
		Name:    binName,
		Version: "test",
		Stdout:  writer,
		Stderr:  writer,
		Args:    append([]string{binName}, args...),
	}

	runErr := cli.Run(t.Context(), env)

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

	t.Setenv(rootDirVar, "")
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
// directory outside the project an unallowed policy stops the run, the allow
// command for that policy the whole of stderr, exit status 1; a denied policy
// stops a run that names its config the same way, where the walk through the
// same project is silent; a manifest with no policy beside it, a scratch copy,
// carries none; the root's own manifest
// names no project, its policy being the global one the library reads
// already; and without a named config a working directory outside any project
// sees no gate at all.
func TestNamedConfigCarriesItsProject(t *testing.T) {
	root := rootdir.Root(t.TempDir())
	t.Setenv(rootDirVar, string(root))
	write(t, root.GlobalConfig(), "packages: []\n")
	write(t, root.Policy(), "registries:\n  - type: standard\n")

	project := filepath.Join(t.TempDir(), "repo", projectdir.Dir)
	write(t, filepath.Join(project, projectdir.Config), "packages: []\n")
	write(t, filepath.Join(project, projectdir.Policy), "registries:\n  - type: standard\n")

	scratch := filepath.Join(t.TempDir(), "scratch", projectdir.Config)
	write(t, scratch, "packages: []\n")

	// A second project, denied: the record `policy deny` writes, keyed by the
	// policy's path (the drive's colon an underscore on Windows).
	denied := filepath.Join(t.TempDir(), "denied", projectdir.Dir)
	deniedPolicy := filepath.Join(denied, projectdir.Policy)
	write(t, filepath.Join(denied, projectdir.Config), "packages: []\n")
	write(t, deniedPolicy, "registries:\n  - type: standard\n")
	write(t, filepath.Join(root.AquaRoot(), "policy-warnings", strings.ReplaceAll(deniedPolicy, ":", "_")), "")

	outside := t.TempDir()

	type run struct {
		name   string
		dir    string
		env    string
		args   []string
		policy string
	}

	runs := []run{
		{
			"named by the variable",
			outside,
			filepath.Join(project, projectdir.Config),
			[]string{"root-dir"},
			filepath.Join(project, projectdir.Policy),
		},
		{
			"named by the flag",
			outside,
			"",
			[]string{"-c", filepath.Join(project, projectdir.Config), "root-dir"},
			filepath.Join(project, projectdir.Policy),
		},
		{
			"denied, named by the variable",
			outside,
			filepath.Join(denied, projectdir.Config),
			[]string{"root-dir"},
			deniedPolicy,
		},
		{
			"denied, named by the flag",
			outside,
			"",
			[]string{"-c", filepath.Join(denied, projectdir.Config), "root-dir"},
			deniedPolicy,
		},
		{"denied, by the walk", filepath.Dir(denied), "", []string{"root-dir"}, ""},
		{"a scratch copy", outside, scratch, []string{"root-dir"}, ""},
		{"the root's own manifest, by the variable", outside, root.GlobalConfig(), []string{"root-dir"}, ""},
		{"the root's own manifest, by the flag", outside, "", []string{"-c", root.GlobalConfig(), "root-dir"}, ""},
		{"nothing named, outside any project", outside, "", []string{"root-dir"}, ""},
	}

	// The root's manifest under another spelling of the root, as a root under
	// macOS's /tmp is reached: the same file, so still no project. Windows
	// runners may lack the privilege a symlink needs.
	if runtime.GOOS != "windows" {
		linked := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(string(root), linked); err != nil {
			t.Fatal(err)
		}

		runs = append(runs, run{
			"the root's own manifest, through a symlinked root",
			outside,
			filepath.Join(linked, projectdir.Config),
			[]string{"root-dir"},
			"",
		})
	}

	for _, c := range runs {
		t.Chdir(c.dir)
		t.Setenv("HERMETIC_CONFIG", c.env)

		out, err := invoke(t, c.args...)

		if c.policy == "" {
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.name, err, out)
			}

			continue
		}

		exit, asked := errors.AsType[shutdown.ExitError](err)
		if !asked || exit != 1 {
			t.Fatalf("%s: err = %v, want exit status 1\n%s", c.name, err, out)
		}

		if want := "hermetic policy allow " + c.policy + "\n"; out != want {
			t.Fatalf("%s: the run said %q, want %q", c.name, out, want)
		}
	}
}
