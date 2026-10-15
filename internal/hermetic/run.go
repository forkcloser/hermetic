package hermetic

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	aquaruntime "github.com/aquaproj/aqua/v2/pkg/runtime"
	"github.com/suzuki-shunsuke/slog-util/slogutil"
	"github.com/suzuki-shunsuke/urfave-cli-v3-util/urfave"
)

// Main runs the binary the way aqua's own main runs aqua, with ours around the
// library: the root, the environment, the project walk and its gate before,
// the mirror after; and the dispatch, which resolves a tool through the
// library and then runs it itself.
func Main(version string) {
	urfave.Main(Name, version, Run, "env", aquaruntime.New(context.Background()).Env())
}

// Run is one invocation of the binary, as Main runs it: env is the process's
// own (its arguments, its streams, its environment).
func Run(ctx context.Context, logger *slogutil.Logger, env *urfave.Env) error {
	root, err := RootFrom(env.Getenv)
	if err != nil {
		return err
	}

	tool, toolArgs := Dispatched(env.Args, runtime.GOOS)
	libraryArgs := env.Args

	if tool != "" {
		libraryArgs = []string{env.Args[0], "which", tool}
	}

	set, unset, err := environment(logger.Logger, root, libraryArgs, os.Environ())
	if err != nil {
		return err
	}
	// The library's view: the process environment (which the verifiers it runs
	// inherit) and the Env it was handed both see ours and nothing scrubbed.
	// The shell's own environment is kept for the tool a dispatch runs.
	shell := os.Environ()

	if applyErr := apply(set, unset); applyErr != nil {
		return applyErr
	}

	getenv := env.Getenv
	env.Getenv = func(key string) string {
		if v, ok := set[key]; ok {
			return v
		}

		if scrubbed(key) {
			return ""
		}

		return getenv(key)
	}

	if tool == "" {
		runErr := runLibrary(ctx, logger, env)
		if installs(env.Args) {
			syncBin(ctx, logger.Logger, root)
		}

		return runErr
	}

	path, err := resolve(ctx, logger, env, root, tool)
	if err != nil {
		return err
	}

	syncBin(ctx, logger.Logger, root)

	return execTool(ctx, path, tool, toolArgs, ToolEnvironment(shell))
}

// resolve finds the tool through the library under the library's environment:
// `which` for the path (it installs the registries and names where the package
// lives, installed or not), then `install <tool>` when the package is not there
// yet, which is the first-use install a links-only root relies on. A path that
// is this very binary is refused: `which` falls back to the PATH for a name no
// config has, where a link of ours would make the dispatch run itself forever.
func resolve(ctx context.Context, logger *slogutil.Logger, env *urfave.Env, root Root, tool string) (string, error) {
	path, err := which(ctx, logger, env, tool)
	if err != nil {
		return "", err
	}

	binary, err := os.Stat(root.Binary(runtime.GOOS))
	if err != nil {
		return "", fmt.Errorf("the binary: %w", err)
	}

	info, err := os.Stat(path)
	if err == nil && os.SameFile(info, binary) {
		return "", fmt.Errorf("%w: %s", errNoPath, tool)
	}

	if err == nil {
		return path, nil
	}

	// -a: the global config's packages too, since without it the library reads
	// the working directory's config alone and the toolset's pins are global.
	install := *env
	install.Args = []string{env.Args[0], "install", "-a", tool}

	if err := runLibrary(ctx, logger, &install); err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%w after installing: %s", errNoPath, tool)
	}

	return path, nil
}

// which asks the library for the tool's path. The library prints it on the
// process's stdout, so that is swapped for a pipe for the call.
func which(ctx context.Context, logger *slogutil.Logger, env *urfave.Env, tool string) (string, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("pipe for the tool's path: %w", err)
	}

	captured := make(chan string, 1)

	go func() {
		out, _ := io.ReadAll(reader)
		captured <- strings.TrimSpace(string(out))
	}()

	which := *env
	which.Stdout = writer
	which.Args = []string{env.Args[0], "which", tool}

	stdout := os.Stdout
	os.Stdout = writer //nolint:reassign // the library prints the path on os.Stdout itself, not on the Env's; the swap is how it is captured.
	runErr := runLibrary(ctx, logger, &which)
	os.Stdout = stdout //nolint:reassign // restored.

	_ = writer.Close()
	path := <-captured
	_ = reader.Close()

	if runErr != nil {
		return "", runErr
	}

	if path == "" {
		return "", fmt.Errorf("%w: %s", errNoPath, tool)
	}

	return path, nil
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
// names its config itself (-c, or HERMETIC_CONFIG), AQUA_CONFIG from the
// project walk, with the project's policy when it passes the gate.
func environment(logger *slog.Logger, root Root, args, environ []string) (map[string]string, []string, error) {
	set, unset, err := LibraryEnvironment(root, environ, "")
	if err != nil {
		return nil, nil, err
	}

	if hasConfigFlag(args) || set[aquaConfigEnv] != "" {
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
	// The policy subcommands are the gate's own keeper: `policy allow` must not
	// warn about the record it is about to write.
	if subcommand(args) != "policy" {
		policyPath, err := ProjectPolicyPath(logger, root.AquaRoot(), projectDir)
		if err != nil {
			return nil, nil, err
		}

		set, unset, err = LibraryEnvironment(root, environ, policyPath)
		if err != nil {
			return nil, nil, err
		}
	}

	set[aquaConfigEnv] = filepath.Join(projectDir, ProjectConfig)

	return set, unset, nil
}
