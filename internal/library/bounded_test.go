package library_test

import (
	"testing"

	"github.com/forkcloser/hermetic/internal/library"
)

// Every entry shape a process environment can carry: refused, or passed by.
func TestReadOptionsEveryEntryShape(t *testing.T) {
	t.Parallel()

	refused := []string{
		"HERMETIC_=x",              // the bare prefix is not a known name
		"HERMETIC_ROOT=x",          // a prefix of a known name
		"HERMETIC_ROOT_DIR_X=x",    // a known name as a prefix
		"HERMETIC_GITHUB_TOKEN2=x", // a known name with a suffix
		"HERMETIC_DISABLE_POLICY=true",
		"HERMETIC_X", // no "=": the key is the whole entry
	}

	for _, entry := range refused {
		if _, err := library.ReadOptions([]string{entry}); err == nil {
			t.Errorf("%q was accepted", entry)
		}
	}

	passedBy := []string{
		"hermetic_root_dir=x", // lower case is another name
		"XHERMETIC_ROOT_DIR=x",
		"AQUA_ROOT_DIR=x",
		"=x", // an empty key
		"",   // an empty entry
		"NOEQUALS",
		"PATH=/a=b", // the first "=" splits
	}

	for _, entry := range passedBy {
		if opts, err := library.ReadOptions([]string{entry}); err != nil || opts != (library.Options{}) {
			t.Errorf("%q: %+v, %v", entry, opts, err)
		}
	}

	// The last value of a repeated name wins, whatever its bytes; an empty
	// value is as good as none.
	opts, err := library.ReadOptions([]string{
		"HERMETIC_GITHUB_TOKEN=first",
		"HERMETIC_GITHUB_TOKEN=second=with\nnoise",
		"HERMETIC_LOG_LEVEL=",
		"HERMETIC_CONFIG=",
		"HERMETIC_NO_GLOBAL_CONFIG=",
	})
	if err != nil {
		t.Fatal(err)
	}

	if opts != (library.Options{Token: "second=with\nnoise"}) {
		t.Errorf("options = %+v, want the last token alone", opts)
	}

	// Every spelling strconv.ParseBool takes, and nothing else.
	for _, value := range []string{"true", "TRUE", "True", "t", "T", "1"} {
		if opts, err := library.ReadOptions([]string{"HERMETIC_NO_GLOBAL_CONFIG=" + value}); err != nil ||
			!opts.NoGlobalConfig {
			t.Errorf("NO_GLOBAL_CONFIG=%s: %+v, %v", value, opts, err)
		}
	}

	for _, value := range []string{"false", "0", "F"} {
		if opts, err := library.ReadOptions([]string{"HERMETIC_NO_GLOBAL_CONFIG=" + value}); err != nil ||
			opts.NoGlobalConfig {
			t.Errorf("NO_GLOBAL_CONFIG=%s: %+v, %v", value, opts, err)
		}
	}

	for _, value := range []string{"yes", "no", "on", " true", "true "} {
		if _, err := library.ReadOptions([]string{"HERMETIC_NO_GLOBAL_CONFIG=" + value}); err == nil {
			t.Errorf("NO_GLOBAL_CONFIG=%q was accepted", value)
		}
	}

	// The four levels as slog spells them, and nothing else.
	for _, value := range []string{"debug", "info", "warn", "error"} {
		if opts, err := library.ReadOptions([]string{"HERMETIC_LOG_LEVEL=" + value}); err != nil ||
			opts.LogLevel != value {
			t.Errorf("LOG_LEVEL=%s: %+v, %v", value, opts, err)
		}
	}

	for _, value := range []string{"loud", "INFO", "warning", "info+1", " info"} {
		if _, err := library.ReadOptions([]string{"HERMETIC_LOG_LEVEL=" + value}); err == nil {
			t.Errorf("LOG_LEVEL=%q was accepted", value)
		}
	}
}
