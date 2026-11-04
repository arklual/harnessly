package agentbrowser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
)

// TestProvisionedChrome is opt-in because it downloads a real pinned archive from Google's public host.
// The test uses only a local page, private pipe and fresh profile, never an existing debugging port.
func TestProvisionedChrome(t *testing.T) {
	if os.Getenv("AGENT_BROWSER_DOWNLOAD_TEST") != "1" {
		t.Skip("set AGENT_BROWSER_DOWNLOAD_TEST=1 for a real download")
	}
	data := t.TempDir()
	// Keep only Xvfb and ldd on PATH, so system Chrome cannot accidentally satisfy this test.
	bin := t.TempDir()
	for _, name := range []string{"Xvfb", "ldd"} {
		if name == "Xvfb" && os.Getenv("AGENT_BROWSER_TEST_NO_XVFB") == "1" {
			continue
		}
		source := "/usr/bin/" + name
		if _, e := os.Stat(source); e == nil {
			must(t, os.Symlink(source, filepath.Join(bin, name)))
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("RUNTIME_BROWSER_CHROME", "")
	t.Setenv("RUNTIME_BROWSER_CHROME_URL", "")
	t.Setenv("RUNTIME_BROWSER_DOWNLOAD", "")
	cfg := Config{DataDir: data, Executable: os.Getenv("AGENT_BROWSER_TEST_RUNTIME"), Mode: os.Getenv("RUNTIME_BROWSER_MODE")}
	m := New(cfg)
	defer m.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	start := time.Now()
	s := m.Ready(ctx)
	if s.State != "preparing" || time.Since(start) > time.Second {
		t.Fatalf("first response: %+v", s)
	}
	last := -1
	for s.State == "preparing" {
		if s.Progress != last {
			t.Logf("state=%s progress=%d hint=%s", s.State, s.Progress, s.Hint)
			last = s.Progress
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
		s = m.Ready(ctx)
	}
	t.Logf("ready: %+v", s)
	if os.Getenv("AGENT_BROWSER_EXPECT_SANDBOX_ERROR") == "1" {
		if s.State != "error" || !strings.Contains(s.Error, "Chrome sandbox is unavailable") {
			t.Fatalf("expected sandbox error: %+v", s)
		}
		return
	}
	if s.State != "up" {
		t.Fatalf("preparation failed: %s", s.Error)
	}
	dir := filepath.Join(data, "agent-browser", "profiles", DefaultProfile)
	t.Cleanup(func() { cleanupHost(t, dir) })
	i, e := m.Instance(ctx, DefaultProfile)
	must(t, e)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><title>Fresh browser</title><body style='background:#bfe'>Fresh browser fixture</body></html>")
	}))
	defer server.Close()
	tab, e := i.NewTab(ctx, "fresh-install", "agent", server.URL)
	must(t, e)
	viewer, e := i.Watch(ctx, tab.ID)
	must(t, e)
	defer viewer.Close()
	select {
	case frame := <-viewer.Frames():
		t.Logf("opened local page; frame=%dx%d encoded_bytes=%d", frame.Width, frame.Height, len(frame.Encode(0)))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var size int64
	must(t, filepath.Walk(filepath.Join(data, "agent-browser", "chrome", provision.Version), func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			size += info.Size()
		}
		return err
	}))
	t.Logf("Chrome %s installed_bytes=%d mode=%s sandbox=%s", provision.Version, size, s.Mode, s.Sandbox)
}
