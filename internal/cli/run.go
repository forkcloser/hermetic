// Package cli is the binary's command line: its grammar, the commands that
// call the library, and the run that tells a command from a dispatch
// (docs/design.md, "The binary").
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/aquaproj/aqua/v2/pkg/config"
	applogger "github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/app/shutdown"

	"github.com/forkcloser/hermetic/internal/dispatch"
	"github.com/forkcloser/hermetic/internal/library"
	"github.com/forkcloser/hermetic/internal/mirror"
	"github.com/forkcloser/hermetic/internal/projectdir"
	"github.com/forkcloser/hermetic/internal/rootdir"
	"github.com/forkcloser/hermetic/internal/transport"
)

// Env is one invocation's view of its process: the version it reports, its
// streams, its environment and its arguments, argv[0] first. A test runs the
// binary through one without owning the process.
type Env struct {
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Args    []string
}

const description = "The toolset's own aqua: the library's commands under one root of its own, and the tools it installs, by name"

var (
	errNotManaged = errors.New("the command is not managed by the library, so it has no version")
	errNotLevel   = errors.New(library.LogLevelEnv + " is not a log level (debug, info, warn, error)")
)

// Main runs the binary as a process: Run under a context primordium's shutdown
// cancels on a signal, then the exit status Run asked for, 0 on nil, 1 with the
// error logged on anything else. It never returns.
func Main(version string) {
	transport.Install(rootdir.Name)

	ctx := shutdown.SetDefaults(context.Background())

	shutdown.Run(ctx, func(ctx context.Context) error {
		return Run(ctx, &Env{
			Version: versionOf(version),
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
			Getenv:  os.Getenv,
			Args:    os.Args,
		})
	})
}

// versionOf is the version the release build stamped, else the module version
// a `go install` recorded, else "unknown".
func versionOf(stamped string) string {
	if stamped != "" {
		return stamped
	}

	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}

	return "unknown"
}

// Run is one invocation of the binary. Under its own name the binary parses
// its grammar and runs the command named; under any other name, through a
// link, it resolves that tool through the library and runs it itself. The
// library, with the root, the environment, the project walk and its gate, is
// made when a command asks for it: a version, a usage or a usage error asks
// for none, so each answers where the root cannot be made.
func Run(ctx context.Context, env *Env) error {
	tool, toolArgs := dispatch.Target(env.Args, runtime.GOOS)

	var (
		cli  grammar
		kctx *kong.Context
		err  error
	)

	level := env.Getenv(library.LogLevelEnv)

	if tool == "" {
		kctx, err = parse(ctx, &cli, env)
		if err != nil || kctx == nil {
			return err
		}

		level = cli.LogLevel
	}

	if level == "" {
		level = library.DefaultLogLevel
	}

	slogLevel, err := levelOf(level)
	if err != nil {
		return err
	}
	// The process-wide handler, the library's logger included: the level is
	// the flag's or HERMETIC_LOG_LEVEL's, never the LOG_LEVEL the handler would
	// read on its own.
	applogger.SetDefaultsForLogger(ctx, slogLevel)

	log := slog.Default()

	if tool == "" {
		keepsGate := strings.HasPrefix(kctx.Command(), "policy")

		bindErr := kctx.BindToProvider(func() (*library.Library, error) {
			return library.New(ctx, log, env.Getenv, env.Stdout, env.Version, cli.Config, keepsGate, level)
		})
		if bindErr != nil {
			return fmt.Errorf("bind the library: %w", bindErr)
		}

		if runErr := kctx.Run(); runErr != nil {
			return refusal(env, fmt.Errorf("%s: %w", kctx.Command(), runErr))
		}

		return nil
	}
	// The shell's own environment, kept for the tool a dispatch runs: the
	// library's run scrubs and sets the process's.
	shell := os.Environ()

	lib, err := library.New(ctx, log, env.Getenv, env.Stdout, env.Version, "", false, level)
	if err != nil {
		return refusal(env, err)
	}

	path, err := lib.Resolve(ctx, tool)
	if err != nil {
		return err
	}

	syncBin(ctx, log, lib.Root)

	return dispatch.Exec(ctx, path, tool, toolArgs, dispatch.Environment(shell))
}

// refusal turns the gate's refusal into the run's last word: the command that
// allows the policy, alone on stderr, and exit status 1 with nothing logged.
// Any other error is returned as it is.
func refusal(env *Env, err error) error {
	refused, ok := errors.AsType[*projectdir.RefusedPolicyError](err)
	if !ok {
		return err
	}

	fmt.Fprintln(env.Stderr, refused.Error())

	return shutdown.ExitError(1) //nolint:wrapcheck // the status is the answer; the refusal is on stderr.
}

// exitRequest is an exit kong asked for, carried out of its parser as a panic:
// --help and --version print and ask for 0, a usage error prints and asks for
// its code. parse answers each on the binary's own exit path, so a test
// survives both and the shutdown handlers run.
type exitRequest int

// parse reads the binary's arguments against its grammar. A nil context with a
// nil error is an invocation kong answered itself (--help, --version); a usage
// error has been printed with the usage and comes back as the exit status 1,
// kong's own 80 folded into the one failure status the toolset reads.
func parse(ctx context.Context, cli *grammar, env *Env) (kctx *kong.Context, err error) {
	defer func() {
		if value := recover(); value != nil {
			code, ok := value.(exitRequest)
			if !ok {
				panic(value)
			}

			kctx = nil
			err = nil

			if code != 0 {
				err = shutdown.ExitError(1)
			}
		}
	}()

	parser, err := kong.New(cli,
		kong.Name(rootdir.Name),
		kong.Description(description),
		kong.Writers(env.Stdout, env.Stderr),
		kong.UsageOnError(),
		kong.Vars{"version": env.Version, "limit": strconv.Itoa(config.DefaultVerCnt)},
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(env),
		kong.Exit(func(code int) { panic(exitRequest(code)) }),
	)
	if err != nil {
		return nil, fmt.Errorf("build the command line: %w", err)
	}

	kctx, err = parser.Parse(env.Args[1:])
	parser.FatalIfErrorf(err)

	return kctx, nil
}

// levelOf is the slog level a name selects; kong has validated the flag's, the
// variable's is checked here.
func levelOf(name string) (slog.Level, error) {
	switch name {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%w: %q", errNotLevel, name)
	}
}

// syncBin mirrors best effort after a run that may have changed what aqua
// exposes: the run's own result stands either way.
func syncBin(ctx context.Context, logger *slog.Logger, r rootdir.Root) {
	if err := mirror.Sync(r, runtime.GOOS); err != nil {
		logger.WarnContext(ctx, "mirror aqua's commands into the bin", "error", err)
	}
}
