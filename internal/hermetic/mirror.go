package hermetic

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Mirror makes the root's bin expose exactly the commands aqua exposes in its
// own bin, aqua itself excepted, each as a link to the binary: a symlink to the
// binary's name beside it, a hard link on Windows. The mirror owns those names:
// whatever sits under one that is not a link to the binary (a hard link left on
// a replaced binary, a symlink elsewhere) is replaced. Under any other name a
// link of ours is removed, since aqua no longer exposes it, and anything else is
// foreign and left alone. aqua's bin is read and never written, so there is no
// second writer.
//
// `aqua` is excepted because `update-aqua` places upstream's binary under that
// name in aqua's bin; a mirrored `aqua` would dispatch `exec -- aqua` to it.
func Mirror(root Root, goos string) error {
	binary := root.Binary(goos)
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("the binary the links point at: %w", err)
	}

	wanted, err := exposed(root.AquaRoot(), goos)
	if err != nil {
		return err
	}

	present, err := settle(root.Bin(), binary, wanted, goos)
	if err != nil {
		return err
	}

	for name := range wanted {
		if present[name] {
			continue
		}

		if err := makeLink(filepath.Join(root.Bin(), name), binary, goos); err != nil {
			return err
		}
	}

	return nil
}

// settle reconciles every existing entry of the bin against what aqua exposes
// and reports the names whose link is already right.
func settle(bin, binary string, wanted map[string]bool, goos string) (map[string]bool, error) {
	if err := os.MkdirAll(bin, dirPerm); err != nil {
		return nil, fmt.Errorf("create the bin directory: %w", err)
	}

	entries, err := os.ReadDir(bin)
	if err != nil {
		return nil, fmt.Errorf("read the bin directory: %w", err)
	}

	present := map[string]bool{}

	for _, entry := range entries {
		if entry.Name() == filepath.Base(binary) {
			continue
		}

		ok, err := reconcile(filepath.Join(bin, entry.Name()), binary, wanted[entry.Name()], goos)
		if err != nil {
			return nil, err
		}

		if ok {
			present[entry.Name()] = true
		}
	}

	return present, nil
}

// entryKind is what an existing entry of the bin is to the mirror.
type entryKind int

const (
	// foreign: a symlink to something else, or on every platform but Windows
	// a regular file. Left alone, wanted or not.
	foreign entryKind = iota
	// linked: a link to the binary, a symlink by name or a Windows hard link
	// to the same file.
	linked
	// stale: on Windows, a regular file that is not the binary's file, which
	// is what a hard link becomes once the binary is replaced. Nothing else of
	// that shape belongs in our bin.
	stale
)

// reconcile settles one existing entry of the bin against what aqua exposes.
// The mirror owns the wanted names: a link to the binary under one stays (ok),
// anything else under one, a stale hard link or a foreign entry, goes so the
// caller recreates the link. Under any other name only a link of ours goes;
// a foreign entry there is left alone.
func reconcile(link, binary string, wanted bool, goos string) (bool, error) {
	kind, err := classify(link, binary, goos)
	if err != nil {
		return false, err
	}

	if kind == linked && wanted {
		return true, nil
	}

	if wanted || kind == linked {
		if err := os.Remove(link); err != nil {
			return false, fmt.Errorf("remove %s: %w", link, err)
		}
	}

	return false, nil
}

// classify tells what an existing entry of the bin is to the mirror.
func classify(link, binary, goos string) (entryKind, error) {
	info, err := os.Lstat(link)
	if err != nil {
		return foreign, fmt.Errorf("inspect %s: %w", link, err)
	}

	if info.Mode()&fs.ModeSymlink != 0 {
		target, readErr := os.Readlink(link)
		if readErr != nil {
			return foreign, fmt.Errorf("read the link %s: %w", link, readErr)
		}

		if target == filepath.Base(binary) || target == binary {
			return linked, nil
		}

		return foreign, nil
	}

	if goos != windows || !info.Mode().IsRegular() {
		return foreign, nil
	}
	// A hard link is a second name for the file as linked: a replaced binary
	// leaves the old names on the old file, which os.SameFile tells apart. Go
	// sets no execute bit on a Windows file, so the mode tells nothing.
	bin, statErr := os.Stat(binary)
	if statErr != nil {
		return foreign, fmt.Errorf("inspect the binary: %w", statErr)
	}

	if os.SameFile(info, bin) {
		return linked, nil
	}

	return stale, nil
}

// exposed is the set of command names in aqua's bin, aqua itself excepted.
func exposed(aquaRoot, goos string) (map[string]bool, error) {
	entries, err := os.ReadDir(filepath.Join(aquaRoot, "bin"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]bool{}, nil
		}

		return nil, fmt.Errorf("read aqua's bin: %w", err)
	}

	names := make(map[string]bool, len(entries))

	for _, entry := range entries {
		name := entry.Name()
		if name == exeName("aqua", goos) || name == exeName(Name, goos) {
			continue
		}

		names[name] = true
	}

	return names, nil
}

func makeLink(link, binary, goos string) error {
	if goos == windows {
		if err := os.Link(binary, link); err != nil {
			return fmt.Errorf("hard-link %s to the binary: %w", link, err)
		}

		return nil
	}
	// Relative, to the binary beside it: the root may move as a whole.
	if err := os.Symlink(filepath.Base(binary), link); err != nil {
		return fmt.Errorf("link %s to the binary: %w", link, err)
	}

	return nil
}
