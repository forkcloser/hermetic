package projectdir_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/projectdir"
)

func TestFindProject(t *testing.T) {
	t.Parallel()

	top := t.TempDir()
	project := filepath.Join(top, "repo")
	deep := filepath.Join(project, "a", "b")

	if err := os.MkdirAll(filepath.Join(project, projectdir.Dir), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}

	config := filepath.Join(project, projectdir.Dir, projectdir.Config)
	if err := os.WriteFile(config, []byte("packages: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := projectdir.Find(deep)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(project, projectdir.Dir); got != want {
		t.Fatalf("from a subdirectory: %q, want %q", got, want)
	}

	// The directory alone, without the config inside, is not a project.
	bare := filepath.Join(top, "bare")
	if mkErr := os.MkdirAll(filepath.Join(bare, projectdir.Dir), 0o750); mkErr != nil {
		t.Fatal(mkErr)
	}

	got, err = projectdir.Find(bare)
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Fatalf("a bare directory found %q, want none", got)
	}
}

// The nearest project wins, a project is its config and not its directory,
// and a working directory that does not exist is walked from where it would
// be.
func TestFindProjectNearestWins(t *testing.T) {
	t.Parallel()

	top := t.TempDir()
	outer := filepath.Join(top, "outer")
	inner := filepath.Join(outer, "a", "inner")
	below := filepath.Join(inner, "b", "c")

	for _, project := range []string{outer, inner} {
		write(t, filepath.Join(project, projectdir.Dir, projectdir.Config), "packages: []\n")
	}

	if err := os.MkdirAll(below, 0o750); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		below:                     filepath.Join(inner, projectdir.Dir),
		inner:                     filepath.Join(inner, projectdir.Dir),
		filepath.Join(outer, "a"): filepath.Join(outer, projectdir.Dir),
		outer:                     filepath.Join(outer, projectdir.Dir),
		top:                       "",
		filepath.Join(below, "does", "not", "exist"): filepath.Join(inner, projectdir.Dir),
		filepath.Join(inner, projectdir.Dir):         filepath.Join(inner, projectdir.Dir),
	}

	for wd, want := range cases {
		got, err := projectdir.Find(wd)
		if err != nil {
			t.Errorf("%s: %v", wd, err)

			continue
		}

		if got != want {
			t.Errorf("FindProject(%s) = %q, want %q", wd, got, want)
		}
	}
}

func TestProjectPolicyPath(t *testing.T) {
	t.Parallel()

	top := t.TempDir()
	aquaRoot := filepath.Join(top, "store")
	project := filepath.Join(top, "repo", projectdir.Dir)

	if err := os.MkdirAll(project, 0o750); err != nil {
		t.Fatal(err)
	}

	policy := filepath.Join(project, projectdir.Policy)
	body := []byte("registries:\n  - type: standard\n")

	if err := os.WriteFile(policy, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// No record: refused, and the refusal is this binary's allow command for
	// the policy's absolute path, never aqua's.
	refused := refusal(t, aquaRoot, project)
	if refused.Policy != policy || refused.Denied {
		t.Fatalf("without a record: %+v, want the policy, not denied", refused)
	}

	if want := "the project policy is not allowed: " + policy; refused.Error() != want {
		t.Fatalf("the refusal reads %q, want %q", refused.Error(), want)
	}

	// The record `policy allow` writes: the policy's copy under policies/ at
	// its absolute path (the drive's colon an underscore on Windows).
	record := filepath.Join(aquaRoot, "policies", strings.ReplaceAll(policy, ":", "_"))
	if err := os.MkdirAll(filepath.Dir(record), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(record, body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := projectdir.PolicyPath(aquaRoot, project)
	if err != nil {
		t.Fatal(err)
	}

	if got != policy {
		t.Fatalf("with a matching record: %q, want %q", got, policy)
	}

	// A changed policy: the record is stale, the policy is refused again.
	changed := append(slices.Clone(body), []byte("  - name: evil\n    type: local\n    path: x.yaml\n")...)
	if err := os.WriteFile(policy, changed, 0o600); err != nil {
		t.Fatal(err)
	}

	if stale := refusal(t, aquaRoot, project); stale.Policy != policy || stale.Denied {
		t.Fatalf("with a stale record: %+v, want refused, not denied", stale)
	}

	// The record `policy deny` writes: the refusal carries the human's choice.
	denied := filepath.Join(aquaRoot, "policy-warnings", strings.ReplaceAll(policy, ":", "_"))
	if err := os.MkdirAll(filepath.Dir(denied), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(denied, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if quiet := refusal(t, aquaRoot, project); quiet.Policy != policy || !quiet.Denied {
		t.Fatalf("with a deny record: %+v, want denied", quiet)
	}

	// A project without a policy file has nothing to gate.
	if err := os.Remove(policy); err != nil {
		t.Fatal(err)
	}

	if none, noneErr := projectdir.PolicyPath(aquaRoot, project); noneErr != nil || none != "" {
		t.Fatalf("without a policy: %q, %v", none, noneErr)
	}
}

// refusal is the gate's refusal for the project, which the test requires.
func refusal(t *testing.T, aquaRoot, project string) *projectdir.RefusedPolicyError {
	t.Helper()

	got, err := projectdir.PolicyPath(aquaRoot, project)

	refused, ok := errors.AsType[*projectdir.RefusedPolicyError](err)
	if !ok || got != "" {
		t.Fatalf("ProjectPolicyPath = %q, %v; want a refusal", got, err)
	}

	return refused
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
