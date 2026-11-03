package rootdir_test

import (
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/filesystem/dirs"

	"github.com/forkcloser/hermetic/internal/rootdir"
)

// binName is the name cmd/hermetic gives the binary, and primordium's dirs.
const binName = "hermetic"

func TestRootFrom(t *testing.T) {
	// The default root is created on resolution, so the platform's base
	// directories point into a scratch home for this test (hence no Parallel).
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	// The precondition the binary meets in cli.Main.
	dirs.SetAppName(binName)

	// A named root wins, made absolute and cleaned.
	got, err := rootdir.From(filepath.FromSlash("/r/"))
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
	got, err = rootdir.From("rel")
	if err != nil {
		t.Fatal(err)
	}

	if !filepath.IsAbs(string(got)) || filepath.Base(string(got)) != "rel" {
		t.Fatalf("relative root: %q", got)
	}

	// Without one, the platform's data directory for the application name,
	// as primordium resolves it.
	got, err = rootdir.From("")
	if err != nil {
		t.Fatal(err)
	}

	want, err = dirs.DataDir()
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Fatalf("default: root = %q, want primordium's %q", got, want)
	}

	if filepath.Base(want) != binName {
		t.Fatalf("the default root is not named after the application: %q", want)
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
		{"binary", root.Binary(binName, "linux"), filepath.Join("r", "x", "bin", "hermetic")},
		{"binary on windows", root.Binary(binName, "windows"), filepath.Join("r", "x", "bin", "hermetic.exe")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
