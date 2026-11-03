package library

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Options is what a run of the library is made from: the HERMETIC_* variables
// (ReadOptions), then what the command line adds.
type Options struct {
	// RootDir names the root; empty is the platform's default (rootdir.From).
	RootDir string
	// Config names the project config outright, like -c: the walk stays out,
	// and the policy beside it goes through the gate as a walked one does.
	Config string
	// NoGlobalConfig leaves the root's pins out of the run.
	NoGlobalConfig bool
	// Token is the GitHub token the library and its verifiers use, the only one
	// they see.
	Token string
	// LogLevel is the library's: debug, info, warn or error (logLevels).
	LogLevel string
	// Version is the binary's, which the library reports as its own.
	Version string
	// KeepsGate is a policy subcommand's, which runs without the gate it keeps.
	KeepsGate bool
}

// The HERMETIC_* variables the binary reads, and nothing else of that prefix.
const (
	prefix            = "HERMETIC_"
	rootDirVar        = prefix + "ROOT_DIR"
	configVar         = prefix + "CONFIG"
	noGlobalConfigVar = prefix + "NO_GLOBAL_CONFIG"
	tokenVar          = prefix + "GITHUB_TOKEN"
	logLevelVar       = prefix + "LOG_LEVEL"
)

// logLevels are the levels LogLevel takes, as slog spells them.
//
//nolint:gochecknoglobals // immutable data.
var logLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}

var (
	errUnknownVariable = errors.New("unknown " + prefix + "* variable")
	errNotBoolean      = errors.New(noGlobalConfigVar + " is not a boolean")
	errNotLevel        = errors.New(logLevelVar + " is not a log level (debug, info, warn, error)")
)

// ReadOptions reads the HERMETIC_* variables of environ. Any other name with
// the prefix is refused, so a misspelling or a knob the binary does not expose
// fails loud. The last value of a repeated name wins, as a process environment
// reads, and an empty value is none; a log level is one of logLevels.
func ReadOptions(environ []string) (Options, error) {
	var (
		opts     Options
		noGlobal string
	)

	for _, entry := range environ {
		key, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		switch key {
		case rootDirVar:
			opts.RootDir = value
		case configVar:
			opts.Config = value
		case noGlobalConfigVar:
			noGlobal = value
		case tokenVar:
			opts.Token = value
		case logLevelVar:
			opts.LogLevel = value
		default:
			return Options{}, fmt.Errorf("%w: %s", errUnknownVariable, key)
		}
	}

	if noGlobal != "" {
		value, err := strconv.ParseBool(noGlobal)
		if err != nil {
			return Options{}, fmt.Errorf("%w: %q", errNotBoolean, noGlobal)
		}

		opts.NoGlobalConfig = value
	}

	if opts.LogLevel != "" && !logLevels[opts.LogLevel] {
		return Options{}, fmt.Errorf("%w: %q", errNotLevel, opts.LogLevel)
	}

	return opts, nil
}
