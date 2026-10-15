package hermetic

import (
	"context"
	"errors"
	"slices"

	"github.com/aquaproj/aqua/v2/pkg/cli/cliargs"
	"github.com/aquaproj/aqua/v2/pkg/cli/generate"
	"github.com/aquaproj/aqua/v2/pkg/cli/install"
	cpolicy "github.com/aquaproj/aqua/v2/pkg/cli/policy"
	aquaroot "github.com/aquaproj/aqua/v2/pkg/cli/root"
	"github.com/aquaproj/aqua/v2/pkg/cli/upc"
	"github.com/aquaproj/aqua/v2/pkg/cli/update"
	"github.com/aquaproj/aqua/v2/pkg/cli/util"
	aquawhich "github.com/aquaproj/aqua/v2/pkg/cli/which"
	aquaruntime "github.com/aquaproj/aqua/v2/pkg/runtime"
	"github.com/suzuki-shunsuke/go-error-with-exit-code/ecerror"
	"github.com/suzuki-shunsuke/slog-util/slogutil"
	"github.com/suzuki-shunsuke/urfave-cli-v3-util/urfave"
	"github.com/suzuki-shunsuke/urfave-cli-v3-util/vcmd"
	"github.com/urfave/cli/v3"
)

// runLibrary runs the library's command tree as the binary exposes it: the
// commands the toolset's scripts, recipes and workflows call and the dispatch
// uses, each built by the library itself, under the two global flags that name
// a config and a log level. The rest of aqua is out of reach: exec (a tool run
// through the library gets the library's environment, not the shell's; the
// links in the bin are the way to run one), the verifier switches and the
// profiling flags, the deprecated flags, and the commands nothing here calls.
func runLibrary(ctx context.Context, logger *slogutil.Logger, env *urfave.Env) error {
	param := &util.Param{
		Stdin:   env.Stdin,
		Stdout:  env.Stdout,
		Stderr:  env.Stderr,
		Logger:  logger,
		Runtime: aquaruntime.New(ctx),
		Version: env.Version,
	}
	global := &cliargs.GlobalArgs{}

	cmd := &cli.Command{
		Name:      Name,
		Usage:     "The toolset's environment: aqua as a library, under one root of its own",
		Version:   env.Version,
		Writer:    env.Stdout,
		ErrWriter: env.Stderr,
		Flags:     keepFlags(cliargs.GlobalFlags(global), "config", "log-level"),
		// urfave's own handler prints an exit-coded error and exits the process
		// on the spot; the error returns to Main instead, with its code, so one
		// path logs every failure.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Commands: []*cli.Command{
			dropFlags(install.New(param, global), "test"),
			update.New(param, global),
			dropFlags(upc.New(param, global), "deep"),
			generate.New(param, global),
			aquawhich.New(param, global),
			dropCommands(cpolicy.New(param, global), "init"),
			aquaroot.New(param, global),
		},
	}

	err := vcmd.With(cmd, "").Run(ctx, env.Args)

	if exit, ok := errors.AsType[cli.ExitCoder](err); ok {
		return ecerror.Wrap(err, exit.ExitCode())
	}

	return err //nolint:wrapcheck // the library's error is the command's result, reported as aqua reports it.
}

// keepFlags is the flags whose primary name is one of names.
func keepFlags(flags []cli.Flag, names ...string) []cli.Flag {
	kept := make([]cli.Flag, 0, len(names))

	for _, flag := range flags {
		if slices.Contains(names, flag.Names()[0]) {
			kept = append(kept, flag)
		}
	}

	return kept
}

// dropFlags removes the flags named from the command's own.
func dropFlags(cmd *cli.Command, names ...string) *cli.Command {
	cmd.Flags = slices.DeleteFunc(cmd.Flags, func(flag cli.Flag) bool {
		return slices.Contains(names, flag.Names()[0])
	})

	return cmd
}

// dropCommands removes the subcommands named.
func dropCommands(cmd *cli.Command, names ...string) *cli.Command {
	cmd.Commands = slices.DeleteFunc(cmd.Commands, func(sub *cli.Command) bool {
		return slices.Contains(names, sub.Name)
	})

	return cmd
}
