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
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/aquaproj/aqua/v2/pkg/config"
	applogger "github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/app/shutdown"
	"github.com/mycophonic/primordium/filesystem/dirs"
	"github.com/mycophonic/primordium/network"
	"github.com/mycophonic/primordium/network/transporter"

	"github.com/forkcloser/hermetic/internal/dispatch"
	"github.com/forkcloser/hermetic/internal/library"
	"github.com/forkcloser/hermetic/internal/mirror"
	"github.com/forkcloser/hermetic/internal/projectdir"
)

// Env is one invocation of the binary: the name it answers to and the version
// it reports, its streams, and its arguments, argv[0] first. The environment is
// the process's. A test runs the binary through one without owning its streams.
type Env struct {
	Name    string
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	Args    []string
}

const (
	description = "The toolset's own aqua: the library's commands under one root of its own, and the tools it installs, by name"

	// defaultLogLevel is the --log-level flag's default, and a dispatch's.
	defaultLogLevel = "info"
)

// The library's own GitHub client wraps the process's client in a retrying one
// of its own (retryablehttp, four retries), so this policy stays short: the two
// multiply on a GitHub API call.
const (
	maxRetries     = 3
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second
)

var errNotManaged = errors.New("the command is not managed by the library, so it has no version")

// Main runs the binary as a process. It sets what is the process's once, before
// anything reads it: the application name primordium's dirs resolves the
// default root under, and the HTTP client, TLS 1.3 with post-quantum hybrid
// key exchange and primordium's retry and backoff, which the library is handed
// for every request it makes (it takes no client of its own). Then Run, under a
// context primordium's shutdown cancels on a signal, and the exit status Run
// asked for: 0 on nil, 1 with the error logged on anything else. It never
// returns.
func Main(name, version string) {
	dirs.SetAppName(name)
	network.SetDefaults()

	//nolint:reassign // the library is handed http.DefaultClient and takes no other.
	http.DefaultClient = transporter.NewClient(transporter.Options{
		MaxRetries:     maxRetries,
		InitialBackoff: initialBackoff,
		MaxBackoff:     maxBackoff,
		UserAgent:      name,
	})

	ctx := shutdown.SetDefaults(context.Background())

	shutdown.Run(ctx, func(ctx context.Context) error {
		return Run(ctx, &Env{
			Name:    name,
			Version: versionOf(version),
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
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
// link, it resolves that tool through the library and runs it itself.
func Run(ctx context.Context, env *Env) error {
	tool, toolArgs := dispatch.Target(env.Args, env.Name, runtime.GOOS)
	if tool == "" {
		return command(ctx, env)
	}
	// The shell's own environment, kept for the tool: the library's run scrubs
	// and sets the process's.
	shell := os.Environ()

	opts, err := library.ReadOptions(shell)
	if err != nil {
		return err
	}

	if opts.LogLevel == "" {
		opts.LogLevel = defaultLogLevel
	}

	setLogging(ctx, opts.LogLevel)

	opts.Version = env.Version

	lib, err := library.New(ctx, opts)
	if err != nil {
		return refusal(env, err)
	}

	self := lib.Root.Binary(env.Name, runtime.GOOS)

	path, err := lib.Resolve(ctx, tool, self)
	if err != nil {
		return err
	}

	syncBin(ctx, lib, self)

	return dispatch.Exec(ctx, path, tool, toolArgs, dispatch.Environment(shell))
}

// command runs the command the arguments name. The library, with the root, the
// environment, the project walk and its gate, is made when the command asks for
// it: a version, a usage or a usage error asks for none, so each answers where
// the root cannot be made.
func command(ctx context.Context, env *Env) error {
	var cli grammar

	kctx, err := parse(ctx, &cli, env)
	if err != nil || kctx == nil {
		return err
	}

	setLogging(ctx, cli.LogLevel)

	bindErr := kctx.BindToProvider(func() (*library.Library, error) {
		opts, err := library.ReadOptions(os.Environ())
		if err != nil {
			return nil, err
		}

		if cli.Config != "" {
			opts.Config = cli.Config
		}

		opts.LogLevel = cli.LogLevel
		opts.Version = env.Version
		opts.KeepsGate = strings.HasPrefix(kctx.Command(), "policy")

		return library.New(ctx, opts)
	})
	if bindErr != nil {
		return fmt.Errorf("bind the library: %w", bindErr)
	}

	if err := kctx.Run(); err != nil {
		return refusal(env, fmt.Errorf("%s: %w", kctx.Command(), err))
	}

	return nil
}

// setLogging installs the process-wide handler, the library's logger included,
// at level, which ReadOptions or kong has validated: the flag's or
// HERMETIC_LOG_LEVEL's, never the LOG_LEVEL the handler would read on its own.
func setLogging(ctx context.Context, level string) {
	var slogLevel slog.Level
	if err := slogLevel.UnmarshalText([]byte(level)); err != nil {
		panic(fmt.Sprintf("an unvalidated log level reached the logger: %q", level))
	}

	applogger.SetDefaultsForLogger(ctx, slogLevel)
}

// refusal turns the gate's refusal into the run's last word: the command that
// allows the policy, alone on stderr, and exit status 1 with nothing logged.
// Any other error is returned as it is.
func refusal(env *Env, err error) error {
	refused, ok := errors.AsType[*projectdir.RefusedPolicyError](err)
	if !ok {
		return err
	}

	fmt.Fprintln(env.Stderr, env.Name+" policy allow "+refused.Policy)

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
		kong.Name(env.Name),
		kong.Description(description),
		kong.Writers(env.Stdout, env.Stderr),
		kong.UsageOnError(),
		kong.Vars{
			"version":  env.Version,
			"limit":    strconv.Itoa(config.DefaultVerCnt),
			"logLevel": defaultLogLevel,
		},
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

// syncBin mirrors best effort after a run that may have changed what aqua
// exposes: the run's own result stands either way. self is the root's binary.
func syncBin(ctx context.Context, lib *library.Library, self string) {
	if err := mirror.Sync(lib.Root, self, runtime.GOOS); err != nil {
		lib.Logger.WarnContext(ctx, "mirror aqua's commands into the bin", "error", err)
	}
}
