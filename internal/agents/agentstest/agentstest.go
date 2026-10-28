// Package agentstest is a rig for tests of what the agent does through the login shell: a home folder, a folder of fake
// agent CLIs that comes first on the PATH, and a stand-in login shell.
package agentstest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/preflight"
)

// Rig is one test's machine.
type Rig struct {
	T    *testing.T
	Home string
	// Bin holds the fake CLIs; it is on the PATH of the login shell, ahead of Tools.
	Bin string
	// Tools holds links to the few system programs the scripts use. The PATH has nothing else, so a test is not
	// answered by an agent CLI the developer's machine happens to have.
	Tools string
	// EnvDir is where the agent's env files live.
	EnvDir  string
	Checker *preflight.Checker
}

// systemTools are linked into Tools: what the probe and install scripts call.
var systemTools = []string{"head", "tail", "env", "cat", "sleep", "grep", "sed", "tr", "awk", "mkdir", "rm", "cp", "mv", "chmod",
	"mktemp", "date", "id", "uname", "wc", "dirname", "basename", "touch", "ls", "ln", "printenv", "true", "false", "test", "kill"}

// New makes a rig. The login shell is a small script that does what a real one does for the agent: runs the command it
// is given (-c) with PATH set to the rig's folders and the account's ~/.profile read. Anything the test adds to the environment of the process
// (t.Setenv) is not seen, as with a real one.
func New(t *testing.T) *Rig {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(t.TempDir(), "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Rig{T: t, Home: home, Bin: bin, Tools: tools, EnvDir: filepath.Join(home, ".harnessly", "agent-env")}
	for _, name := range systemTools {
		r.Link(name)
	}
	shell := filepath.Join(t.TempDir(), "loginsh")
	body := "#!/bin/sh\nPATH=\"" + bin + ":" + tools + "\"; export PATH\n[ -f \"$HOME/.profile\" ] && . \"$HOME/.profile\"\nshift\neval \"$1\"\n"
	if err := os.WriteFile(shell, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Checker = &preflight.Checker{Home: home, Env: []string{"PATH=" + tools, "HOME=" + home}, Shell: shell}
	return r
}

// Link makes a system program available to the scripts; one this machine lacks is skipped.
func (r *Rig) Link(name string) {
	r.T.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		return
	}
	if err := os.Symlink(path, filepath.Join(r.Tools, name)); err != nil && !os.IsExist(err) {
		r.T.Fatal(err)
	}
}

// Script writes an executable sh script into the rig's CLI folder.
func (r *Rig) Script(name, body string) {
	r.T.Helper()
	if err := os.WriteFile(filepath.Join(r.Bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		r.T.Fatal(err)
	}
}

// Remove deletes a fake CLI.
func (r *Rig) Remove(name string) {
	r.T.Helper()
	if err := os.Remove(filepath.Join(r.Bin, name)); err != nil {
		r.T.Fatal(err)
	}
}

// WriteEnv writes a file of the env folder.
func (r *Rig) WriteEnv(name, body string) {
	r.T.Helper()
	if err := os.MkdirAll(r.EnvDir, 0o700); err != nil {
		r.T.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.EnvDir, name), []byte(body), 0o600); err != nil {
		r.T.Fatal(err)
	}
}

// WriteHome writes a file under the home folder, with the given mode (0o755 for a script).
func (r *Rig) WriteHome(rel, body string, mode os.FileMode) {
	r.T.Helper()
	path := filepath.Join(r.Home, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.T.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		r.T.Fatal(err)
	}
}
