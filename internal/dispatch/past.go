package dispatch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/forkcloser/hermetic/internal/rootdir"
)

// ErrNoPath is a name nothing answers: no config pins it, and the PATH holds
// nothing for it but the binary's own links.
var ErrNoPath = errors.New("the library resolved no path for the tool")

// PastLink is where the library's answer for a tool leads. For a name no
// config has, the library falls back to the PATH, and the first hit there is a
// link to this binary, the root's own, since the library skips only its own
// proxy: that hit is passed over for the next entry on the PATH that is
// neither the binary running nor the root's binary, which is what a script
// that runs outside any project, an autotools build in a scratch directory,
// gets from the system; only when nothing remains is the name refused, rather
// than dispatching itself forever. The binary running is a self in its own
// right: a link to it runs under a root whose binary is another file when
// HERMETIC_ROOT_DIR says so. Any other answer stands, installed yet or not.
func PastLink(root rootdir.Root, path, tool string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: %s", ErrNoPath, tool)
	}

	selves, err := selves(root)
	if err != nil {
		return "", err
	}

	if info, statErr := os.Stat(path); statErr == nil && isAny(info, selves) {
		if past := lookPathPast(selves, tool); past != "" {
			return past, nil
		}

		return "", fmt.Errorf("%w: %s", ErrNoPath, tool)
	}

	return path, nil
}

// selves is every file a PATH entry must not resolve to: the binary running,
// and the root's binary when the root holds one.
func selves(root rootdir.Root) ([]os.FileInfo, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("the binary running: %w", err)
	}

	running, err := os.Stat(self)
	if err != nil {
		return nil, fmt.Errorf("the binary running: %w", err)
	}

	files := []os.FileInfo{running}

	binary, err := os.Stat(root.Binary(runtime.GOOS))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return files, nil
		}

		return nil, fmt.Errorf("the binary: %w", err)
	}

	return append(files, binary), nil
}

// isAny reports whether info is one of the files.
func isAny(info os.FileInfo, files []os.FileInfo) bool {
	for _, file := range files {
		if os.SameFile(info, file) {
			return true
		}
	}

	return false
}

// lookPathPast is the first executable named tool on the PATH that is none of
// the selves: every entry whose file is the binary running or the root's, the
// root's bin and any other link to either, is passed over.
func lookPathPast(selves []os.FileInfo, tool string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}

		for _, name := range toolNames(tool) {
			// #nosec G703 -- the PATH is the shell's own, read as any lookup reads it.
			if candidate := filepath.Join(dir, name); runnable(candidate, selves) {
				return candidate
			}
		}
	}

	return ""
}

// toolNames is what a tool's name may be spelled as in a PATH entry: the name
// itself, and on Windows the name under each of PATHEXT's extensions, as a
// shell would find it.
func toolNames(tool string) []string {
	names := []string{tool}

	if runtime.GOOS != windows {
		return names
	}

	for ext := range strings.SplitSeq(os.Getenv("PATHEXT"), ";") {
		if ext != "" {
			names = append(names, tool+strings.ToLower(ext), tool+strings.ToUpper(ext))
		}
	}

	return names
}

// runnable reports whether path is an executable file that is none of the
// selves.
func runnable(path string, selves []os.FileInfo) bool {
	// #nosec G703 -- the path is a PATH entry joined with the tool's name, read as any lookup reads it.
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || isAny(info, selves) {
		return false
	}

	return runtime.GOOS == windows || info.Mode()&0o111 != 0
}
