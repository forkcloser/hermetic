package hermetic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/aquaproj/aqua/v2/pkg/config"
	"github.com/aquaproj/aqua/v2/pkg/controller"
	aquawhich "github.com/aquaproj/aqua/v2/pkg/controller/which"
	aquaruntime "github.com/aquaproj/aqua/v2/pkg/runtime"
	applogger "github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/app/shutdown"
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
	errNoPath     = errors.New("the library resolved no path for the tool")
	errNotManaged = errors.New("the command is not managed by the library, so it has no version")
	errNotLevel   = errors.New(LogLevelEnv + " is not a log level (debug, info, warn, error)")
)

// Main runs the binary as a process: Run under a context primordium's shutdown
// cancels on a signal, then the exit status Run asked for, 0 on nil, 1 with the
// error logged on anything else. It never returns.
func Main(version string) {
	useTransporter()

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
	tool, toolArgs := Dispatched(env.Args, runtime.GOOS)

	var (
		cli  grammar
		kctx *kong.Context
		err  error
	)

	level := env.Getenv(LogLevelEnv)

	if tool == "" {
		kctx, err = parse(ctx, &cli, env)
		if err != nil || kctx == nil {
			return err
		}

		level = cli.LogLevel
	}

	if level == "" {
		level = defaultLogLevel
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

		bindErr := kctx.BindToProvider(func() (*library, error) {
			return newLibrary(ctx, log, env, cli.Config, keepsGate, level)
		})
		if bindErr != nil {
			return fmt.Errorf("bind the library: %w", bindErr)
		}

		if runErr := kctx.Run(); runErr != nil {
			return fmt.Errorf("%s: %w", kctx.Command(), runErr)
		}

		return nil
	}
	// The shell's own environment, kept for the tool a dispatch runs: the
	// library's run scrubs and sets the process's.
	shell := os.Environ()

	lib, err := newLibrary(ctx, log, env, "", false, level)
	if err != nil {
		return err
	}

	path, err := resolve(ctx, lib, tool)
	if err != nil {
		return err
	}

	syncBin(ctx, log, lib.root)

	return execTool(ctx, path, tool, toolArgs, ToolEnvironment(shell))
}

// newLibrary is the library as a command runs it: the root, the environment
// applied to the process, which the verifiers the library runs inherit, the
// project walk and its gate, and the parameters derived from that environment.
func newLibrary(
	ctx context.Context,
	log *slog.Logger,
	env *Env,
	configFlag string,
	keepsGate bool,
	level string,
) (*library, error) {
	root, err := RootFrom(env.Getenv)
	if err != nil {
		return nil, err
	}

	set, unset, err := environment(log, root, configFlag != "", keepsGate, os.Environ())
	if err != nil {
		return nil, err
	}

	if applyErr := apply(set, unset); applyErr != nil {
		return nil, applyErr
	}

	base, err := libraryParam(log, root, set, env.Version, configFlag, level)
	if err != nil {
		return nil, err
	}

	return &library{
		logger:  log,
		root:    root,
		runtime: aquaruntime.New(ctx),
		base:    base,
		stdout:  env.Stdout,
	}, nil
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
		kong.Name(Name),
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

// which asks the library where a command lives: the registries installed, the
// package's path named whether it is installed yet or not, and, for a name no
// config has, the PATH's answer.
func which(ctx context.Context, lib *library, param *config.Param, tool string) (*aquawhich.FindResult, error) {
	ctrl, err := controller.InitializeWhichCommandController(ctx, lib.logger, param, http.DefaultClient, lib.runtime)
	if err != nil {
		return nil, fmt.Errorf("initialize the which controller: %w", err)
	}

	found, err := ctrl.Which(ctx, lib.logger.With("exe_name", tool), param, tool)
	if err != nil {
		return nil, err
	}

	return found, nil
}

// resolve finds the tool a dispatch runs: which for the path, then install for
// the package when it is not there yet, the first-use install a links-only root
// relies on. For a name no config has, which falls back to the PATH, and the
// first hit there is this very binary, the link the dispatch came through,
// since the library skips only its own proxy: that hit is passed over for the
// next entry on the PATH that is not the binary, which is what a script that
// runs outside any project, an autotools build in a scratch directory, gets
// from the system; only when nothing remains is the name refused, rather than
// dispatching itself forever.
func resolve(ctx context.Context, lib *library, tool string) (string, error) {
	found, err := which(ctx, lib, lib.param(), tool)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}

	path := found.ExePath
	if path == "" {
		return "", fmt.Errorf("%w: %s", errNoPath, tool)
	}

	binary, err := os.Stat(lib.root.Binary(runtime.GOOS))
	if err != nil {
		return "", fmt.Errorf("the binary: %w", err)
	}

	info, err := os.Stat(path)
	if err == nil && os.SameFile(info, binary) {
		if past := lookPathPast(binary, tool); past != "" {
			return past, nil
		}

		return "", fmt.Errorf("%w: %s", errNoPath, tool)
	}

	if err == nil {
		return path, nil
	}
	// All: the global config's packages too, since without it the library
	// reads the working directory's config alone and the toolset's pins are
	// global.
	param := lib.param()
	param.All = true
	param.Args = []string{tool}

	ctrl, err := controller.InitializeInstallCommandController(ctx, lib.logger, param, http.DefaultClient, lib.runtime)
	if err != nil {
		return "", fmt.Errorf("initialize the install controller: %w", err)
	}

	if err := ctrl.Install(ctx, lib.logger, param); err != nil {
		return "", fmt.Errorf("install %s: %w", tool, err)
	}

	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%w after installing: %s", errNoPath, tool)
	}

	return path, nil
}

