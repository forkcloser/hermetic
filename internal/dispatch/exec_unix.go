//go:build !windows

package dispatch

import (
	"context"
	"fmt"
	"syscall"
)

// Exec replaces the process with the tool: its argv[0] is the name it was
// invoked under, its environment the one given, and signals and the exit code
// are its own, nothing of ours in between. The context ends with the process.
func Exec(_ context.Context, path, name string, args, environ []string) error {
	// #nosec G204 -- the path is what the library resolved for the name, the arguments the user's own.
	if err := syscall.Exec(path, append([]string{name}, args...), environ); err != nil {
		return fmt.Errorf("run %s: %w", path, err)
	}

	return nil
}
