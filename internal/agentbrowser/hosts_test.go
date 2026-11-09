package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

// fakeHost holds a profile's host lock and answers on its socket like a browser host of the given build ("" is a
// host too old to know Host.hello). Browser.close releases the lock, as Chrome's exit does.
func fakeHost(t *testing.T, m *Manager, id, build string) (running func() bool) {
	t.Helper()
	dir := m.profileDir(id)
	must(t, os.MkdirAll(dir, 0700))
	lock, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	must(t, err)
	must(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	socket := m.profileSocket(id)
	must(t, host.PrivateDir(filepath.Dir(socket)))
	ln, err := net.Listen("unix", socket)
	must(t, err)
	t.Cleanup(func() { _ = ln.Close(); _ = lock.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				stream := cdp.NewStream(conn)
				defer stream.Close()
				for {
					raw, err := stream.Read(context.Background())
					if err != nil {
						return
					}
					var msg struct {
						ID     int    `json:"id"`
						Method string `json:"method"`
					}
					_ = json.Unmarshal(raw, &msg)
					answer := map[string]any{"id": msg.ID, "result": map[string]any{}}
					switch {
					case msg.Method == host.HelloMethod && build != "":
						answer["result"] = map[string]string{"build": build}
					case msg.Method == host.HelloMethod:
						delete(answer, "result")
						answer["error"] = map[string]any{"code": -32601, "message": "'Host.hello' wasn't found"}
					case msg.Method == "Browser.close":
						_ = lock.Close()
					}
					out, _ := json.Marshal(answer)
					_ = stream.Write(context.Background(), out)
				}
			}()
		}
	}()
	return func() bool { return host.Running(dir) }
}

func TestSweepReplacesHostsOfAnotherBuild(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	current := fakeHost(t, m, "current", host.BuildID())
	old := fakeHost(t, m, "old", "0.0.1")
	ancient := fakeHost(t, m, "ancient", "")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	m.Sweep(ctx)
	if !current() || old() || ancient() {
		t.Fatalf("after sweep: current %v, old %v, ancient %v", current(), old(), ancient())
	}
}

func TestSweepWithBrowserOffClosesEveryHost(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), Off: true})
	defer m.Close()
	running := fakeHost(t, m, "current", host.BuildID())
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	m.Sweep(ctx)
	if running() {
		t.Fatal("RUNTIME_BROWSER=off left a host running")
	}
}

func TestMakeRoomClosesTheIdlestHostAndRefusesWhenAllAreBusy(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), MaxHosts: 2})
	defer m.Close()
	orphan := fakeHost(t, m, "orphan", host.BuildID())
	busy := fakeHost(t, m, "busy", host.BuildID())
	i := newInstance(m, "busy")
	defer i.cancel()
	i.operations = 1
	m.mu.Lock()
	m.instances["busy"] = i
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	must(t, m.makeRoom(ctx, "new"))
	if orphan() || !busy() {
		t.Fatalf("orphan running %v, busy running %v", orphan(), busy())
	}
	m.cfg.MaxHosts = 1
	if err := m.makeRoom(ctx, "new"); err == nil {
		t.Fatal("a busy browser was closed for a new one")
	}
}

func TestNewTabPastTheLimitIsRefused(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), MaxTabs: 2})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	i.pages["a"] = newPage(Tab{ID: "a"})
	// Below the limit the tab goes on to the browser (none here).
	if _, err := i.NewTab(t.Context(), "chat", "agent", "about:blank"); !errors.Is(err, cdp.ErrClosed) {
		t.Fatalf("below the limit: %v", err)
	}
	i.pages["b"] = newPage(Tab{ID: "b"})
	_, err := i.NewTab(t.Context(), "chat", "agent", "about:blank")
	if err == nil || !strings.Contains(err.Error(), "RUNTIME_BROWSER_MAX_TABS") {
		t.Fatalf("past the limit: %v", err)
	}
}
