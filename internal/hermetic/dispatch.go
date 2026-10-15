package hermetic

import (
	"errors"
	"strings"
)

var errNoPath = errors.New("the library resolved no path for the tool")

// Dispatched tells whether the binary was invoked under another name, through
// a link in the root's bin: then that name is the tool to run, with the rest
// of the arguments. Under its own name the binary is aqua, and tool is "".
func Dispatched(args []string, goos string) (tool string, toolArgs []string) {
	if len(args) == 0 {
		return "", nil
	}

	name := baseName(args[0], goos)
	if goos == windows {
		name = strings.TrimSuffix(name, ".exe")
		if strings.EqualFold(name, Name) {
			return "", nil
		}
	}

	if name == Name {
		return "", nil
	}

	return name, args[1:]
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

// valueFlags are the global flags that take a value in the next argument;
// `-x=v` carries its value itself. The subcommand comes after them.
//
//nolint:gochecknoglobals // immutable data mirroring the global flags exposed.
var valueFlags = map[string]bool{
	"-c": true, "--config": true, "--log-level": true,
}

// subcommand is the first argument that is neither a flag nor a flag's value,
// "" when there is none.
func subcommand(args []string) string {
	skip := false

	for _, arg := range args[1:] {
		switch {
		case skip:
			skip = false
		case valueFlags[arg]:
			skip = true
		case strings.HasPrefix(arg, "-"):
		default:
			return arg
		}
	}

	return ""
}

// installs reports whether a run of the library under its own name may have
// changed what aqua exposes in its bin: the installing subcommands.
func installs(args []string) bool {
	switch subcommand(args) {
	case "install", "i", "update", "up":
		return true
	default:
		return false
	}
}

// hasConfigFlag reports whether the run names its config on the command line
// (-c/--config), in which case the project walk stays out of it.
func hasConfigFlag(args []string) bool {
	for _, a := range args[1:] {
		if a == "--" {
			return false
		}

		if a == "-c" || a == "--config" || strings.HasPrefix(a, "-c=") || strings.HasPrefix(a, "--config=") {
			return true
		}
	}

	return false
}
