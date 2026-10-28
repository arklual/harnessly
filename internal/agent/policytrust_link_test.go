//go:build linux || darwin

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// A link in a folder the account writes is a name it can point anywhere, so a policy reached through one is judged by
// the folder of the link, not by what it points at.
func TestFileWritableByAgentJudgesTheLinkOnTheWay(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes everything")
	}
	const unwritable = "/etc/hostname"
	if _, err := os.Stat(unwritable); err != nil || changeable(unwritable) {
		t.Skip("no root-owned file to point at")
	}
	if fileWritableByAgent(unwritable) {
		t.Fatal("a root-owned file in root's folders is not writable")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "policy.toml")
	if err := os.Symlink(unwritable, link); err != nil {
		t.Fatal(err)
	}
	if !fileWritableByAgent(link) {
		t.Error("a link in a folder the account writes, to a file it cannot, counts as trusted")
	}
	// A link to the folder that holds the file: the file's name is then inside the link's folder as well.
	dirLink := filepath.Join(dir, "etc")
	if err := os.Symlink("/etc", dirLink); err != nil {
		t.Fatal(err)
	}
	if !fileWritableByAgent(filepath.Join(dirLink, "hostname")) {
		t.Error("a folder link in a writable folder counts as trusted")
	}
}

// What a start or SIGHUP judged writable stays so for the reloads in between.
func TestSoftPolicyStaysSoftBetweenSighups(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	writeFile(t, file, "terminal = false\n")
	s := newSoftStore(file, dir)
	s.Reload(true)
	if !s.Soft() {
		t.Fatal("not soft")
	}
	s.writable = rootOwned // something under the file changed so that it now looks trusted
	writeFile(t, file, "terminal = true\n")
	s.Reload(false)
	if s.Current().Terminal || !s.Soft() || !s.Held() {
		t.Fatalf("a non-forced reload widened the policy: %+v soft=%v held=%v", s.Current(), s.Soft(), s.Held())
	}
	s.Reload(true)
	if !s.Current().Terminal || s.Soft() {
		t.Errorf("a SIGHUP applies and judges again: %+v", s.Current())
	}
}
