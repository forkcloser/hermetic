package hermetic_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// The bounded walks: every shape of an invocation name on both platforms,
// every shape of an environment entry, nested projects, and the mirror
// over random bins, each held to what docs/design.md says ("It dispatches
// on argv[0]", "It controls the library's environment", "The project
// directory", "It keeps its own bin").

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
		tool, args := hermetic.Dispatched([]string{c.argv0, "x", "y"}, c.goos)
		if tool != c.tool {
			t.Errorf("Dispatched(%q, %s) = %q, want %q", c.argv0, c.goos, tool, c.tool)
		}

		if tool != "" && !slices.Equal(args, []string{"x", "y"}) {
			t.Errorf("Dispatched(%q, %s) args = %q", c.argv0, c.goos, args)
		}
	}
}

// Every entry shape a process environment can carry, and what the design
// says of each: scrubbed, mapped, refused, or passed by.
func TestLibraryEnvironmentEveryEntryShape(t *testing.T) {
	t.Parallel()

	root := hermetic.Root(t.TempDir())

	refused := []string{
		"HERMETIC_=x",              // the bare prefix is not a known name
		"HERMETIC_ROOT=x",          // a prefix of a known name
		"HERMETIC_ROOT_DIR_X=x",    // a known name as a prefix
		"HERMETIC_GITHUB_TOKEN2=x", // a known name with a suffix
		"HERMETIC_X",               // no "=": the key is the whole entry
	}

	for _, entry := range refused {
		if _, _, err := hermetic.LibraryEnvironment(root, []string{entry}, ""); err == nil {
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
		set, unset, err := hermetic.LibraryEnvironment(root, []string{entry}, "")
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
		_, unset, err := hermetic.LibraryEnvironment(root, []string{entry}, "")
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
	set, _, err := hermetic.LibraryEnvironment(root, []string{
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
		set, _, err := hermetic.LibraryEnvironment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=" + v}, "")
		if err != nil || set["AQUA_GLOBAL_CONFIG"] != string(os.PathListSeparator) {
			t.Errorf("NO_GLOBAL_CONFIG=%s: %v %q", v, err, set["AQUA_GLOBAL_CONFIG"])
		}
	}

	for _, v := range []string{"false", "0", "F"} {
		set, _, err := hermetic.LibraryEnvironment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=" + v}, "")
		if err != nil || set["AQUA_GLOBAL_CONFIG"] != root.GlobalConfig() {
			t.Errorf("NO_GLOBAL_CONFIG=%s: %v %q", v, err, set["AQUA_GLOBAL_CONFIG"])
		}
	}

	for _, v := range []string{"yes", "no", "on", " true", "true "} {
		if _, _, err := hermetic.LibraryEnvironment(root, []string{"HERMETIC_NO_GLOBAL_CONFIG=" + v}, ""); err == nil {
			t.Errorf("NO_GLOBAL_CONFIG=%q was accepted", v)
		}
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

	if got := hermetic.ToolEnvironment(environ); !slices.Equal(got, want) {
		t.Fatalf("tool environment = %q, want %q", got, want)
	}

	if got := hermetic.ToolEnvironment(nil); len(got) != 0 {
		t.Fatalf("an empty environment gave %q", got)
	}
}

// The nearest project wins, a project is its config and not its directory,
// and a working directory that does not exist is walked from where it would
// be.
func TestFindProjectNearestWins(t *testing.T) {
	t.Parallel()

	top := t.TempDir()
	outer := filepath.Join(top, "outer")
	inner := filepath.Join(outer, "a", "inner")
	below := filepath.Join(inner, "b", "c")

	for _, project := range []string{outer, inner} {
		write(t, filepath.Join(project, hermetic.ProjectDir, hermetic.ProjectConfig), "packages: []\n")
	}

	if err := os.MkdirAll(below, 0o750); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		below:                     filepath.Join(inner, hermetic.ProjectDir),
		inner:                     filepath.Join(inner, hermetic.ProjectDir),
		filepath.Join(outer, "a"): filepath.Join(outer, hermetic.ProjectDir),
		outer:                     filepath.Join(outer, hermetic.ProjectDir),
		top:                       "",
		filepath.Join(below, "does", "not", "exist"): filepath.Join(inner, hermetic.ProjectDir),
		filepath.Join(inner, hermetic.ProjectDir):    filepath.Join(inner, hermetic.ProjectDir),
	}

	for wd, want := range cases {
		got, err := hermetic.FindProject(wd)
		if err != nil {
			t.Errorf("%s: %v", wd, err)

			continue
		}

		if got != want {
			t.Errorf("FindProject(%s) = %q, want %q", wd, got, want)
		}
	}
}

// The mirror over random bins: whatever the bin held, after one pass every
// exposed name is a link to the binary, a foreign entry under an unexposed
// name is untouched, a link of ours under an unexposed name is gone, aqua's
// own bin is never written, and a second pass changes nothing.
func TestMirrorConverges(t *testing.T) {
	t.Parallel()

	names := []string{"a", "b", "c", "d", "e", "aqua", "hermetic"}
	kinds := []string{"absent", "ours", "ours-absolute", "foreign-link", "foreign-file", "foreign-dir", "dangling"}

	for seed := range 64 {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			t.Parallel()

			rng := rand.New(rand.NewPCG(uint64(seed), 7))

			root, binary := mirrorRoot(t, "linux")
			aquaBin := filepath.Join(root.AquaRoot(), "bin")
			exposed := map[string]bool{}
			before := map[string]string{}

			for _, name := range names {
				if rng.IntN(2) == 0 {
					exposed[name] = true

					write(t, filepath.Join(aquaBin, name), "")
				}

				if name == hermetic.Name {
					continue
				}

				kind := kinds[rng.IntN(len(kinds))]
				path := filepath.Join(root.Bin(), name)

				switch kind {
				case "ours":
					symlink(t, hermetic.Name, path)
				case "ours-absolute":
					symlink(t, binary, path)
				case "foreign-link":
					symlink(t, "/usr/bin/true", path)
				case "foreign-file":
					write(t, path, "theirs")
				case "foreign-dir":
					if err := os.Mkdir(path, 0o750); err != nil {
						t.Fatal(err)
					}
				case "dangling":
					symlink(t, filepath.Join(t.TempDir(), "gone"), path)
				}

				before[name] = kind
			}

			aquaBefore := listing(t, aquaBin)

			if err := hermetic.Mirror(root, "linux"); err != nil {
				t.Fatalf("%v: %v", before, err)
			}

			check := func(pass string) {
				t.Helper()

				for _, name := range names {
					path := filepath.Join(root.Bin(), name)
					target, err := os.Readlink(path)
					linked := err == nil && (target == hermetic.Name || target == binary)

					switch {
					case name == hermetic.Name:
						// The binary itself, never touched.
						if info, statErr := os.Lstat(path); statErr != nil || info.Mode()&os.ModeSymlink != 0 {
							t.Errorf("%s: the binary: %v", pass, statErr)
						}
					case exposed[name] && name != "aqua":
						if !linked {
							t.Errorf(
								"%s: %s (%s) is not a link to the binary: %q %v",
								pass,
								name,
								before[name],
								target,
								err,
							)
						}
					case before[name] == "ours" || before[name] == "ours-absolute" || before[name] == "absent":
						if _, err := os.Lstat(path); err == nil {
							t.Errorf("%s: %s (%s) is still there", pass, name, before[name])
						}
					default:
						// Foreign, under an unexposed name (aqua included): as it was.
						if linked {
							t.Errorf("%s: %s (%s) became a link of ours", pass, name, before[name])
						}

						if _, err := os.Lstat(path); err != nil {
							t.Errorf("%s: %s (%s) is gone", pass, name, before[name])
						}
					}
				}

				if got := listing(t, aquaBin); got != aquaBefore {
					t.Errorf("%s: aqua's bin was written", pass)
				}
			}

			check("first pass")

			after := listing(t, root.Bin())

			if err := hermetic.Mirror(root, "linux"); err != nil {
				t.Fatal(err)
			}

			check("second pass")

			if got := listing(t, root.Bin()); got != after {
				t.Error("the second pass changed the bin")
			}
		})
	}
}

func symlink(t *testing.T, target, path string) {
	t.Helper()

	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// listing is a digest of a directory: every name with its mode and, for a
// link, its target.
func listing(t *testing.T, dir string) string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var lines []string

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}

		target := ""
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ = os.Readlink(filepath.Join(dir, entry.Name()))
		}

		lines = append(lines, entry.Name()+" "+info.Mode().String()+" "+target)
	}

	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))

	return hex.EncodeToString(sum[:])
}

// The root's derived paths on both platforms.
func TestRootPaths(t *testing.T) {
	t.Parallel()

	root := hermetic.Root(filepath.Join("r", "x"))

	for _, c := range []struct{ name, got, want string }{
		{"bin", root.Bin(), filepath.Join("r", "x", "bin")},
		{"aqua root", root.AquaRoot(), filepath.Join("r", "x", "store")},
		{"global config", root.GlobalConfig(), filepath.Join("r", "x", "hermetic.yaml")},
		{"policy", root.Policy(), filepath.Join("r", "x", "policy.yaml")},
		{"binary", root.Binary("linux"), filepath.Join("r", "x", "bin", "hermetic")},
		{"binary on windows", root.Binary("windows"), filepath.Join("r", "x", "bin", "hermetic.exe")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
