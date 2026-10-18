package hermetic

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/aquaproj/aqua/v2/pkg/config"
	finder "github.com/aquaproj/aqua/v2/pkg/config-finder"
	"github.com/aquaproj/aqua/v2/pkg/policy"
)

// libraryParam is what the library's controllers run under, derived from the
// environment the binary set for them (set, as LibraryEnvironment returns it)
// and nothing else: not the library's own flag layer, and not its settings
// file, which stays the human's because every value it could supply is pinned
// in set. A command that names its config (configFlag) wins over the project
// walk's AQUA_CONFIG.
func libraryParam(
	logger *slog.Logger,
	root Root,
	set map[string]string,
	version, configFlag, logLevel string,
) (*config.Param, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get the working directory: %w", err)
	}
	// A process without a home (a service account's, a bare container's) gets
	// an empty one, as the library's own entry point gives it.
	home, _ := os.UserHomeDir()

	param := &config.Param{
		CWD:                   cwd,
		HomeDir:               home,
		AQUAVersion:           version,
		ConfigFilePath:        configFlag,
		LogLevel:              logLevel,
		LogColor:              set[aquaPrefix+"LOG_COLOR"],
		RootDir:               root.AquaRoot(),
		MaxParallelism:        config.GetMaxParallelism(set[aquaPrefix+"MAX_PARALLELISM"], 0, logger),
		GlobalConfigFilePaths: finder.ParseGlobalConfigFilePaths(cwd, set[aquaGlobalConfigEnv]),
	}

	if param.ConfigFilePath == "" {
		param.ConfigFilePath = set[aquaConfigEnv]
	}

	for _, flag := range []struct {
		key    string
		target *bool
	}{
		{"PROGRESS_BAR", &param.ProgressBar},
		{"DISABLE_LAZY_INSTALL", &param.DisableLazyInstall},
		{"DISABLE_POLICY", &param.DisablePolicy},
		{"DISABLE_TRACKING", &param.DisableTracking},
		{"CHECKSUM", &param.Checksum},
		{"REQUIRE_CHECKSUM", &param.RequireChecksum},
		{"ENFORCE_CHECKSUM", &param.EnforceChecksum},
		{"ENFORCE_REQUIRE_CHECKSUM", &param.EnforceRequireChecksum},
	} {
		value, err := strconv.ParseBool(set[aquaPrefix+flag.key])
		if err != nil {
			return nil, fmt.Errorf("%s%s is not a boolean: %w", aquaPrefix, flag.key, err)
		}

		*flag.target = value
	}

	if !param.DisablePolicy {
		param.PolicyConfigFilePaths = policy.ParseEnv(set[aquaPolicyConfigEnv])
		for i, p := range param.PolicyConfigFilePaths {
			if !filepath.IsAbs(p) {
				param.PolicyConfigFilePaths[i] = filepath.Join(cwd, p)
			}
		}
	}

	return param, nil
}
