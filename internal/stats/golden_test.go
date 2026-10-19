package stats

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/gate"
)

// testdata/golden.json: what the previous runtime's dispatcher and /api/stats/* handlers answered (testdata/gen, under
// node) on generated homes, step by step on a frozen clock, and the cache files each request left. Every request must be
// answered alike and leave the same files.

type goldenFile struct {
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	B64     string `json:"b64"`
	Raw     string `json:"raw"`
	Append  bool   `json:"append"`
	MtimeNs string `json:"mtimeNs"`
}

type goldenStep struct {
	Kind          string             `json:"kind"`
	Now           float64            `json:"now"`
	Files         []goldenFile       `json:"files"`
	Method        string             `json:"method"`
	Path          string             `json:"path"`
	Query         [][2]string        `json:"query"`
	Headers       map[string]string  `json:"headers"`
	Body          *string            `json:"body"`
	Status        int                `json:"status"`
	AnswerHeaders map[string]string  `json:"answerHeaders"`
	Answer        string             `json:"answer"`
	Written       map[string]*string `json:"written"`
}

type goldenScenario struct {
	Files []goldenFile `json:"files"`
	Steps []goldenStep `json:"steps"`
}

type goldenSet struct {
	TZ        string            `json:"tz"`
	Home      string            `json:"home"`
	Scripts   map[string]string `json:"scripts"`
	Scenarios []goldenScenario  `json:"scenarios"`
}

type replay struct {
	t          *testing.T
	root, home string
	data       string
	homeMark   string
	now        time.Time
	rt         *Route
	last       map[string]string
}

func (p *replay) resolve(path string) string {
	switch {
	case strings.HasPrefix(path, "$HOME"):
		return p.home + path[len("$HOME"):]
	case strings.HasPrefix(path, "$DATA"):
		return p.data + path[len("$DATA"):]
	}
	return path
}

