package dispatch_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/dispatch"
	"github.com/forkcloser/hermetic/internal/rootdir"
)

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
		{name: "windows own name, as typed", args: []string{`C:\r\bin\HERMETIC.EXE`, "version"}, goos: "windows"},
		{name: "windows own name, mixed", args: []string{`Hermetic.Exe`}, goos: "windows"},
		{
			name: "windows tool", args: []string{`C:\r\bin\limen.exe`, "version"}, goos: "windows",
			tool: "limen", toolArgs: []string{"version"},
		},
		{
			name: "windows tool, as typed", args: []string{`C:\r\bin\LIMEN.EXE`, "version"}, goos: "windows",
			tool: "LIMEN", toolArgs: []string{"version"},
		},
		{
			name:     "windows tool named by its extension alone",
			args:     []string{`.exe`},
			goos:     "windows",
			tool:     ".exe",
			toolArgs: []string{},
		},
		{name: "empty", args: nil, goos: "linux"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			tool, toolArgs := dispatch.Target(c.args, c.goos)
			if tool != c.tool {
				t.Fatalf("tool = %q, want %q", tool, c.tool)
			}

			if tool != "" && !slices.Equal(toolArgs, c.toolArgs) {
				t.Fatalf("args = %q, want %q", toolArgs, c.toolArgs)
			}
		})
	}
}

func TestDispatchedEveryNameShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		argv0, goos, tool string
	}{
		// The binary's own name, bare and under every path shape.
		{"hermetic", "linux", ""},
		{"./hermetic", "linux", ""},
		{"/r/bin/hermetic", "darwin", ""},
		{`C:\r\bin\hermetic.exe`, "windows", ""},
		{`C:/r/bin/hermetic.exe`, "windows", ""},
		{`hermetic.exe`, "windows", ""},
		{`C:\r\bin\Hermetic.exe`, "windows", ""},
		// A tool, the same shapes.
		{"limen", "linux", "limen"},
		{"./limen", "linux", "limen"},
		{"/r/bin/limen", "darwin", "limen"},
		{`C:\r\bin\limen.exe`, "windows", "limen"},
		{`C:/r/bin/limen.exe`, "windows", "limen"},
		{`limen.exe`, "windows", "limen"},
		// A backslash is a name character off Windows, a separator on it.
		{`a\b`, "linux", `a\b`},
		{`a\b`, "windows", "b"},
		// A name that merely contains the binary's.
		{"/r/bin/hermetic-helper", "linux", "hermetic-helper"},
		{"/r/bin/nothermetic", "linux", "nothermetic"},
		// A name right under the root of the filesystem.
		{"/limen", "linux", "limen"},
		{"/hermetic", "linux", ""},
		{`\limen.exe`, "windows", "limen"},
		// A path that ends in a separator names nothing: the binary itself.
		{"/r/bin/", "linux", ""},
		{"", "linux", ""},
		// .exe is Windows's: elsewhere it is part of the tool's name.
		{"/r/bin/limen.exe", "linux", "limen.exe"},
		{"/r/bin/hermetic.exe", "linux", "hermetic.exe"},
	}

	for _, c := range cases {
		tool, args := dispatch.Target([]string{c.argv0, "x", "y"}, c.goos)
		if tool != c.tool {
			t.Errorf("Target(%q, %s) = %q, want %q", c.argv0, c.goos, tool, c.tool)
		}

		if tool != "" && !slices.Equal(args, []string{"x", "y"}) {
			t.Errorf("Target(%q, %s) args = %q", c.argv0, c.goos, args)
		}
	}
}

func FuzzTarget(f *testing.F) {
	f.Add("/r/bin/hermetic", "a", true)
	f.Add(`C:\r\bin\limen.exe`, "", false)
	f.Add("hermetic/", "x", true)

	f.Fuzz(func(t *testing.T, argv0, arg string, unix bool) {
		goos := "linux"
		if !unix {
			goos = "windows"
		}

		tool, args := dispatch.Target([]string{argv0, arg}, goos)

		// A tool is the last path element, never the binary's name, never
		// empty, never a path; its arguments are the rest, untouched.
		if tool == "" {
			return
		}

		if tool == rootdir.Name || strings.ContainsAny(tool, "/") ||
			(goos == "windows" && strings.ContainsAny(tool, `\`)) {
			t.Fatalf("Target(%q, %s) = %q", argv0, goos, tool)
		}

		if !strings.HasSuffix(argv0, tool) && !(goos == "windows" && invokedWithExe(argv0, tool)) {
			t.Fatalf("Target(%q, %s) = %q, not the name invoked", argv0, goos, tool)
		}

		if !slices.Equal(args, []string{arg}) {
			t.Fatalf("args = %q, want %q", args, []string{arg})
		}
	})
}

// invokedWithExe reports whether argv0 is tool followed by ".exe" in any case,
// as a Windows caller may spell it.
func invokedWithExe(argv0, tool string) bool {
	const ext = len(".exe")

	return len(argv0) >= len(tool)+ext &&
		strings.EqualFold(argv0[len(argv0)-ext:], ".exe") &&
		strings.HasSuffix(argv0[:len(argv0)-ext], tool)
}

func TestToolEnvironment(t *testing.T) {
	t.Parallel()

	got := dispatch.Environment([]string{
		"AQUA_ROOT_DIR=/theirs", "COSIGN_PASSWORD=secret", "GITHUB_TOKEN=shell", "HERMETIC_ROOT_DIR=/r", "PATH=/bin",
	})
	want := []string{"COSIGN_PASSWORD=secret", "GITHUB_TOKEN=shell", "HERMETIC_ROOT_DIR=/r", "PATH=/bin"}

	if !slices.Equal(got, want) {
		t.Fatalf("tool environment = %v, want %v", got, want)
	}
}

// The tool's environment is the shell's minus every AQUA_* entry and nothing
// else: order, duplicates and malformed entries all kept.
func TestToolEnvironmentKeepsEverythingElse(t *testing.T) {
	t.Parallel()

	environ := []string{
		"AQUA_ROOT_DIR=/theirs", "PATH=/a", "AQUA_=x", "", "=x", "NOEQUALS", "COSIGN_PASSWORD=p",
		"AQUA=kept", "aqua_root_dir=kept", "PATH=/b", "HERMETIC_ROOT_DIR=/r", "AQUA_GLOBAL_CONFIG=g",
		"GITHUB_TOKEN=t", "XAQUA_ROOT_DIR=kept",
	}
	want := []string{
		"PATH=/a", "", "=x", "NOEQUALS", "COSIGN_PASSWORD=p", "AQUA=kept", "aqua_root_dir=kept", "PATH=/b",
		"HERMETIC_ROOT_DIR=/r", "GITHUB_TOKEN=t", "XAQUA_ROOT_DIR=kept",
	}

	if got := dispatch.Environment(environ); !slices.Equal(got, want) {
		t.Fatalf("tool environment = %q, want %q", got, want)
	}

	if got := dispatch.Environment(nil); len(got) != 0 {
		t.Fatalf("an empty environment gave %q", got)
	}
}

func FuzzToolEnvironment(f *testing.F) {
	f.Add("AQUA_ROOT_DIR=/theirs\nPATH=/bin\nCOSIGN_X=1")
	f.Add("\n=\nNOEQ\nAQUA_=x\nAQUA=kept")

	f.Fuzz(func(t *testing.T, raw string) {
		environ := strings.Split(raw, "\n")
		got := dispatch.Environment(environ)

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
