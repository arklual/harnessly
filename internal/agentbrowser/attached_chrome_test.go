package agentbrowser

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

func TestAttachedChrome(t *testing.T) {
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	dir := t.TempDir()
	args := []string{"--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + filepath.Join(dir,
		"chrome"), "--no-first-run", "--no-default-browser-check", "--disable-dev-shm-usage"}
	args = append(args, strings.Fields(os.Getenv("RUNTIME_BROWSER_CHROME_ARGS"))...)
	args = append(args, "about:blank")
	cmd := exec.Command(chrome, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// This fixture owns its external Chrome too; keep its SingletonSocket below the Unix limit.
	chromeTemp, err := os.MkdirTemp("/tmp", "agent-browser-test-")
	must(t, err)
	defer os.RemoveAll(chromeTemp)
	cmd.Env = append(os.Environ(), "TMPDIR="+chromeTemp)
	must(t, cmd.Start())
	defer func() {
		// Children may still write the profile after an abrupt parent-only kill.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}()
	var port string
	eventually(t, 10*time.Second, func() bool {
		b, e := os.ReadFile(filepath.Join(dir, "chrome", "DevToolsActivePort"))
		if e != nil {
			return false
		}
		port, _, _ = strings.Cut(string(b), "\n")
		return port != ""
	})
	m := New(Config{DataDir: filepath.Join(dir, "registry"), CDP: "http://127.0.0.1:" + port})
	defer m.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	i, err := m.Instance(ctx, DefaultProfile)
	must(t, err)
	if i.Profile().Kind != "attached" {
		t.Fatal("not an attached profile")
	}
	if tabs := i.Tabs(""); len(tabs) != 1 || tabs[0].Conversation != "" {
		t.Fatalf("external tab must remain unowned: %+v", tabs)
	}
	tab, err := i.NewTab(ctx, "chat", "person", "about:blank")
	must(t, err)
	evaluate(t, i, tab.ID, "window.persist=7")
	m.Close()
	c, err := cdp.Dial(ctx, "http://127.0.0.1:"+port, nil)
	must(t, err)
	defer c.Close()
	go func() { _ = c.Run(ctx) }()
	_, err = c.Call(ctx, "Browser.getVersion", nil, "")
	must(t, err)
	if _, err = os.Stat(filepath.Join(dir, "registry", "agent-browser",
		"profiles", DefaultProfile, "host.pid")); !os.IsNotExist(err) {
		t.Fatal("attached profile launched a host")
	}
}

func TestChromeLongDataPath(t *testing.T) {
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	data := filepath.Join(t.TempDir(), strings.Repeat("long-directory-", 10))
	exe, err := os.Executable()
	must(t, err)
	profile := filepath.Join(data, "agent-browser", "profiles", DefaultProfile)
	t.Cleanup(func() { cleanupHost(t, profile) })
	m := New(Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: "headless"})
	defer m.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	i, err := m.Instance(ctx, DefaultProfile)
	must(t, err)
	_, err = i.NewTab(ctx, "chat", "agent", "about:blank")
	must(t, err)
	socket := host.SocketPath(filepath.Join(data, "agent-browser", "run"), DefaultProfile)
	for path, mode := range map[string]os.FileMode{socket: 0600, filepath.Dir(socket): 0700, profile: 0700} {
		st, e := os.Stat(path)
		must(t, e)
		if st.Mode().Perm() != mode {
			t.Fatalf("%s: mode %o, want %o", path, st.Mode().Perm(), mode)
		}
	}
	err = host.Run(ctx, host.Config{ProfileDir: profile, Socket: socket, Chrome: chrome, Mode: "headless"})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second host was not refused: %v", err)
	}

}

func TestChromeStartupFailureStatus(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome-fixture")
	must(t, os.WriteFile(fake, []byte(`#!/bin/sh
echo 'FATAL Socket path too long: /private/profile/SingletonSocket' >&2
exit 1
`),
		0700))
	exe, err := os.Executable()
	must(t, err)
	m := New(Config{DataDir: dir, Executable: exe, Chrome: fake, Mode: "headless"})
	defer m.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, err = m.Instance(ctx, DefaultProfile)
	if err == nil || !strings.Contains(err.Error(), "Socket path too long") || strings.Contains(err.Error(), "/private") {
		t.Fatalf("startup diagnostic: %v", err)
	}
	t.Logf("status: %v", err)
}
