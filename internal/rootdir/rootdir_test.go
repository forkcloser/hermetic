package rootdir_test

import (
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/filesystem/dirs"

	"github.com/forkcloser/hermetic/internal/rootdir"
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
	got, err := rootdir.From(getenv(map[string]string{rootdir.Env: filepath.FromSlash("/r/")}))
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
	got, err = rootdir.From(getenv(map[string]string{rootdir.Env: "rel"}))
	if err != nil {
		t.Fatal(err)
	}

	if !filepath.IsAbs(string(got)) || filepath.Base(string(got)) != "rel" {
		t.Fatalf("relative root: %q", got)
	}

	// Without the variable, the platform's data directory for the name, as
	// primordium resolves it.
	got, err = rootdir.From(getenv(nil))
	if err != nil {
		t.Fatal(err)
	}

	dirs.SetAppName(rootdir.Name)

	want, err = dirs.DataDir()
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Fatalf("default: root = %q, want primordium's %q", got, want)
	}

	if filepath.Base(want) != rootdir.Name {
		t.Fatalf("the default root is not named after the binary: %q", want)
	}
}

// The root's derived paths on both platforms.
func TestRootPaths(t *testing.T) {
	t.Parallel()

	root := rootdir.Root(filepath.Join("r", "x"))

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
