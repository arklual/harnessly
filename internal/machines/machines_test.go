package machines

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/push"
)

func assistantLine(req string, ts time.Time, out int) string {
	return fmt.Sprintf(`{"type":"assistant","requestId":"%s","timestamp":"%s","message":{"id":"m-%s","model":"claude-opus-4-6","usage":{"input_tokens":10,"output_tokens":%d,"cache_read_input_tokens":5}}}`,
		req, ts.UTC().Format(time.RFC3339Nano), req, out)
}

// A transcript read again after it grew, with a line still being written at its end, gives what reading it whole does.
func TestUsageReadsWhatWasAppended(t *testing.T) {
	now := time.Unix(1792315800, 0)
	base := t.TempDir()
	file := filepath.Join(base, "projects", "p", "s.jsonl")
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	write := func(s string, appendTo bool) {
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appendTo {
			flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		f, _ := os.OpenFile(file, flag, 0o600)
		_, _ = f.WriteString(s)
		f.Close()
	}
	fresh := func() map[string]*Activity {
		u := &Usage{ClaudeRoot: filepath.Join(base, "projects"), CodexRoot: filepath.Join(base, "none"), Now: func() time.Time { return now }}
		return u.Build()
	}
	u := &Usage{ClaudeRoot: filepath.Join(base, "projects"), CodexRoot: filepath.Join(base, "none"), Now: func() time.Time { return now }}
	write(assistantLine("a", now.Add(-time.Hour), 100)+"\n"+assistantLine("b", now.Add(-50*time.Minute), 200)+"\n", false)
	steps := []struct {
		text     string
		appendTo bool
	}{
		{assistantLine("b", now.Add(-50*time.Minute), 900)[:40], true}, // a chunk of b, not yet a whole line
		{assistantLine("b", now.Add(-50*time.Minute), 900)[40:] + "\n", true},
		{assistantLine("c", now.Add(-10*time.Minute), 50), true}, // no newline yet, but whole
		{"\n", true},
		{assistantLine("z", now.Add(-5*time.Minute), 7) + "\n", false}, // rewritten in place
	}
	for i, s := range steps {
		write(s.text, s.appendTo)
		got, want := u.Build(), fresh()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: %+v vs %+v", i, got["claude"], want["claude"])
		}
	}
	if a := u.Build()["claude"]; a == nil || a.Cum[len(a.Cum)-1] <= 0 {
		t.Fatal("no spend")
	}
}

func TestUsageActivityNeverWaits(t *testing.T) {
	now := time.Unix(1792315800, 0)
	u := &Usage{ClaudeRoot: t.TempDir(), CodexRoot: t.TempDir(), Now: func() time.Time { return now }}
	if a := u.Activity(); a != nil {
		t.Fatal("the first call has nothing yet")
	}
	deadline := time.Now().Add(5 * time.Second)
	for u.Activity() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if u.Activity() == nil {
		t.Fatal("the background pass did not finish")
	}
}

// fakeProc is a /proc with the listening sockets of two processes of the user and one of somebody else.
func fakeProc(t *testing.T) string {
	root := t.TempDir()
	files := map[string]string{
		"net/tcp": "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
			"   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1\n" +
			"   1: 0100007F:1F40 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 222 1\n" +
			"   2: 0100007F:2710 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1001        0 333 1\n" +
			"   3: 0100007F:0050 00000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 444 1\n",
		"net/tcp6": "  sl  local_address remote_address st\n" +
			"   0: 00000000000000000000000000000000:1F41 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 555 1\n",
	}
	for name, content := range files {
		_ = os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755)
		_ = os.WriteFile(filepath.Join(root, name), []byte(content), 0o644)
	}
	link := func(pid, fd, target string) {
		dir := filepath.Join(root, pid, "fd")
		_ = os.MkdirAll(dir, 0o755)
		_ = os.Symlink(target, filepath.Join(dir, fd))
	}
	link("10", "3", "socket:[111]")
	link("11", "3", "socket:[111]") // forked: the newest pid is named
	link("11", "4", "socket:[222]")
	link("11", "5", "socket:[555]")
	link("12", "3", "socket:[333]")
	link("12", "4", "/dev/null")
	for _, pid := range []string{"10", "11", "12"} {
		_ = os.Symlink("/work/"+pid, filepath.Join(root, pid, "cwd"))
	}
	return root
}

