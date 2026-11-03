package mirror_test

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

	"github.com/forkcloser/hermetic/internal/mirror"
	"github.com/forkcloser/hermetic/internal/rootdir"
)

// binName is the name cmd/hermetic gives the binary.
const binName = "hermetic"

// mirrorRoot is a scratch root with a binary for the platform named and the
// commands aqua exposes, as symlinks to its proxy the way aqua links them.
func mirrorRoot(t *testing.T, goos string, exposed ...string) (rootdir.Root, string) {
	t.Helper()

	root := rootdir.Root(t.TempDir())
	aquaBin := filepath.Join(root.AquaRoot(), "bin")

	if err := os.MkdirAll(aquaBin, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(root.Bin(), 0o750); err != nil {
		t.Fatal(err)
	}

	binary := root.Binary(binName, goos)
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

func binNames(t *testing.T, root rootdir.Root) []string {
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
	if err := os.Symlink(binName, filepath.Join(root.Bin(), "gone")); err != nil {
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

	if err := mirror.Sync(root, root.Binary(binName, "linux"), "linux"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"limen", "ssh"} {
		target, err := os.Readlink(filepath.Join(root.Bin(), name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if target != binName {
			t.Fatalf("%s -> %q, want %q", name, target, binName)
		}
	}

	if want := []string{binName, "keep", "limen", "ssh", "theirs"}; !slices.Equal(binNames(t, root), want) {
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

	if err := mirror.Sync(root, root.Binary(binName, "linux"), "linux"); err != nil {
		t.Fatal(err)
	}

	if want := []string{binName, "keep", "limen", "theirs"}; !slices.Equal(binNames(t, root), want) {
		t.Fatalf("bin = %v, want %v", binNames(t, root), want)
	}

	// No binary: the mirror refuses rather than linking to nothing.
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}

	if err := mirror.Sync(
		root,
		root.Binary(binName, "linux"),
		"linux",
	); err == nil ||
		!strings.Contains(err.Error(), "the binary") {
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

	if err := mirror.Sync(root, root.Binary(binName, "windows"), "windows"); err != nil {
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

	if err := mirror.Sync(root, root.Binary(binName, "windows"), "windows"); err != nil {
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

				if name == binName {
					continue
				}

				kind := kinds[rng.IntN(len(kinds))]
				path := filepath.Join(root.Bin(), name)

				switch kind {
				case "ours":
					symlink(t, binName, path)
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

			if err := mirror.Sync(root, root.Binary(binName, "linux"), "linux"); err != nil {
				t.Fatalf("%v: %v", before, err)
			}

			check := func(pass string) {
				t.Helper()

				for _, name := range names {
					path := filepath.Join(root.Bin(), name)
					target, err := os.Readlink(path)
					linked := err == nil && (target == binName || target == binary)

					switch {
					case name == binName:
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

			if err := mirror.Sync(root, root.Binary(binName, "linux"), "linux"); err != nil {
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

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
