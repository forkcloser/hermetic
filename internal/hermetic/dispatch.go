package hermetic

import (
	"strings"
)

// Dispatched tells whether the binary was invoked under another name, through
// a link in the root's bin: then that name is the tool to run, with the rest
// of the arguments. Under its own name the binary is aqua, and tool is "".
func Dispatched(args []string, goos string) (tool string, toolArgs []string) {
	if len(args) == 0 {
		return "", nil
	}

	name := baseName(args[0], goos)
	if goos == windows {
		name = trimExe(name)
		if strings.EqualFold(name, Name) {
			return "", nil
		}
	}

	if name == Name {
		return "", nil
	}

	return name, args[1:]
}

// trimExe drops a trailing ".exe" in any case: the file system that spells
// the name is case-insensitive, and argv[0] carries the caller's spelling.
func trimExe(name string) string {
	const ext = ".exe"

	if len(name) > len(ext) && strings.EqualFold(name[len(name)-len(ext):], ext) {
		return name[:len(name)-len(ext)]
	}

	return name
}

// baseName is the last element of an invocation path for the platform named,
// not the running one: Windows paths split on both separators.
func baseName(path, goos string) string {
	seps := "/"
	if goos == windows {
		seps = `/\`
	}

	if i := strings.LastIndexAny(path, seps); i >= 0 {
		return path[i+1:]
	}

	return path
}