func TestOwnedPortsListsAndProbes(t *testing.T) {
	root := fakeProc(t)
	var probes atomic.Int32
	o := &OwnedPorts{UID: 1000, ProcRoot: root, Hidden: map[int]bool{8001: true},
		OwnerOf: func(pid int) (int, bool) {
			if pid == 12 {
				return 1001, true
			}
			return 1000, true
		},
		Probe: func(port int, budget time.Duration) bool {
			probes.Add(1)
			if port == 8000 {
				time.Sleep(3 * time.Second) // never answers within the cap
			}
			return port == 5173
		},
	}
	start := time.Now()
	ports, gen := o.List(nil)
	if took := time.Since(start); took > 2500*time.Millisecond {
		t.Fatalf("the probes held the listing for %v", took)
	}
	want := []LoadPort{{Port: 5173, PID: 11, Cwd: "/work/11", Responding: true, Fresh: true}, {Port: 8000, PID: 11, Cwd: "/work/11"}}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("%+v", ports)
	}
	if again, gen2 := o.List(nil); gen2 != gen || !reflect.DeepEqual(again, ports) {
		t.Fatal("a listing within two seconds is the same one")
	}
	if o.ListenerPID(8001) != 11 || o.ListenerPID(10000) != 0 {
		t.Fatal("ListenerPID")
	}
}

