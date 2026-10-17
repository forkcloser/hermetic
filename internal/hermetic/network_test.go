package hermetic_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// asBinary, set in a child's environment, makes the test binary run as the
// binary itself: Main, under the name the dispatch reads from os.Args[0].
const asBinary = "TEST_AS_THE_BINARY"

func TestMain(m *testing.M) {
	if os.Getenv(asBinary) == "1" {
		hermetic.Main("")

		return
	}

	m.Run()
}

// TestDownloadsRefuseTLS12 holds the library's downloads to the process's
// TLS: a package served over TLS 1.2 only is refused at the handshake. Go's
// default client would complete it and fail later, on the test server's
// untrusted certificate; the refusal is what shows the library ran on the
// replaced client. update-checksum downloads the asset to sum it and fetches
// nothing else, so the run stays on the local server.
func TestDownloadsRefuseTLS12(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "never served")
	}))
	server.TLS = &tls.Config{
		MaxVersion: tls.VersionTLS12,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	binary := binaryNamed(t)

	root := t.TempDir()
	write(
		t,
		filepath.Join(root, "policy.yaml"),
		"registries:\n  - type: standard\n    ref: semver(\">= 3.0.0\")\npackages:\n  - registry: standard\n",
	)

	project := t.TempDir()
	write(
		t,
		filepath.Join(project, hermetic.ProjectDir, hermetic.ProjectConfig),
		"checksum:\n  enabled: true\nregistries:\n  - name: local\n    type: local\n    path: registry.yaml\npackages:\n  - name: probe/probe@v1.0.0\n    registry: local\n",
	)
	write(
		t,
		filepath.Join(project, hermetic.ProjectDir, "registry.yaml"),
		"packages:\n  - type: http\n    repo_owner: probe\n    repo_name: probe\n    url: "+server.URL+"/probe-{{.Version}}.tar.gz\n    format: tar.gz\n",
	)
	write(t, filepath.Join(project, hermetic.ProjectDir, hermetic.ProjectPolicy),
		"registries:\n  - name: local\n    type: local\n    path: registry.yaml\npackages:\n  - registry: local\n")

	env := append(os.Environ(), asBinary+"=1", hermetic.RootEnv+"="+root)

	allow := exec.CommandContext(
		t.Context(),
		binary,
		"policy",
		"allow",
		filepath.Join(hermetic.ProjectDir, hermetic.ProjectPolicy),
	)

	allow.Dir, allow.Env = project, env
	if out, err := allow.CombinedOutput(); err != nil {
		t.Fatalf("policy allow: %v\n%s", err, out)
	}

	// The library retries a failed download with its own backoff; the first
	// refusal is the verdict, so the child is stopped there.
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	run := exec.CommandContext(ctx, binary, "update-checksum")
	run.Dir, run.Env = project, env

	stderr, err := run.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := run.Start(); err != nil {
		t.Fatal(err)
	}

	var seen []string

	refused := false

	for lines := bufio.NewScanner(stderr); lines.Scan(); {
		line := lines.Text()
		seen = append(seen, line)

		if strings.Contains(line, "x509:") {
			break
		}

		if strings.Contains(line, "protocol version not supported") {
			refused = true

			break
		}
	}

	cancel()

	_ = run.Wait()

	if !refused {
		t.Fatalf(
			"update-checksum against a TLS 1.2 server: want the handshake refused, got\n%s",
			strings.Join(seen, "\n"),
		)
	}
}

// binaryNamed links the test binary under the binary's own name, which the
// dispatch requires: any other name is a tool to run.
func binaryNamed(t *testing.T) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	name := hermetic.Name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	link := filepath.Join(t.TempDir(), name)
	if err := os.Link(self, link); err != nil {
		t.Fatalf("link the test binary as %s: %v", name, err)
	}

	return link
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
