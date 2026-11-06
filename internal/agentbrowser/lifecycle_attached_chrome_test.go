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

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func lifecycleExternalChrome(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	data := t.TempDir()
	args := []string{"--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + data,
		"--no-first-run", "--no-default-browser-check", "--disable-dev-shm-usage", "--password-store=basic", "about:blank"}
	args = append(args, strings.Fields(os.Getenv("RUNTIME_BROWSER_CHROME_ARGS"))...)
	cmd := exec.Command(chrome, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	chromeTemp, err := os.MkdirTemp("/tmp", "agent-browser-test-")
	must(t, err)
	cmd.Env = append(os.Environ(), "TMPDIR="+chromeTemp)
	must(t, cmd.Start())
	t.Cleanup(func() {
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
		_ = os.RemoveAll(chromeTemp)
	})
	var port string
	eventually(t, 10*time.Second, func() bool {
		raw, err := os.ReadFile(filepath.Join(data, "DevToolsActivePort"))
		if err != nil {
			return false
		}
		port, _, _ = strings.Cut(string(raw), "\n")
		return port != ""
	})
	return "http://127.0.0.1:" + port
}

func TestChromeAttachedPerProfileLifecycle(t *testing.T) {
	one, two := lifecycleExternalChrome(t), lifecycleExternalChrome(t)
	cfg := Config{DataDir: t.TempDir(), CDP: "http://127.0.0.1:1", LookupProfile: func(id string) (Profile, bool) {
		switch id {
		case "one":
			return Profile{ID: id, Kind: "attached", CDP: one}, true
		case "two":
			return Profile{ID: id, Kind: "attached", CDP: two}, true
		}
		return Profile{}, false
	}}
	m := New(cfg)
	defer m.Close()
	i, err := m.Instance(t.Context(), "one")
	must(t, err)
	j, err := m.Instance(t.Context(), "two")
	must(t, err)
	server := cookieFixture(t)
	a, err := i.NewTab(t.Context(), "chat", "agent", server.URL)
	must(t, err)
	b, err := j.NewTab(t.Context(), "other", "agent", server.URL)
	must(t, err)
	eventually(t, 5*time.Second, func() bool { return string(evaluate(t, i, a.ID, "document.title")) == `"cookie fixture"` })
	eventually(t, 5*time.Second, func() bool { return string(evaluate(t, j, b.ID, "document.title")) == `"cookie fixture"` })
	evaluate(t, i, a.ID, `document.cookie='scope=one; Max-Age=3600';window.survives=7;true`)
	if got := string(evaluate(t, j, b.ID, "document.cookie")); got != `""` {
		t.Fatal("attached profiles share cookies:", got)
	}
	must(t, m.StopProfile(t.Context(), "one"))
	if m.ProfileRunning("one") {
		t.Fatal("attached instance did not disconnect")
	}
	if !m.ProfileRunning("two") {
		t.Fatal("stop disconnected other profile")
	}
	if err = m.ClearProfile(t.Context(), "one"); err == nil {
		t.Fatal("cleared external browser")
	}
	i, err = m.Instance(t.Context(), "one")
	must(t, err)
	if got := string(evaluate(t, i, a.ID, "window.survives")); got != "7" {
		t.Fatal("stop killed external Chrome:", got)
	}
	m.Close()
	next := New(cfg)
	defer next.Close()
	must(t, next.CloseConversation(t.Context(), "chat"))
	c, err := cdp.Dial(t.Context(), one, nil)
	must(t, err)
	defer c.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	raw, err := c.Call(ctx, "Target.getTargets", nil, "")
	must(t, err)
	if strings.Contains(string(raw), a.ID) {
		t.Fatal("dormant attached registry cleanup retained owned target")
	}
	if !next.ConversationHasTabs("other") || next.ConversationHasTabs("chat") {
		t.Fatal("dormant attached cleanup removed wrong metadata")
	}
	if m.preparation != nil || next.preparation != nil {
		t.Fatal("attached profiles provisioned Chrome")
	}
	t.Log("per-profile external endpoints isolate cookies; stop only disconnects; clear refuses; dormant cleanup closes remote owned tab without launching Chrome")
}
