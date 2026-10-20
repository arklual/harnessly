package machines

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/push"
)

type apiGolden struct {
	Token string `json:"token"`
	Host  struct {
		CPUs     int     `json:"cpus"`
		TotalMem float64 `json:"totalmem"`
		FreeMem  float64 `json:"freemem"`
		Load     float64 `json:"load"`
		Uptime   float64 `json:"uptime"`
		Hostname string  `json:"hostname"`
		User     string  `json:"user"`
	} `json:"host"`
	Fixtures     map[string]string `json:"fixtures"`
	Events       [][2]float64      `json:"events"`
	ActivityAsOf float64           `json:"activityAsOf"`
	Release      struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	} `json:"release"`
	Requests []struct {
		Method        string            `json:"method"`
		URL           string            `json:"url"`
		Headers       map[string]string `json:"headers"`
		Body          *string           `json:"body"`
		Activity      *string           `json:"activity"`
		Status        int               `json:"status"`
		AnswerHeaders map[string]string `json:"answerHeaders"`
		Answer        string            `json:"answer"`
		Conditional   bool              `json:"conditional"`
	} `json:"requests"`
	Files map[string]*string `json:"files"`
}

// TestGoldenAPI sends the requests the previous runtime's dispatcher answered to these routes behind the gate, on the
// same files, and checks status, the headers a caller acts on, the body and the files left behind.
func TestGoldenAPI(t *testing.T) {
	gd := loadGoldenFile(t)
	g := gd.API
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	_ = os.MkdirAll(data, 0o700)
	for name, content := range g.Fixtures {
		_ = os.WriteFile(filepath.Join(data, name), []byte(content), 0o600)
	}
	release := filepath.Join(dir, "release.json")
	_ = os.WriteFile(release, []byte(`{"version":"`+g.Release.Version+`","commit":"`+g.Release.Commit+`"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["machines"]}`), 0o600)

	now := time.UnixMilli(int64(gd.Fixed))
	var activity map[string]*Activity
	claude := BuildActivity(g.Events, g.ActivityAsOf, goldenTZ)
	snapshot := NewSnapshot(SnapshotConfig{
		DataDir: data, NamePrefix: "acme-", Codexbar: true, ReleaseFile: release, User: g.Host.User,
		Activity: func() map[string]*Activity { return activity },
		Host: func() HostStats {
			return HostStats{CPUs: g.Host.CPUs, Load1: g.Host.Load, TotalMem: g.Host.TotalMem, FreeMem: g.Host.FreeMem, Uptime: g.Host.Uptime}
		},
		Hostname: func() (string, error) { return g.Host.Hostname, nil },
		Now:      func() time.Time { return now },
	})
	sessionOK := func(r *http.Request) bool { return strings.Contains(r.Header.Get("Cookie"), "session-token=good") }
	sender := push.New(push.Config{Dir: data, Subject: "mailto:ops@example.org", TagPrefix: "acme:", Now: func() time.Time { return now }})
	routes := Routes(Config{
		PlatformAPI: "/api/platform", DataDir: data, SessionOK: sessionOK,
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) || r.Header.Get("X-Cli-Token") == g.Token {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		},
		Snapshot: snapshot, Load: NewLoadMonitor(LoadDeps{}), Push: sender, CanSend: true, AppName: "Acme", TagPrefix: "acme:",
		Health: Health{App: "harnessly", FallbackVersion: "0.0.0-development", ReleaseFile: release, Alive: func() bool { return true }},
		Now:    func() time.Time { return now },
	})
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f.Add(f.AsRoutes(routes...)...)

	lastEtag := ""
	for i, q := range g.Requests {
		activity = nil
		if q.Activity != nil && *q.Activity == "claude" {
			activity = map[string]*Activity{"claude": claude}
		}
		var body io.Reader
		if q.Body != nil {
			body = strings.NewReader(*q.Body)
		}
		req := httptest.NewRequest(q.Method, q.URL, body)
		for k, v := range q.Headers {
			req.Header.Set(k, v)
		}
		if q.Conditional {
			req.Header.Set("If-None-Match", lastEtag)
		}
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, req)
		res := rec.Result()
		got, _ := io.ReadAll(res.Body)
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		where := q.Method + " " + q.URL
		if res.StatusCode != q.Status || string(got) != q.Answer {
			t.Errorf("%d %s:\n got %d %s\nwant %d %s", i, where, res.StatusCode, got, q.Status, q.Answer)
			continue
		}
		for k, want := range q.AnswerHeaders {
			have := res.Header.Get(k)
			if k == "etag" && q.Status == 304 {
				if have != req.Header.Get("If-None-Match") {
					t.Errorf("%d %s: etag %s, sent %s", i, where, have, req.Header.Get("If-None-Match"))
				}
				continue
			}
			if have != want {
				t.Errorf("%d %s: %s = %q, want %q", i, where, k, have, want)
			}
		}
		for _, k := range []string{"ETag", "Allow", "Cache-Control", "Content-Type"} {
			if _, ok := q.AnswerHeaders[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
				t.Errorf("%d %s: unexpected %s %q", i, where, k, res.Header.Get(k))
			}
		}
	}
	for name, want := range g.Files {
		raw, err := os.ReadFile(filepath.Join(data, name))
		if want == nil {
			if err == nil {
				t.Errorf("%s exists", name)
			}
			continue
		}
		if string(raw) != *want {
			t.Errorf("%s:\n got %s\nwant %s", name, raw, *want)
		}
	}
	_ = json.Valid
}
