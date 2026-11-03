package rootdir

import (
	"fmt"
	"path/filepath"

	"github.com/mycophonic/primordium/filesystem/dirs"
)

const windows = "windows"

// Root is the toolset's directory.
type Root string

// From resolves the root: dir when not empty, made absolute, else the
// platform's data directory for the application name primordium's dirs was
// given ($XDG_DATA_HOME/<name> or ~/.local/share/<name> on Linux,
// ~/Library/Application Support/<name> on macOS, %LOCALAPPDATA%\<name> on
// Windows), created when missing.
func From(dir string) (Root, error) {
	if dir != "" {
		// Absolute: the three derived variables reach a dispatched tool, which
		// would resolve a relative one against its own working directory.
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("resolve the root %q: %w", dir, err)
		}

		return Root(abs), nil
	}

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

// Binary is where the binary named name lives, the target of every link in Bin.
func (r Root) Binary(name, goos string) string {
	return filepath.Join(r.Bin(), ExeName(name, goos))
}

// ExeName is a command's file name on the platform named: .exe on Windows.
func ExeName(name, goos string) string {
	if goos == windows {
		return name + ".exe"
	}

	return name
}