func (p *replay) write(f goldenFile) {
	file := p.resolve(f.Path)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if f.Dir {
		_ = os.MkdirAll(file, 0o755)
		return
	}
	text, _ := base64.StdEncoding.DecodeString(f.B64)
	data := []byte(strings.ReplaceAll(string(text), p.homeMark, p.home))
	if f.Raw != "" {
		raw, _ := base64.StdEncoding.DecodeString(f.Raw)
		data = append(data, raw...)
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if f.Append {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	fh, err := os.OpenFile(file, flags, 0o644)
	if err != nil {
		p.t.Fatal(err)
	}
	_, _ = fh.Write(data)
	fh.Close()
	ns, _ := strconv.ParseInt(f.MtimeNs, 10, 64)
	mt := time.Unix(0, ns)
	_ = os.Chtimes(file, mt, mt)
}

func (p *replay) snapshot() map[string]string {
	out := map[string]string{}
	for _, w := range []string{"stats/cache.json", "stats/uptime-cache.json"} {
		if b, err := os.ReadFile(filepath.Join(p.data, w)); err == nil {
			out[w] = string(b)
		}
	}
	dir := filepath.Join(p.data, "stats", "daily-reports")
	names, _ := os.ReadDir(dir)
	for _, n := range names {
		b, _ := os.ReadFile(filepath.Join(dir, n.Name()))
		out["stats/daily-reports/"+n.Name()] = string(b)
	}
	return out
}

// answer is what the gate writes for a route's (status, value, headers).
func answer(status int, v any, h http.Header) (int, string, http.Header) {
	if h == nil {
		h = http.Header{}
	}
	var body []byte
	switch x := v.(type) {
	case nil:
	case gate.Raw:
		body = x.Body
		h.Set("Content-Type", x.ContentType)
	default:
		body = gate.Encode(v)
		if h.Get("Content-Type") == "" {
			h.Set("Content-Type", "application/json; charset=utf-8")
		}
	}
	return status, string(body), h
}

func runGolden(t *testing.T, set goldenSet, limitFailures int) {
	loc, err := time.LoadLocation(set.TZ)
	if err != nil {
		t.Fatal(err)
	}
	saved := time.Local
	time.Local = loc
	defer func() { time.Local = saved }()
	failures := 0
	for si, sc := range set.Scenarios {
		root := t.TempDir()
		p := &replay{t: t, root: root, home: filepath.Join(root, "home"), data: filepath.Join(root, "data"), homeMark: set.Home}
		bin := filepath.Join(root, "bin")
		_ = os.MkdirAll(bin, 0o755)
		_ = os.MkdirAll(p.home, 0o755)
		_ = os.MkdirAll(p.data, 0o755)
		for name, text := range set.Scripts {
			if err := os.WriteFile(filepath.Join(bin, name), []byte(text), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		p.rt = &Route{Home: p.home, DataDir: p.data, ConfigFile: filepath.Join(p.data, "config.json"), CwdRewrites: DefaultCwdRewrites,
			Authorized: func(r *http.Request) bool { return strings.Contains(r.Header.Get("Cookie"), "session-token=good") },
			ShellPath:  func() string { return bin + ":/usr/bin:/bin" },
			Now:        func() time.Time { return p.now }}
		for _, f := range sc.Files {
			p.write(f)
		}
		p.last = p.snapshot()
		routes := p.rt.Routes()
		var goUptime, tsUptime []string
		for i, st := range sc.Steps {
			switch st.Kind {
			case "clock":
				p.now = time.UnixMilli(int64(st.Now))
				continue
			case "write":
				for _, f := range st.Files {
					p.write(f)
				}
				continue
			}
			q := url.Values{}
			var parts []string
			for _, kv := range st.Query {
				parts = append(parts, url.QueryEscape(kv[0])+"="+url.QueryEscape(kv[1]))
				q.Add(kv[0], kv[1])
			}
			target := st.Path
			if len(parts) > 0 {
				target += "?" + strings.Join(parts, "&")
			}
			var body *strings.Reader
			if st.Body != nil {
				body = strings.NewReader(*st.Body)
			} else {
				body = strings.NewReader("")
			}
			r := httptest.NewRequest(st.Method, target, body)
			for k, v := range st.Headers {
				r.Header.Set(k, v)
			}
			if st.Body != nil && r.Header.Get("Content-Type") == "" {
				r.Header.Set("Content-Type", "application/json")
			}
			key := strings.TrimPrefix(strings.TrimPrefix(st.Path, "/agents"), "/api/stats/")
			status, got, h := answer(routes[key](r))
			got = strings.ReplaceAll(got, root, "$ROOT")
			where := "scenario " + strconv.Itoa(si) + " step " + strconv.Itoa(i) + " " + st.Method + " " + target
			bad := false
			if status != st.Status || got != st.Answer {
				bad = true
				t.Errorf("%s: status %d want %d\n got %s\nwant %s", where, status, st.Status, clipText(got), clipText(st.Answer))
			} else {
				for _, name := range []string{"content-type", "etag", "allow"} {
					want := st.AnswerHeaders[name]
					if name == "content-type" && status == http.StatusNotModified {
						continue
					}
					if h.Get(name) != want {
						bad = true
						t.Errorf("%s: header %s %q want %q", where, name, h.Get(name), want)
					}
				}
			}
			now := p.snapshot()
			written := map[string]*string{}
			for k, v := range now {
				if old, ok := p.last[k]; !ok || old != v {
					v := v
					written[k] = &v
				}
			}
			for k := range p.last {
				if _, ok := now[k]; !ok {
					written[k] = nil
				}
			}
			p.last = now
			// The old uptime wrote its file without waiting: under load the write lands a step late. Its contents are
			// compared in order at the end of the scenario instead.
			if w, ok := written[uptimeFile]; ok && w != nil {
				goUptime = append(goUptime, *w)
			}
			// "": caught in the middle of the write, which may then put back the same contents.
			if w, ok := st.Written[uptimeFile]; ok && w != nil && *w != "" && (len(tsUptime) == 0 || tsUptime[len(tsUptime)-1] != *w) {
				tsUptime = append(tsUptime, *w)
			}
			delete(written, uptimeFile)
			for k, want := range st.Written {
				if k == uptimeFile {
					continue
				}
				g, ok := written[k]
				if !ok || (want == nil) != (g == nil) || (want != nil && *want != *g) {
					bad = true
					gs := "<unchanged>"
					if ok && g != nil {
						gs = *g
					}
					ws := "<removed>"
					if want != nil {
						ws = *want
					}
					t.Errorf("%s: wrote %s\n got %s\nwant %s", where, k, clipText(gs), clipText(ws))
				}
			}
			for k := range written {
				if _, ok := st.Written[k]; !ok {
					bad = true
					t.Errorf("%s: wrote %s, the old runtime did not", where, k)
				}
			}
			if bad {
				failures++
				if failures >= limitFailures {
					t.Fatalf("stopping after %d failing steps", failures)
				}
			}
		}
		if strings.Join(goUptime, "\n") != strings.Join(tsUptime, "\n") {
			t.Errorf("scenario %d: uptime cache written %d times, the old runtime %d, or with other contents", si, len(goUptime), len(tsUptime))
		}
	}
}

const uptimeFile = "stats/uptime-cache.json"

func clipText(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

func loadGolden(t *testing.T, path string) goldenSet {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var set goldenSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	return set
}

func TestGolden(t *testing.T) {
	runGolden(t, loadGolden(t, filepath.Join("testdata", "golden.json")), 20)
}

// TS_GOLDEN=<file> replays generated scenarios (testdata/gen with GEN_RANDOM).
func TestGeneratedGolden(t *testing.T) {
	path := os.Getenv("TS_GOLDEN")
	if path == "" {
		t.Skip("TS_GOLDEN not set")
	}
	runGolden(t, loadGolden(t, path), 30)
}
