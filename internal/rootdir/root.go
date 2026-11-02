// Package rootdir is the toolset's directory: where it is, and the paths aqua's
// root, global config and policy derive from it (docs/design.md, "The root").
package rootdir

import (
	"fmt"
	"path/filepath"

	"github.com/mycophonic/primordium/filesystem/dirs"
)

const (
	// Name is the binary's name, the one the dispatch compares against and the
	// target every link in the root's bin points at.
	Name = "hermetic"

	// Env names the root; aqua's root, global config and policy derive from
	// it, and the shell's values of those three are never consulted.
	Env = "HERMETIC_ROOT_DIR"

	windows = "windows"
)

// Root is the toolset's directory.
type Root string

// From resolves the root: Env when set, made absolute, else the
// platform's data directory for the binary's name as primordium resolves it
// ($XDG_DATA_HOME/hermetic or ~/.local/share/hermetic on Linux,
// ~/Library/Application Support/hermetic on macOS,
// %LOCALAPPDATA%\hermetic on Windows), created when missing.
func From(getenv func(string) string) (Root, error) {
	if r := getenv(Env); r != "" {
		// Absolute: the three derived variables reach a dispatched tool, which
		// would resolve a relative one against its own working directory.
		abs, err := filepath.Abs(r)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", Env, err)
		}

		return Root(abs), nil
	}

	dirs.SetAppName(Name)

	dir, err := dirs.DataDir()
	if err != nil {
		return "", fmt.Errorf("resolve the default root: %w", err)
	}

	return Root(dir), nil
}

// Bin is the one PATH entry a shell needs: the binary and the links to it.
func (r Root) Bin() string { return filepath.Join(string(r), "bin") }

// AquaRoot is the library's own root (AQUA_ROOT_DIR): its package store, its
// bin, its policy records, under the names the library fixes. Never on a PATH.
func (r Root) AquaRoot() string { return filepath.Join(string(r), "store") }

// GlobalConfig is the toolset's pins (AQUA_GLOBAL_CONFIG). Its checksums file
// beside it is the library's to name.
func (r Root) GlobalConfig() string { return filepath.Join(string(r), "hermetic.yaml") }

// Policy is the toolset's policy (AQUA_POLICY_CONFIG), the one trusted on sight.
func (r Root) Policy() string { return filepath.Join(string(r), "policy.yaml") }

// Binary is where the binary lives, the target of every link in Bin.
func (r Root) Binary(goos string) string {
	return filepath.Join(r.Bin(), ExeName(Name, goos))
}

// ExeName is a command's file name on the platform named: .exe on Windows.
func ExeName(name, goos string) string {
	if goos == windows {
		return name + ".exe"
	}

	return name
}
