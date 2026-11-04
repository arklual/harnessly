package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadyDoesNotWaitForDownload(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("RUNTIME_BROWSER_CHROME", "")
	t.Setenv("RUNTIME_BROWSER_CHROME_URL", server.URL)
	t.Setenv("RUNTIME_BROWSER_DOWNLOAD", "")
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	start := time.Now()
	s := m.Ready(t.Context())
	if s.State != "preparing" || time.Since(start) > time.Second {
		t.Fatalf("blocked: %+v", s)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := m.InstanceFor(ctx, "chat")
	if _, ok := err.(*PreparingError); !ok {
		t.Fatalf("agent reply: %T %v", err, err)
	}
}

func TestReadyDisabledAndAttached(t *testing.T) {
	for _, tc := range []struct {
		cfg   Config
		state string
	}{
		{Config{Off: true}, "down"}, {Config{CDP: "http://127.0.0.1:1"}, "up"},
	} {
		m := New(tc.cfg)
		if s := m.Ready(t.Context()); s.State != tc.state {
			t.Fatalf("%+v", s)
		}
		m.Close()
	}
}

func TestReadyRecoversWithoutRestart(t *testing.T) {
	chrome, err := os.Executable()
	must(t, err)
	m := New(Config{DataDir: t.TempDir(), Chrome: chrome})
	defer m.Close()
	var fixed atomic.Bool
	var probes atomic.Int32
	m.probe = func(context.Context, string, string) (provision.Inspection, error) {
		probes.Add(1)
		if !fixed.Load() {
			return provision.Inspection{}, &provision.Diagnostic{Message: "repair needed", Command: "repair-command"}
		}
		return provision.Inspection{Version: "Chrome fixture", Sandbox: "enabled"}, nil
	}
	eventually(t, time.Second, func() bool { return m.Ready(t.Context()).State == "error" })
	if s := m.Ready(t.Context()); s.Command != "repair-command" {
		t.Fatalf("lost command: %+v", s)
	}
	_, err = m.Instance(t.Context(), "before")
	var diagnostic *provision.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Command != "repair-command" {
		t.Fatalf("lost typed diagnostic: %v", err)
	}
	fixed.Store(true)
	if s := m.Ready(t.Context()); s.State != "error" || probes.Load() != 1 {
		t.Fatal("retried before cooldown", s)
	}
	m.mu.Lock()
	m.preparation.finished = time.Now().Add(-15 * time.Second)
	m.mu.Unlock()
	if s := m.Ready(t.Context()); s.State != "preparing" {
		t.Fatalf("no automatic retry: %+v", s)
	}
	eventually(t, time.Second, func() bool { return m.Ready(t.Context()).State == "up" })
	if probes.Load() != 2 || m.Ready(t.Context()).Command != "" {
		t.Fatal("stale preparation")
	}
}

func TestPanelRetriesPreparationOnSameSocket(t *testing.T) {
	chrome, err := os.Executable()
	must(t, err)
	m := New(Config{DataDir: t.TempDir(), Chrome: chrome})
	defer m.Close()
	var probes atomic.Int32
	m.probe = func(context.Context, string, string) (provision.Inspection, error) {
		n := probes.Add(1)
		return provision.Inspection{}, &provision.Diagnostic{Message: fmt.Sprintf("probe %d", n), Command: "repair-command"}
	}
	socket := openContractSocket(t, m, "retry")
	waitPreparation(t, socket, func(raw []byte) bool {
		return strings.Contains(string(raw), `"error":"probe 1"`) &&
			strings.Contains(string(raw), `"command":"repair-command"`)
	})
	socket.send(t, Input{T: "browser.retry"})
	waitPreparation(t, socket, func(raw []byte) bool { return strings.Contains(string(raw), `"error":"probe 2"`) })
	if probes.Load() != 2 {
		t.Fatal("duplicate preparation", probes.Load())
	}
}

func TestChromeReadyRetryNewInstance(t *testing.T) {
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	data := t.TempDir()
	m := New(Config{DataDir: data, Chrome: chrome, Mode: "headless"})
	t.Cleanup(func() {
		m.Close()
		for _, id := range []string{"first", "second"} {
			cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", id))
		}
	})
	var fixed atomic.Bool
	m.probe = func(ctx context.Context, chrome, data string) (provision.Inspection, error) {
		if !fixed.Load() {
			return provision.Inspection{}, &provision.Diagnostic{Message: "sandbox unavailable", Command: "repair"}
		}
		return provision.Probe(ctx, chrome, data)
	}
	eventually(t, time.Second, func() bool { return m.Ready(t.Context()).State == "error" })
	fixed.Store(true)
	m.mu.Lock()
	m.preparation.finished = time.Now().Add(-15 * time.Second)
	m.mu.Unlock()
	eventually(t, 35*time.Second, func() bool { return m.Ready(t.Context()).State == "up" })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, id := range []string{"first", "second"} {
		instance, e := m.Instance(ctx, id)
		must(t, e)
		tab, e := instance.NewTab(ctx, "retry", "agent", "about:blank")
		must(t, e)
		if got := string(evaluate(t, instance, tab.ID, "1+1")); got != "2" {
			t.Fatal(got)
		}
	}
}

func TestChromeLiveHostProtectsOldVersion(t *testing.T) {
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	data := t.TempDir()
	old := filepath.Join(data, "agent-browser", "chrome", "123.0.0.1", "chrome-linux64", "chrome")
	current := filepath.Join(data, "agent-browser", "chrome", provision.Version, "chrome-linux64", "chrome")
	for _, path := range []string{old, current} {
		must(t, os.MkdirAll(filepath.Dir(path), 0700))
		// A wrapper uses the system browser's own resources; the host still leases this version's directory.
		must(t, os.WriteFile(path, []byte("#!/bin/sh\nexec "+strconv.Quote(chrome)+" \"$@\"\n"), 0700))
	}
	m := New(Config{DataDir: data, Chrome: old, Mode: "headless"})
	dir := filepath.Join(data, "agent-browser", "profiles", DefaultProfile)
	t.Cleanup(func() { m.Close(); cleanupHost(t, dir) })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, err = m.Instance(ctx, DefaultProfile)
	must(t, err)
	cfg := provision.FromEnv(data)
	cfg.Chrome = current
	_, err = provision.Ensure(ctx, cfg, nil)
	must(t, err)
	if _, err = os.Stat(old); err != nil {
		t.Fatal("removed live host resources", err)
	}
	m.Close()
	_, err = provision.Ensure(ctx, cfg, nil)
	must(t, err)
	if _, err = os.Stat(old); err != nil {
		t.Fatal("runtime disconnect removed live host resources", err)
	}
	cleanupHost(t, dir)
	eventually(t, 5*time.Second, func() bool {
		_, e := provision.Ensure(ctx, cfg, nil)
		if e != nil {
			return false
		}
		_, e = os.Stat(old)
		return os.IsNotExist(e)
	})
}

func waitPreparation(t *testing.T, socket *contractSocket, match func([]byte) bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-socket.messages:
			if match(raw) {
				return
			}
		case err := <-socket.errors:
			t.Fatal(err)
		case <-timer.C:
			t.Fatal("preparation status did not arrive")
		}
	}
}
