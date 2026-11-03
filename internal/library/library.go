package library

import (
	"context"
	"errors"
	"fmt"
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

// Library is what a command runs the library with: its logger and runtime,
// the root, and the parameters the binary derived for it.
type Library struct {
	Logger  *slog.Logger
	Root    rootdir.Root
	Runtime *aquaruntime.Runtime

	base *config.Param
}

// Param is a copy of the parameters the binary derived, for a command to fill
// its own fields in.
func (lib *Library) Param() *config.Param {
	param := *lib.base

	return &param
}

// New is the library as a command runs it: the root, the project the run
// names or walks to and its gate, the environment its verifiers inherit,
// applied to the process, and the parameters. It logs through slog's default
// logger.
func New(ctx context.Context, opts Options) (*Library, error) {
	root, err := rootdir.From(opts.RootDir)
	if err != nil {
		return nil, err
	}

	configPath, projectPolicy, err := project(root, opts.Config, opts.KeepsGate)
	if err != nil {
		return nil, err
	}

	err = setEnvironment(root, opts.Token, os.Environ())
	if err != nil {
		return nil, err
	}

	log := slog.Default()

	base, err := newParam(log, root, opts, configPath, projectPolicy)
	if err != nil {
		return nil, err
	}

	return &Library{
		Logger:  log,
		Root:    root,
		Runtime: aquaruntime.New(ctx),
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

// Resolve finds the tool a dispatch runs: which for the path, past the links
// to self, the root's binary (dispatch.PastLink), then install for the package
// when it is not there yet, the first-use install a links-only root relies on.
func (lib *Library) Resolve(ctx context.Context, tool, self string) (string, error) {
	found, err := lib.Which(ctx, lib.Param(), tool)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}

	path, err := dispatch.PastLink(self, found.ExePath, tool)
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

// project is the config a run reads and the project policy that joins the
// root's, once it passed the gate. The project is the one the run names
// (named: -c or HERMETIC_CONFIG), whose directory holds its policy as a walked
// one does, so a named config carries its project and a job that names the
// checkout's manifest resolves from any working directory as it would from the
// checkout; a manifest with no policy beside it, a scratch copy, has none to
// carry. Without a named config the project is the walk's, upward from the
// working directory. The policy subcommands are the gate's own keepers
// (keepsGate): `policy allow` must not be refused the record it is about to
// write.
func project(root rootdir.Root, named string, keepsGate bool) (configPath, policy string, err error) {
	if named != "" && isRootManifest(named, root) {
		return named, "", nil
	}

	projectDir, err := projectOf(named)
	if err != nil || projectDir == "" {
		return named, "", err
	}

	if !keepsGate {
		policy, err = projectdir.PolicyPath(root.AquaRoot(), projectDir)
		// Denied by the walk: the repository's config is ignored and the
		// root's pins answer. Denied but named outright, the refusal stands:
		// naming the config asks for it.
		if refused, ok := errors.AsType[*projectdir.RefusedPolicyError](err); ok && refused.Denied && named == "" {
			return "", "", nil
		}

		if err != nil {
			return "", "", err
		}
	}

	if named == "" {
		return filepath.Join(projectDir, projectdir.Config), policy, nil
	}

	return named, policy, nil
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
