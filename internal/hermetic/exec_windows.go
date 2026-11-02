//go:build windows

package hermetic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mycophonic/primordium/app/shutdown"
)

// execTool runs the tool as a child, since Windows has no exec: stdio
// inherited, the environment the one given, and its exit status carried out as
// ours, silently, since the tool has said what it had to.
func execTool(ctx context.Context, path, name string, args, environ []string) error {
	// #nosec G204 -- the path is what the library resolved for the name, the arguments the user's own.
	cmd := exec.CommandContext(ctx, path, args...)
	// A batch file runs as cmd.exe /c on the command line, not the path: under
	// the bare name, cmd.exe would look the name up on the PATH again, where a
	// link to this binary may come first, and dispatch forever.
	if !isBatch(path) {
		cmd.Args[0] = name
	}

	cmd.Env = environ
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return fmt.Errorf("%s: %w", name, shutdown.ExitError(exit.ExitCode()))
	}

	if err != nil {
		return fmt.Errorf("run %s: %w", path, err)
	}

	return nil
}

// isBatch reports whether path is a file Windows hands to cmd.exe.
func isBatch(path string) bool {
	ext := filepath.Ext(path)

	return strings.EqualFold(ext, ".bat") || strings.EqualFold(ext, ".cmd")
}
