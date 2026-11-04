package agentbrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
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
