package hermetic_test

import (
	"errors"
	"io"
	"os"
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
