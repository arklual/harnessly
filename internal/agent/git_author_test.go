package agent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestManagedGitAuthorLifecycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	home, linkDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	try := func(n, e string) {
		t.Helper()
		if err := syncGitAuthor(home, xdg, linkDir, hubproto.GitAuthor{Name: n, Email: e}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(key string) string {
		t.Helper()
		cmd := exec.Command("git", "config", "--get", key)
		cmd.Dir = home
		out, _ := cmd.Output()
		return string(out)
	}
	try("Alice", "first@example.org")
	if read("user.email") != "first@example.org\n" || read("user.name") != "Alice\n" {
		t.Fatal("author unavailable to Git")
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n name = My pen name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	try("Bob", "second@example.org")
	if read("user.email") != "second@example.org\n" || read("user.name") != "My pen name\n" {
		t.Fatal("handoff lost local override or retained previous owner")
	}
	try("", "")
	if read("user.email") != "" || read("user.name") != "My pen name\n" {
		t.Fatal("owner removal left stale defaults or changed local config")
	}
}
func TestManagedGitAuthorLeavesCustomXDGConfig(t *testing.T) {
	home, linkDir := t.TempDir(), t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(home, ".config")
	dir := filepath.Join(xdg, "git")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config")
	want := "[user]\n name = My settings\n"
	if err := os.WriteFile(cfg, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syncGitAuthor(home, xdg, linkDir, hubproto.GitAuthor{Name: "Alice", Email: "a@example.org"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfg)
	if string(got) != want {
		t.Fatal("changed personal XDG config")
	}
	if _, err := os.Stat(filepath.Join(linkDir, managedGitAuthor)); !os.IsNotExist(err) {
		t.Fatal("left an inactive default")
	}
}

func TestGitAuthorUsesAuthenticatedLinkSetupPolicy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	req := map[string]any{"author": hubproto.GitAuthor{Name: "Verified Owner", Email: "owner@example.org"}}
	deny := startHubAgent(t, rigOptions{policy: "setup = false\n", beforeStart: beat}).connect()
	if _, e := deny.call(hubproto.OpGitAuthor, req); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("unapproved setup: %s", e)
	}
	r := startHubAgent(t, rigOptions{beforeStart: beat})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall(hubproto.OpHello, nil))
	if !slices.Contains(hello.Features, hubproto.OpGitAuthor) {
		t.Fatal("missing owner setup feature")
	}
	c.mustCall(hubproto.OpGitAuthor, req)
	if raw, err := os.ReadFile(filepath.Join(r.rig.Home, ".harnessly", managedGitAuthor)); err != nil || !bytes.Contains(raw, []byte("owner@example.org")) {
		t.Fatalf("missing managed author: %v", err)
	}
	c.mustCall(hubproto.OpGitAuthor, map[string]any{"author": hubproto.GitAuthor{}})
	if _, err := os.Stat(filepath.Join(r.rig.Home, ".harnessly", managedGitAuthor)); !os.IsNotExist(err) {
		t.Fatal("owner defaults survived removal")
	}
}
