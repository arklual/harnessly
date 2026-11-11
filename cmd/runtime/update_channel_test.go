package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/selfupdate"
	"github.com/ly-solutions/harnessly/internal/updatecheck"
)

func releaseBuild(t *testing.T) {
	t.Helper()
	old := agent.BuildVersion
	t.Cleanup(func() { agent.BuildVersion = old })
	agent.BuildVersion = "0.16.100"
	for _, k := range []string{"RUNTIME_HUB_URL", "RUNTIME_UPDATE_CHECK", "RUNTIME_UPDATE_CHECK_URL", "RUNTIME_UPDATE_STABLE_URL",
		"RUNTIME_UPDATE_CHANNEL", "RUNTIME_UPDATE_MODE", "RUNTIME_FLEET_ROOT"} {
		t.Setenv(k, "")
	}
}

func TestUpdateChannelOfAStandaloneRuntime(t *testing.T) {
	releaseBuild(t)
	for raw, want := range map[string]string{"": "stable", "stable": "stable", "latest": "latest", "nightly": "stable"} {
		t.Setenv("RUNTIME_UPDATE_CHANNEL", raw)
		c, on := newUpdateChecker(true)
		if !on || c.Channel != want {
			t.Errorf("RUNTIME_UPDATE_CHANNEL=%q: channel %q, checking %v", raw, c.Channel, on)
		}
		if got := updateState(c, on, nil); got.Channel != want || got.Source != "releases" {
			t.Errorf("RUNTIME_UPDATE_CHANNEL=%q: the answer says %q from %q", raw, got.Channel, got.Source)
		}
	}
	t.Setenv("RUNTIME_UPDATE_CHANNEL", "")
	t.Setenv("RUNTIME_UPDATE_STABLE_URL", " https://mirror.example.org/stable.json ")
	if c, _ := newUpdateChecker(true); c.StableURL != "https://mirror.example.org/stable.json" {
		t.Errorf("stable pointer address %q", c.StableURL)
	}
}

// hubRecorder is a hub that serves the files manifest as it always did and remembers what it was asked for.
type hubRecorder struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newHubRecorder(t *testing.T) *hubRecorder {
	t.Helper()
	h := &hubRecorder{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.paths = append(h.paths, r.URL.Path)
		h.mu.Unlock()
		if r.URL.Path != "/dl/manifest.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"version":"0.16.200","files":{"runtime-linux-amd64":{"sha256":"` + strings.Repeat("ab", 32) + `","size":3}}}`))
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hubRecorder) asked() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.paths, " ")
}

// A machine a hub carries follows the programs its hub serves, whatever the channel says: it asks for no release and
// no stable pointer, and takes the hub's newer programs as before.
func TestUpdateChannelLeavesAHubMachineAlone(t *testing.T) {
	for _, channel := range []string{"", "stable", "latest"} {
		releaseBuild(t)
		hub := newHubRecorder(t)
		t.Setenv("RUNTIME_HUB_URL", hub.URL)
		t.Setenv("RUNTIME_UPDATE_CHANNEL", channel)
		c, on := newUpdateChecker(true)
		if on {
			t.Fatalf("channel %q: a runtime a hub carries looks for releases", channel)
		}
		exe := filepath.Join(t.TempDir(), "harnessly-runtime")
		if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		self := &selfupdate.Updater{HubURL: hub.URL, Files: []selfupdate.File{{Name: "runtime", Path: exe}}, GOOS: "linux", GOARCH: "amd64"}
		st, err := self.Check(context.Background())
		if err != nil || !st.Checked || st.UpToDate || st.Version != "0.16.200" {
			t.Fatalf("channel %q: the hub's newer programs are not offered: %+v, %v", channel, st, err)
		}
		if hub.asked() != "/dl/manifest.json" {
			t.Fatalf("channel %q: the hub was asked for %q", channel, hub.asked())
		}
		if got := updateState(c, on, self); got.Channel != "" || got.Note != "" || got.Source != "hub" || got.UpdateAvailable {
			t.Fatalf("channel %q: the answer %+v", channel, got)
		}
	}
}

// A machine in fleet mode takes what its rollout brings: no channel, no request to the hub or to the releases.
func TestUpdateChannelLeavesAFleetMachineAlone(t *testing.T) {
	for _, channel := range []string{"", "stable", "latest"} {
		releaseBuild(t)
		hub := newHubRecorder(t)
		t.Setenv("RUNTIME_HUB_URL", hub.URL)
		t.Setenv("RUNTIME_UPDATE_MODE", "fleet")
		t.Setenv("RUNTIME_FLEET_ROOT", t.TempDir())
		t.Setenv("RUNTIME_UPDATE_CHANNEL", channel)
		c, on := newUpdateChecker(true)
		self := newSelfUpdater(t.TempDir())
		if on || self == nil || self.Fleet == nil {
			t.Fatalf("channel %q: checking %v, updater %+v", channel, on, self)
		}
		st, err := self.Check(context.Background())
		if err != nil || st.Mode != "fleet" || !st.UpToDate {
			t.Fatalf("channel %q: %+v, %v", channel, st, err)
		}
		self.Run(context.Background()) // returns at once in fleet mode
		if hub.asked() != "" {
			t.Fatalf("channel %q: a fleet machine asked its hub for %q", channel, hub.asked())
		}
		if got := updateState(c, on, self); got.Channel != "" || got.Source != "fleet" || got.UpdateAvailable {
			t.Fatalf("channel %q: the answer %+v", channel, got)
		}
	}
}

func TestUpdateRouteForTheCommandLine(t *testing.T) {
	data := t.TempDir()
	cli := &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}}
	c := &updatecheck.Checker{Current: "0.16.100", Channel: updatecheck.ChannelStable}
	routes := updateRoutes(deps{DataDir: data, CLI: cli, SessionOK: func(*http.Request) bool { return false }}, c, true, nil)
	route := routes[len(routes)-1]
	ask := func(method, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/cli/update", nil)
		if token != "" {
			r.Header.Set("x-cli-token", token)
		}
		if !route.Match(r) {
			t.Fatal("route does not match its path")
		}
		w := httptest.NewRecorder()
		route.Handler.ServeHTTP(w, r)
		return w
	}
	if w := ask(http.MethodGet, ""); w.Code != http.StatusForbidden {
		t.Fatalf("no token: %d", w.Code)
	}
	const token = "tok"
	if err := os.WriteFile(filepath.Join(data, "cli-token"), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := ask(http.MethodPost, token); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
	w := ask(http.MethodGet, token)
	var got updateAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if got.Channel != "stable" || got.Source != "releases" || got.Current != "0.16.100" {
		t.Fatalf("%+v", got)
	}
}
