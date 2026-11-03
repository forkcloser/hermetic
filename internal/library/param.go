package library

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/aquaproj/aqua/v2/pkg/config"

	"github.com/forkcloser/hermetic/internal/rootdir"
)

// newParam is what the library's controllers run under. aqua's own command
// layer, which reads its settings file and AQUA_* variables into this struct,
// is not linked: every field is set here. The policy gate and the lazy install
// on, checksums verified and required whatever a config says, the vacuum's
// use tracking off (vacuum is not exposed), no progress bar, the library's
// default parallelism.
func newParam(
	log *slog.Logger,
	root rootdir.Root,
	opts Options,
	configPath, projectPolicy string,
) (*config.Param, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get the working directory: %w", err)
	}
	// A process without a home (a service account's, a bare container's) gets
	// an empty one, as the library's own entry point gives it.
	home, _ := os.UserHomeDir()

	param := &config.Param{
		CWD:                    cwd,
		HomeDir:                home,
		AQUAVersion:            opts.Version,
		ConfigFilePath:         configPath,
		LogLevel:               opts.LogLevel,
		RootDir:                root.AquaRoot(),
		MaxParallelism:         config.GetMaxParallelism("", 0, log),
		Checksum:               true,
		RequireChecksum:        true,
		EnforceChecksum:        true,
		EnforceRequireChecksum: true,
		DisableTracking:        true,
	}

	param.PolicyConfigFilePaths, err = policies(root, projectPolicy)
	if err != nil {
		return nil, err
	}

	if !opts.NoGlobalConfig {
		param.GlobalConfigFilePaths = []string{root.GlobalConfig()}
	}

	return param, nil
}

// policies are the policies the library trusts on sight: the root's, the
// toolset's own, when the root holds one, then the project's when one passed
// the gate. A root with no toolset in it, a runner's or a fresh session's, has
// no policy of its own, and the library refuses a listed file that is not
// there; so the list is of what exists, never of what a root should have. A
// root policy that cannot be looked for is an error, never an absence.
func policies(root rootdir.Root, projectPolicy string) ([]string, error) {
	var paths []string

	switch _, err := os.Stat(root.Policy()); {
	case err == nil:
		paths = append(paths, root.Policy())
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("look for the root policy: %w", err)
	}

	if projectPolicy != "" {
		paths = append(paths, projectPolicy)
	}

	return paths, nil
}
