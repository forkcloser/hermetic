package hermetic_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/hermetic/internal/hermetic"
)

// TestDispatchPassesOverItsOwnLink holds a dispatch for a name no config has
// to what the system gives: the library's PATH fallback finds the root's own
// link first, which is this binary, and the dispatch passes it over for the
// next entry on the PATH, so a script outside any project gets the system's
// tool through the link, as it did through aqua's; a name with nothing past
// the link is refused rather than dispatched forever.
func TestDispatchPassesOverItsOwnLink(t *testing.T) {
	t.Parallel()

	rig := linkedRoot(t)
	tool := "fakehead"

	link(t, rig.root, tool)

	run := exec.CommandContext(t.Context(), filepath.Join(rig.root.Bin(), exe(tool)))
	run.Dir, run.Env = rig.outside, rig.env

	out, err := run.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "fake" {
		t.Fatalf("dispatch of %s: %v\n%s", tool, err, out)
	}

	link(t, rig.root, "nothingpast")

	refused := exec.CommandContext(t.Context(), filepath.Join(rig.root.Bin(), exe("nothingpast")))
	refused.Dir, refused.Env = rig.outside, rig.env

	out, err = refused.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "resolved no path") {
		t.Fatalf("a name with nothing past the link: err = %v, want a refusal\n%s", err, out)
	}
}

// TestWhichAnswersPastItsOwnLink holds `which` to the dispatch's answer: for a
// name no config has, the path printed is the system's tool past the root's
// own link, never the link, so `$(hermetic which <tool>)` in a script names
// what runs; a name with nothing past the link is refused the same way.
func TestWhichAnswersPastItsOwnLink(t *testing.T) {
	t.Parallel()

	rig := linkedRoot(t)
	tool := "fakehead"

	link(t, rig.root, tool)

	run := exec.CommandContext(t.Context(), rig.root.Binary(runtime.GOOS), "which", tool)
	run.Dir, run.Env = rig.outside, rig.env

	out, err := run.Output()
	if err != nil || strings.TrimSpace(string(out)) != rig.fake {
		t.Fatalf("which %s = %q, %v, want %s\n%s", tool, out, err, rig.fake, exitStderr(err))
	}

	link(t, rig.root, "nothingpast")

	refused := exec.CommandContext(t.Context(), rig.root.Binary(runtime.GOOS), "which", "nothingpast")
	refused.Dir, refused.Env = rig.outside, rig.env

	out, err = refused.Output()
	if err == nil || !strings.Contains(exitStderr(err), "resolved no path") {
		t.Fatalf("which of a name with nothing past the link = %q, %v, want a refusal\n%s", out, err, exitStderr(err))
	}
}

// TestWhichFromARootWithoutABinary holds `which` where the root holds no
// binary, so no link can be the binary: the library's PATH answer for a name
// no config has stands as is, rather than being refused for a binary to pass
// over that is not there.
func TestWhichFromARootWithoutABinary(t *testing.T) {
	t.Parallel()

	system := t.TempDir()
	tool, name := "fakehead", "fakehead"

	if runtime.GOOS == "windows" {
		name = "fakehead.cmd"
	}

	fake := filepath.Join(system, name)
	write(t, fake, "#!/bin/sh\necho fake\n")

	if err := os.Chmod(fake, 0o700); err != nil {
		t.Fatal(err)
	}

	path := strings.Join([]string{system, "/bin", "/usr/bin"}, string(os.PathListSeparator))
	env := append(os.Environ(), asBinary+"=1", hermetic.RootEnv+"="+t.TempDir(), "PATH="+path)

	run := exec.CommandContext(t.Context(), binaryNamed(t), "which", tool)
	run.Dir, run.Env = t.TempDir(), env

	out, err := run.Output()
	if err != nil || strings.TrimSpace(string(out)) != fake {
		t.Fatalf("which %s from a root without a binary = %q, %v, want %s\n%s", tool, out, err, fake, exitStderr(err))
	}
}

// exitStderr is what the child wrote to stderr when err is its exit.
func exitStderr(err error) string {
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return string(exit.Stderr)
	}

	return ""
}

// linkedRig is a scratch root whose binary is this test binary, the
// environment that runs the test binary as the binary on a PATH a shell has,
// the root's bin first, decoys, then a system directory holding a fake tool at
// fake, and a working directory outside any project.
type linkedRig struct {
	root    hermetic.Root
	env     []string
	outside string
	fake    string
}

func linkedRoot(t *testing.T) linkedRig {
	t.Helper()

	self, execErr := os.Executable()
	if execErr != nil {
		t.Fatal(execErr)
	}

	root := hermetic.Root(t.TempDir())
	if err := os.MkdirAll(root.Bin(), 0o750); err != nil {
		t.Fatal(err)
	}
	// The root's binary is this test binary, as a hard link: SameFile with
	// what the links resolve to, as in a root the installer wrote.
	if err := os.Link(self, root.Binary(runtime.GOOS)); err != nil {
		t.Fatal(err)
	}

	system := t.TempDir()
	tool, name := "fakehead", "fakehead"
	script := "#!/bin/sh\necho fake\n"

	if runtime.GOOS == "windows" {
		name, script = "fakehead.cmd", "@echo fake\r\n"
	}

	fake := filepath.Join(system, name)
	write(t, fake, script)

	if err := os.Chmod(fake, 0o700); err != nil {
		t.Fatal(err)
	}
	// Two decoys ahead of the script on the PATH, each under the tool's name: a
	// directory, and off Windows, where the bit means nothing, a file without
	// the executable bit. Neither is what a shell would run, and neither is
	// taken.
	dirDecoy := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirDecoy, tool), 0o750); err != nil {
		t.Fatal(err)
	}

	ahead := []string{root.Bin(), dirDecoy}

	if runtime.GOOS != "windows" {
		fileDecoy := t.TempDir()
		write(t, filepath.Join(fileDecoy, tool), script)
		ahead = append(ahead, fileDecoy)
	}

	// A dispatch mirrors aqua's bin into the root's after it resolves, and this
	// scratch root exposes nothing, so each run gets its link made just before.
	// The PATH a shell has: the root's bin first, the decoys, then the system;
	// sh for the script where the script needs it.
	path := strings.Join(append(ahead, system, "/bin", "/usr/bin"), string(os.PathListSeparator))
	env := append(os.Environ(), asBinary+"=1", hermetic.RootEnv+"="+string(root), "PATH="+path)

	// Outside any project, as the scripts that met this ran: a project's policy
	// would otherwise join the output with the gate's warning.
	return linkedRig{root: root, env: env, outside: t.TempDir(), fake: fake}
}

// link places name in the root's bin as a link to the binary, the way the
// mirror does: a symlink by name, a hard link on Windows.
func link(t *testing.T, root hermetic.Root, name string) {
	t.Helper()

	target := filepath.Join(root.Bin(), exe(name))

	if runtime.GOOS == "windows" {
		if err := os.Link(root.Binary(runtime.GOOS), target); err != nil {
			t.Fatal(err)
		}

		return
	}

	if err := os.Symlink(hermetic.Name, target); err != nil {
		t.Fatal(err)
	}
}

// exe is the name as the root's bin spells it on the platform.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}

	return name
}