// lookPathPast is the first executable named tool on the PATH that is not the
// binary: every entry whose file is the binary, the root's bin and any other
// link to it, is passed over.
func lookPathPast(binary os.FileInfo, tool string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}

		for _, name := range toolNames(tool) {
			// #nosec G703 -- the PATH is the shell's own, read as any lookup reads it.
			if candidate := filepath.Join(dir, name); runnable(candidate, binary) {
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

// runnable reports whether path is an executable file that is not the binary.
func runnable(path string, binary os.FileInfo) bool {
	// #nosec G703 -- the path is a PATH entry joined with the tool's name, read as any lookup reads it.
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || os.SameFile(info, binary) {
		return false
	}

	return runtime.GOOS == windows || info.Mode()&0o111 != 0
}

// syncBin mirrors best effort after a run that may have changed what aqua
// exposes: the run's own result stands either way.
func syncBin(ctx context.Context, logger *slog.Logger, root Root) {
	if err := Mirror(root, runtime.GOOS); err != nil {
		logger.WarnContext(ctx, "mirror aqua's commands into the bin", "error", err)
	}
}

// apply sets and unsets the process environment for the library.
func apply(set map[string]string, unset []string) error {
	for _, key := range unset {
		if err := os.Unsetenv(key); err != nil {
			return fmt.Errorf("unset %s: %w", key, err)
		}
	}

	for key, value := range set {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}

	return nil
}

// environment is what the library's run sets and unsets, and, unless the run
// names its config itself (configured: -c, or HERMETIC_CONFIG), AQUA_CONFIG
// from the project walk, with the project's policy when it passes the gate.
// The policy subcommands are the gate's own keepers (keepsGate): `policy
// allow` must not warn about the record it is about to write.
func environment(
	logger *slog.Logger,
	root Root,
	configured, keepsGate bool,
	environ []string,
) (map[string]string, []string, error) {
	set, unset, err := LibraryEnvironment(root, environ, "")
	if err != nil {
		return nil, nil, err
	}

	if configured || set[aquaConfigEnv] != "" {
		return set, unset, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("get the working directory: %w", err)
	}

	projectDir, err := FindProject(wd)
	if err != nil {
		return nil, nil, err
	}

	if projectDir == "" {
		return set, unset, nil
	}

	if !keepsGate {
		policyPath, err := ProjectPolicyPath(logger, root.AquaRoot(), projectDir)
		if err != nil {
			return nil, nil, err
		}

		set, unset, err = LibraryEnvironment(root, environ, policyPath)
		if err != nil {
			return nil, nil, err
		}
	}

	set[aquaConfigEnv] = projectDir + string(os.PathSeparator) + ProjectConfig

	return set, unset, nil
}