func TestLoadGivesUpASlowScan(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "stat"), []byte("cpu  1 2 3 4 5 6 7 8\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "meminfo"), []byte("MemTotal: 100 kB\nMemAvailable: 50 kB\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "loadavg"), []byte("0.5 0.4 0.3 1/2 3\n"), 0o644)
	var slow atomic.Bool
	slow.Store(true)
	m := NewLoadMonitor(LoadDeps{ProcRoot: root, Cores: 2, ScanTimeout: 500 * time.Millisecond, Sleep: func(time.Duration) {},
		Statfs: func(string) (float64, float64, float64, error) {
			if slow.Load() {
				time.Sleep(time.Second)
			}
			return 10, 5, 5, nil
		}})
	if _, err := m.Load(); !errors.Is(err, ErrScanTimeout) {
		t.Fatalf("got %v", err)
	}
	slow.Store(false)
	time.Sleep(600 * time.Millisecond) // the abandoned scan finishes and must leave nothing behind
	l, err := m.Load()
	if err != nil || l.Disk.UsedPct != 50 || l.Mem.SwapInPerSec != nil {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestLoadServesARecentResultWhileTheNextScanRuns(t *testing.T) {
	now := time.Unix(1792315800, 0)
	m := NewLoadMonitor(LoadDeps{Now: func() time.Time { return now }})
	m.served, m.servedAt = &MachineLoad{Level: "ok"}, now.Add(-5*time.Second)
	l, err := m.Load()
	if err != nil || l.Level != "ok" {
		t.Fatalf("%v %v", l, err)
	}
}

func goldenGate(t *testing.T, cfg Config) *gate.Gate {
	t.Helper()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["machines"]}`), 0o600)
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f.Add(f.AsRoutes(Routes(cfg)...)...)
	return f
}

func serve(f http.Handler, method, url string, h map[string]string) (int, http.Header, string) {
	req := httptest.NewRequest(method, url, nil)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, rec.Result().Header, string(b)
}

// The routes that need the previous runtime alive, a manifest file or a configured push are left to the previous
// runtime without them: the gate hands them on (here to nothing, a 502).
func TestRoutesStepAsideWithoutWhatTheyNeed(t *testing.T) {
	data := t.TempDir()
	alive := false
	manifest := filepath.Join(data, "manifest.json")
	f := goldenGate(t, Config{PlatformAPI: "/api/platform", DataDir: data, Push: push.New(push.Config{Dir: data}),
		SessionOK:    func(*http.Request) bool { return true },
		APIRefusal:   func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
		Health:       Health{App: "harnessly", FallbackVersion: "0.0.0-development", Alive: func() bool { return alive }},
		ManifestFile: manifest})
	for _, p := range []string{"/api/health", "/api/manifest", "/api/push/test", "/api/platform/release-notice"} {
		if code, _, _ := serve(f, "POST", p, nil); code != http.StatusBadGateway {
			t.Errorf("%s answered %d", p, code)
		}
	}
	alive = true
	if code, _, body := serve(f, "GET", "/agents/api/health", nil); code != 200 || body != `{"app":"harnessly","version":"0.0.0-development","commit":null}` {
		t.Fatalf("%d %s", code, body)
	}
	_ = os.WriteFile(manifest, []byte(`{ "name": "Acme", "icons": [ {"src": "/i.png"} ], "id": "/agents/" }`), 0o600)
	code, h, body := serve(f, "GET", "/api/manifest", nil)
	if code != 200 || body != `{"name":"Acme","icons":[{"src":"/i.png"}],"id":"/agents/"}` || h.Get("Cache-Control") != "no-cache" ||
		h.Get("Content-Type") != "application/json; charset=utf-8" || h.Get("ETag") == "" {
		t.Fatalf("%d %v %s", code, h, body)
	}
	if code, _, _ := serve(f, "GET", "/api/manifest", map[string]string{"If-None-Match": h.Get("ETag")}); code != 304 {
		t.Fatalf("%d", code)
	}
}

func TestShadowOfTheSnapshotIgnoresTheMomentsFigures(t *testing.T) {
	routes := Routes(Config{PlatformAPI: "/api/platform"})
	vol := routes[0].Volatile
	if len(vol) == 0 || !strings.HasPrefix(vol[0], "machines.*.") {
		t.Fatalf("%v", vol)
	}
	for _, v := range vol {
		if strings.Count(v, "*") == 0 {
			t.Errorf("%s", v)
		}
	}
	_ = strconv.Itoa
}

// What was read is kept across a restart: the next process starts from it and reads only what was added since.
func TestUsageCacheSurvivesARestart(t *testing.T) {
	now := time.Unix(1792315800, 0)
	base := t.TempDir()
	file := filepath.Join(base, "projects", "p", "s.jsonl")
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	_ = os.WriteFile(file, []byte(assistantLine("a", now.Add(-time.Hour), 100)+"\n"), 0o600)
	codex := filepath.Join(base, "sessions", "2026", "r.jsonl")
	_ = os.MkdirAll(filepath.Dir(codex), 0o755)
	tok := func(ts time.Time, in, out int) string {
		return fmt.Sprintf(`{"timestamp":"%s","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"output_tokens":%d}}}}`,
			ts.UTC().Format(time.RFC3339Nano), in, out)
	}
	_ = os.WriteFile(codex, []byte(`{"type":"turn_context","payload":{"model":"gpt-5.5"}}`+"\n"+tok(now.Add(-time.Hour), 1000, 10)+"\n"), 0o600)
	cache := filepath.Join(base, "cache")
	mk := func() *Usage {
		return &Usage{ClaudeRoot: filepath.Join(base, "projects"), CodexRoot: filepath.Join(base, "sessions"), CacheFile: cache,
			Now: func() time.Time { return now }}
	}
	first := mk().Build()
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("no cache written")
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(assistantLine("b", now.Add(-time.Minute), 50) + "\n")
	f.Close()
	f, _ = os.OpenFile(codex, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(tok(now.Add(-time.Minute), 3000, 40) + "\n")
	f.Close()
	restarted := mk()
	got := restarted.Build()
	want := (&Usage{ClaudeRoot: filepath.Join(base, "projects"), CodexRoot: filepath.Join(base, "sessions"), Now: func() time.Time { return now }}).Build()
	if !reflect.DeepEqual(got, want) || reflect.DeepEqual(got, first) {
		t.Fatalf("%+v\n%+v", got["codex"], want["codex"])
	}
	if tr := restarted.files[codex]; tr == nil || tr.model != "gpt-5.5" || len(tr.rows) != 2 {
		t.Fatalf("%+v", tr)
	}
}
