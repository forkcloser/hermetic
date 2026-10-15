package hermetic_test

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/suzuki-shunsuke/slog-util/slogutil"
	"github.com/suzuki-shunsuke/urfave-cli-v3-util/urfave"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// TestSurface holds the binary to the commands and global flags it exposes, as
// its own help lists them, and refuses what is cut: a command, a verifier
// switch, a deprecated flag, a policy subcommand.
func TestSurface(t *testing.T) {
	// Run resolves the root, applies the library's environment to the process
	// and walks the working directory for a project: a scratch root, an empty
	// working directory, and no Parallel.
	t.Setenv(hermetic.RootEnv, t.TempDir())
	t.Chdir(t.TempDir())

	help, err := invoke(t, "help")
	if err != nil {
		t.Fatalf("help: %v\n%s", err, help)
	}

	commands := []string{
		"generate",
		"help",
		"install",
		"policy",
		"root-dir",
		"update",
		"update-checksum",
		"version",
		"which",
	}
	if got := section(help, "COMMANDS:"); !slices.Equal(got, commands) {
		t.Fatalf("commands = %v, want %v\n%s", got, commands, help)
	}

	flags := []string{"--config", "--help", "--log-level", "--version"}
	if got := section(help, "GLOBAL OPTIONS:"); !slices.Equal(got, flags) {
		t.Fatalf("global flags = %v, want %v\n%s", got, flags, help)
	}

	policy, err := invoke(t, "policy", "help")
	if err != nil {
		t.Fatalf("policy help: %v\n%s", err, policy)
	}

	subcommands := []string{"allow", "deny"}
	if got := section(policy, "COMMANDS:"); !slices.Equal(got, subcommands) {
		t.Fatalf("policy subcommands = %v, want %v\n%s", got, subcommands, policy)
	}

	for _, args := range [][]string{
		{"exec", "--", "true"},
		{"--disable-cosign", "help"},
		{"--trace", "t", "help"},
		{"install", "--test"},
		{"update-checksum", "--deep"},
		{"policy", "init"},
	} {
		if out, err := invoke(t, args...); err == nil {
			t.Fatalf("%q was accepted\n%s", args, out)
		}
	}
}

// invoke runs the binary's Run with the arguments and returns what it wrote
// on its stdout, the library's own logging discarded.
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

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = devnull.Close() }()

	logger := slogutil.New(&slogutil.InputNew{Name: hermetic.Name, Version: "test", Out: devnull})
	env := &urfave.Env{
		Program: hermetic.Name,
		Version: "test",
		Stdin:   os.Stdin,
		Stdout:  writer,
		Stderr:  writer,
		Getenv:  os.Getenv,
		Args:    append([]string{hermetic.Name}, args...),
	}

	runErr := hermetic.Run(t.Context(), logger, env)

	_ = writer.Close()
	out := <-captured
	_ = reader.Close()

	return out, runErr
}

// section is the sorted first words of the help section titled, up to its
// blank line, the alias separator stripped.
func section(help, title string) []string {
	var names []string

	in := false

	for line := range strings.SplitSeq(help, "\n") {
		switch {
		case strings.HasPrefix(line, title):
			in = true
		case in && strings.TrimSpace(line) == "":
			slices.Sort(names)

			return names
		case in:
			names = append(names, strings.TrimSuffix(strings.Fields(line)[0], ","))
		}
	}

	slices.Sort(names)

	return names
}
