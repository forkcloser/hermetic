package library_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/library"
)

func FuzzReadOptions(f *testing.F) {
	f.Add("AQUA_ROOT_DIR=/theirs\nHERMETIC_GITHUB_TOKEN=t\nPATH=/bin")
	f.Add("HERMETIC_NO_GLOBAL_CONFIG=true\nGH_TOKEN=x\n=\n\nNOEQ")
	f.Add("HERMETIC_LOG_LEVEL=warn\nHERMETIC_LOG_LEVEL=loud")
	f.Add("HERMETIC_TYPO=1")

	f.Fuzz(func(t *testing.T, raw string) {
		environ := strings.Split(raw, "\n")

		opts, err := library.ReadOptions(environ)

		// The model: the last value of each known name, and a refusal for an
		// unknown name of the prefix, a boolean strconv cannot read, or a
		// level outside the four.
		last := map[string]string{}
		unknown := false

		for _, entry := range environ {
			key, value, _ := strings.Cut(entry, "=")

			switch key {
			case "HERMETIC_ROOT_DIR", "HERMETIC_CONFIG", "HERMETIC_NO_GLOBAL_CONFIG",
				"HERMETIC_GITHUB_TOKEN", "HERMETIC_LOG_LEVEL":
				last[key] = value
			default:
				unknown = unknown || strings.HasPrefix(key, "HERMETIC_")
			}
		}

		noGlobal, boolErr := strconv.ParseBool(last["HERMETIC_NO_GLOBAL_CONFIG"])
		badBool := last["HERMETIC_NO_GLOBAL_CONFIG"] != "" && boolErr != nil
		level := last["HERMETIC_LOG_LEVEL"]
		badLevel := level != "" && !slices.Contains([]string{"debug", "info", "warn", "error"}, level)

		if unknown || badBool || badLevel {
			if err == nil {
				t.Fatalf("accepted %q: %+v", raw, opts)
			}

			return
		}

		if err != nil {
			t.Fatalf("refused %q: %v", raw, err)
		}

		want := library.Options{
			RootDir:        last["HERMETIC_ROOT_DIR"],
			Config:         last["HERMETIC_CONFIG"],
			NoGlobalConfig: noGlobal,
			Token:          last["HERMETIC_GITHUB_TOKEN"],
			LogLevel:       level,
		}
		if opts != want {
			t.Fatalf("%q: %+v, want %+v", raw, opts, want)
		}
	})
}
