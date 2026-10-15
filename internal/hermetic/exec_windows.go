//go:build windows

package hermetic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/suzuki-shunsuke/go-error-with-exit-code/ecerror"
)

// execTool runs the tool as a child, since Windows has no exec: stdio
// inherited, the environment the one given, and its exit code carried out as
// ours.
func execTool(ctx context.Context, path, name string, args, environ []string) error {
	// #nosec G204 -- the path is what the library resolved for the name, the arguments the user's own.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Args[0] = name
	cmd.Env = environ
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return ecerror.Wrap(err, exit.ExitCode())
	}

	if err != nil {
		return fmt.Errorf("run %s: %w", path, err)
	}

	return nil
}
