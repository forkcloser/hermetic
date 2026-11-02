package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aquaproj/aqua/v2/pkg/config"
	"github.com/aquaproj/aqua/v2/pkg/controller"
	aquawhich "github.com/aquaproj/aqua/v2/pkg/controller/which"
	aquaruntime "github.com/aquaproj/aqua/v2/pkg/runtime"

	"github.com/forkcloser/hermetic/internal/dispatch"
	"github.com/forkcloser/hermetic/internal/projectdir"
	"github.com/forkcloser/hermetic/internal/rootdir"
)

// Library is what a command runs the library with: its logger and runtime, the
// parameters the binary derived for it, and the stream a command answers on.
type Library struct {
	Logger  *slog.Logger
	Root    rootdir.Root
	Runtime *aquaruntime.Runtime
	Stdout  io.Writer

	base *config.Param
}

// Param is a copy of the parameters the binary derived, for a command to fill
// its own fields in.
func (lib *Library) Param() *config.Param {
	param := *lib.base

	return &param
}

// New is the library as a command runs it: the root, the environment applied
// to the process, which the verifiers the library runs inherit, the project
// walk and its gate, and the parameters derived from that environment.
// configFlag is the config the command names; keepsGate is a policy
// subcommand's, which runs without the gate it keeps.
func New(
	ctx context.Context,
	log *slog.Logger,
	getenv func(string) string,
	stdout io.Writer,
	version, configFlag string,
	keepsGate bool,
	level string,
) (*Library, error) {
	root, err := rootdir.From(getenv)
	if err != nil {
		return nil, err
	}

	set, unset, err := projectEnvironment(root, configFlag, keepsGate, os.Environ())
	if err != nil {
		return nil, err
	}

	if applyErr := apply(set, unset); applyErr != nil {
		return nil, applyErr
	}

	base, err := libraryParam(log, root, set, version, configFlag, level)
	if err != nil {
		return nil, err
	}

	return &Library{
		Logger:  log,
		Root:    root,
		Runtime: aquaruntime.New(ctx),
		Stdout:  stdout,
		base:    base,
	}, nil
}

// Which asks the library where a command lives: the registries installed, the
// package's path named whether it is installed yet or not, and, for a name no
// config has, the PATH's answer.
func (lib *Library) Which(ctx context.Context, param *config.Param, tool string) (*aquawhich.FindResult, error) {
	ctrl, err := controller.InitializeWhichCommandController(ctx, lib.Logger, param, http.DefaultClient, lib.Runtime)
	if err != nil {
		return nil, fmt.Errorf("initialize the which controller: %w", err)
	}

	found, err := ctrl.Which(ctx, lib.Logger.With("exe_name", tool), param, tool)
	if err != nil {
		return nil, err
	}

	return found, nil
}

// Resolve finds the tool a dispatch runs: which for the path, past the root's
// own link, then install for the package when it is not there yet, the
// first-use install a links-only root relies on.
func (lib *Library) Resolve(ctx context.Context, tool string) (string, error) {
	found, err := lib.Which(ctx, lib.Param(), tool)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}

	path, err := dispatch.PastLink(lib.Root, found.ExePath, tool)
	if err != nil {
		return "", err
	}

	if _, statErr := os.Stat(path); statErr == nil {
		return path, nil
	}
	// All: the global config's packages too, since without it the library
	// reads the working directory's config alone and the toolset's pins are
	// global.
	param := lib.Param()
	param.All = true
	param.Args = []string{tool}

	ctrl, err := controller.InitializeInstallCommandController(ctx, lib.Logger, param, http.DefaultClient, lib.Runtime)
	if err != nil {
		return "", fmt.Errorf("initialize the install controller: %w", err)
	}

	if err := ctrl.Install(ctx, lib.Logger, param); err != nil {
		return "", fmt.Errorf("install %s: %w", tool, err)
	}

	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%w after installing: %s", dispatch.ErrNoPath, tool)
	}

	return path, nil
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

// projectEnvironment is what the library's run sets and unsets, and AQUA_CONFIG with
// the project's policy when it passes the gate. The project is the one the
// run names (configFlag: -c, or HERMETIC_CONFIG, already in set), whose
// directory holds its policy as a walked one does, so a named config carries
// its project and a job that names the checkout's manifest resolves from any
// working directory as it would from the checkout; a manifest with no policy
// beside it, a scratch copy, has none to carry. Without a named config the
// project is the walk's, upward from the working directory. The policy
// subcommands are the gate's own keepers (keepsGate): `policy allow` must not
// warn about the record it is about to write.
func projectEnvironment(
	root rootdir.Root,
	configFlag string,
	keepsGate bool,
	environ []string,
) (map[string]string, []string, error) {
	set, unset, err := Environment(root, environ, "")
	if err != nil {
		return nil, nil, err
	}

	named := configFlag
	if named == "" {
		named = set[aquaConfigEnv]
	}

	if named != "" && isRootManifest(named, root) {
		return set, unset, nil
	}

	projectDir, err := projectOf(named)
	if err != nil {
		return nil, nil, err
	}

	if projectDir == "" {
		return set, unset, nil
	}

	if !keepsGate {
		policyPath, err := projectdir.PolicyPath(root.AquaRoot(), projectDir)
		// Denied by the walk: the repository's config is ignored and the
		// root's pins answer. Denied but named outright, the refusal stands:
		// naming the config asks for it.
		if refused, ok := errors.AsType[*projectdir.RefusedPolicyError](err); ok && refused.Denied && named == "" {
			return set, unset, nil
		}

		if err != nil {
			return nil, nil, err
		}

		set, unset, err = Environment(root, environ, policyPath)
		if err != nil {
			return nil, nil, err
		}
	}

	if named == "" {
		set[aquaConfigEnv] = projectDir + string(os.PathSeparator) + projectdir.Config
	}

	return set, unset, nil
}

// isRootManifest reports whether the named config is the root's own manifest,
// the machine toolset's pins: the policy beside it is the global policy the
// library reads on every run, so it names no project and nothing of it goes
// through the gate. Compared as files, since a root is reached through
// symlinked paths (macOS's /tmp) as often as not.
func isRootManifest(named string, root rootdir.Root) bool {
	manifest, err := os.Stat(named)
	if err != nil {
		return false
	}

	global, err := os.Stat(root.GlobalConfig())
	if err != nil {
		return false
	}

	return os.SameFile(manifest, global)
}

// projectOf is the project directory a run works in: the directory of the
// config it names, made absolute as the library resolves a named config, or
// the walk's from the working directory when none is named; "" when neither
// gives one.
func projectOf(named string) (string, error) {
	if named != "" {
		abs, err := filepath.Abs(named)
		if err != nil {
			return "", fmt.Errorf("resolve the named config: %w", err)
		}

		return filepath.Dir(abs), nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get the working directory: %w", err)
	}

	return projectdir.Find(cwd)
}
