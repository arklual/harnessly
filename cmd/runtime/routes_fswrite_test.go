package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/files"
	"harnessly/internal/gate"
	"harnessly/internal/machines"
	"harnessly/internal/nodejson"
	"harnessly/internal/tmux"
)

// testdata/fswrite-golden.json: what the previous runtime's dispatcher and disk-writing handlers answered
// (testdata/fswrite, under node) on the same folders and repositories, and the files they left.

type fswriteFixture struct {
	Path   string  `json:"path"`
	Text   *string `json:"text"`
	Base64 *string `json:"base64"`
	Repeat string  `json:"repeat"`
	Count  int     `json:"count"`
	Link   string  `json:"link"`
	Age    *int64  `json:"age"`
}

type fswriteRequest struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         [][2]string       `json:"query"`
	Headers       map[string]string `json:"headers"`
	Body          json.RawMessage   `json:"body"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
	Uploads       []map[string]any  `json:"uploads"`
}

var revisionField = regexp.MustCompile(`"revision":"[0-9a-f]{64}"`)

// fsSnapshot lists a folder as the generator did: paths under root, links, folders, files with mode and small contents.
func fsSnapshot(root, dir string) []map[string]any {
	var out []map[string]any
	var walk func(d string)
	walk = func(d string) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, n := range names {
			p := filepath.Join(d, n)
			rel, _ := filepath.Rel(root, p)
			st, _ := os.Lstat(p)
			switch {
			case st.Mode()&os.ModeSymlink != 0:
				target, _ := os.Readlink(p)
				out = append(out, map[string]any{"path": rel, "link": target})
			case st.IsDir():
				out = append(out, map[string]any{"path": rel, "dir": true})
				walk(p)
			default:
				var content any
				if st.Size() <= 4096 {
					raw, _ := os.ReadFile(p)
					content = base64.StdEncoding.EncodeToString(raw)
				}
				out = append(out, map[string]any{"path": rel, "mode": strconv.FormatInt(int64(st.Mode().Perm()), 8), "base64": content, "size": float64(st.Size())})
			}
		}
	}
	walk(dir)
	return out
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	var u, v any
	_ = json.Unmarshal(x, &u)
	_ = json.Unmarshal(y, &v)
	p, _ := json.Marshal(u)
	q, _ := json.Marshal(v)
	return bytes.Equal(p, q)
}

// latin1 is a header value as the generator's client sent it: each character up to U+00FF as one byte.
func latin1(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return string(b)
}

// withoutGitText blanks what git printed in a sync's steps: another git version words it otherwise.
func withoutGitText(answer string) string {
	var v map[string]any
	if json.Unmarshal([]byte(answer), &v) != nil {
		return answer
	}
	steps, _ := v["steps"].([]any)
	for _, s := range steps {
		if m, ok := s.(map[string]any); ok {
			m["stdout"], m["stderr"] = "", ""
		}
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestFswriteRoutesAnswerAsTheOldRuntime(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	raw, err := os.ReadFile("testdata/fswrite-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Now        int64            `json:"now"`
		Token      string           `json:"token"`
		GitVersion string           `json:"gitVersion"`
		GitSetup   string           `json:"gitSetup"`
		Fixtures   []fswriteFixture `json:"fixtures"`
		Requests   []fswriteRequest `json:"requests"`
		Trees      map[string]any   `json:"trees"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home, data := filepath.Join(root, "home"), filepath.Join(root, "data")
	for _, d := range []string{home, data} {
		_ = os.MkdirAll(d, 0o755)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("RUNTIME_UPLOAD_HEADER_PREFIXES", "x-acme-,x-old-")
	t.Setenv("RUNTIME_UPLOADS_DIR", "")
	now := time.UnixMilli(g.Now)
	for _, f := range g.Fixtures {
		p := filepath.Join(root, f.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		switch {
		case f.Link != "":
			_ = os.Symlink(f.Link, p)
		case f.Base64 != nil:
			b, _ := base64.StdEncoding.DecodeString(*f.Base64)
			_ = os.WriteFile(p, b, 0o644)
		case f.Repeat != "":
			_ = os.WriteFile(p, []byte(strings.Repeat(f.Repeat, f.Count)), 0o644)
		default:
			_ = os.WriteFile(p, []byte(*f.Text), 0o644)
		}
		if f.Age != nil {
			at := now.Add(-time.Duration(*f.Age) * time.Second)
			_ = os.Chtimes(p, at, at)
		}
	}
	setup := exec.Command("sh", "-c", g.GitSetup)
	setup.Env = append(os.Environ(), "ROOT="+root)
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("git setup: %v %s", err, out)
	}
	gitVersion, _ := exec.Command("git", "--version").Output()
	sameGit := strings.TrimSpace(string(gitVersion)) == g.GitVersion

	socket := "hs-test-fswrite-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	sessions := map[string]string{"edit-1": filepath.Join(root, "proj"), "edit-gone": filepath.Join(root, "nowhere")}
	for _, n := range []string{"norepo", "noup", "uptodate", "behind", "ahead", "diverged", "local", "fetchfail"} {
		sessions["sync-"+n] = filepath.Join(root, n)
	}
	_ = os.MkdirAll(sessions["edit-gone"], 0o755)
	for name, dir := range sessions {
		if out, err := exec.Command("tmux", "-L", socket, "-f", "/dev/null", "new-session", "-d", "-s", name, "-c", dir, "sleep 3600").CombinedOutput(); err != nil {
			t.Fatalf("tmux: %v %s", err, out)
		}
	}
	_ = os.RemoveAll(sessions["edit-gone"])

	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte(g.Token), 0o600)
	_ = os.WriteFile(filepath.Join(data, "state-mode.json"), []byte(`{"goSlices":["fswrite"]}`), 0o600)
	sessionOK := func(r *http.Request) bool {
		c, err := r.Cookie("session-token")
		return err == nil && c.Value == "good"
	}
	tokenOK := cliTokenFirst(data, []string{"x-cli-token", "x-old-cli-token"})
	d := deps{Home: home, DataDir: data, Tmux: tm, TokenHeaders: []string{"x-cli-token", "x-old-cli-token"}, SessionOK: sessionOK,
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) || tokenOK(r) {
				return 0, nil, nil, false
			}
			return http.StatusUnauthorized, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		}}
	draws := 0
	prevUploads := newUploads
	newUploads = func(d deps) *files.Uploads {
		return &files.Uploads{Dir: uploadsDir(d), Now: func() time.Time { return now },
			Random: func() string { draws++; return fmt.Sprintf("%08x", draws) }}
	}
	t.Cleanup(func() { newUploads = prevUploads })
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: data, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	stopper := &machines.Stopper{Listed: func() ([]machines.LoadProcess, error) { return nil, nil }, UID: os.Getuid(),
		RuntimePIDs: func() []int { return []int{os.Getpid()} }}
	f.Add(f.AsRoutes(append(fswriteRoutes(d), stopRoute(d, stopper))...)...)
	srv := httptest.NewServer(f)
	defer srv.Close()

	// The revisions the page opened the files at, over this project's real path.
	proj, _ := filepath.EvalSymlinks(filepath.Join(root, "proj"))
	var pairs []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "sub/d.txt"} {
		content, _ := os.ReadFile(filepath.Join(proj, name))
		h := sha256.New()
		h.Write([]byte(proj + "\x00" + filepath.Join(proj, name) + "\x00"))
		h.Write(content)
		pairs = append(pairs, "{{rev:"+name+"}}", hex.EncodeToString(h.Sum(nil)))
	}
	revisions := strings.NewReplacer(pairs...)
	scrub := func(s string) string {
		return revisionField.ReplaceAllString(strings.ReplaceAll(s, root, "$ROOT"), `"revision":"{{rev}}"`)
	}
	for i, c := range g.Requests {
		var body string
		if err := json.Unmarshal(c.Body, &body); err != nil {
			var long struct {
				Head, Repeat, Tail string
				Count              int
			}
			_ = json.Unmarshal(c.Body, &long)
			body = long.Head + strings.Repeat(long.Repeat, long.Count) + long.Tail
		}
		body = revisions.Replace(body)
		var q []string
		for _, kv := range c.Query {
			q = append(q, url.QueryEscape(kv[0])+"="+url.QueryEscape(kv[1]))
		}
		u := srv.URL + c.Path
		if len(q) > 0 {
			u += "?" + strings.Join(q, "&")
		}
		req, _ := http.NewRequest(c.Method, u, strings.NewReader(body))
		for k, v := range c.Headers {
			req.Header[http.CanonicalHeaderKey(k)] = []string{latin1(v)}
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("#%d: %v", i, err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		where := fmt.Sprintf("#%d %s %s %v", i, c.Method, c.Path, c.Query)
		answer, want := scrub(string(got)), c.Answer
		if strings.HasSuffix(c.Path, "/git/sync") && !sameGit {
			answer, want = withoutGitText(answer), withoutGitText(want)
		}
		if res.StatusCode != c.Status || answer != want {
			t.Errorf("%s\n got %d %s\nwant %d %s", where, res.StatusCode, answer, c.Status, want)
			continue
		}
		for k, w := range c.AnswerHeaders {
			have := res.Header.Get(k)
			switch {
			case k == "content-length":
			case k == "etag" && (scrub(string(got)) != string(got) || strings.HasSuffix(c.Path, "/git/sync")):
				if have != nodejson.ETag(got) {
					t.Errorf("%s: etag %s", where, have)
				}
			case have != w:
				t.Errorf("%s: %s = %q, want %q", where, k, have, w)
			}
		}
		for _, k := range []string{"ETag", "Allow", "Cache-Control"} {
			if _, ok := c.AnswerHeaders[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
				t.Errorf("%s: unexpected %s: %s", where, k, res.Header.Get(k))
			}
		}
		if c.Uploads != nil {
			if have := fsSnapshot(root, filepath.Join(data, "uploads")); !sameJSON(have, c.Uploads) {
				t.Errorf("%s: uploads\n got %v\nwant %v", where, have, c.Uploads)
			}
		}
	}
	if have := fsSnapshot(root, filepath.Join(data, "uploads")); !sameJSON(have, g.Trees["uploads"]) {
		t.Errorf("uploads left\n got %v\nwant %v", have, g.Trees["uploads"])
	}
	if have := fsSnapshot(root, filepath.Join(root, "proj")); !sameJSON(have, g.Trees["proj"]) {
		t.Errorf("project left\n got %v\nwant %v", have, g.Trees["proj"])
	}
	for name, want := range g.Trees {
		if name == "uploads" || name == "proj" {
			continue
		}
		out, _ := exec.Command("git", "-C", filepath.Join(root, name), "log", "--all", "--format=%H %D").Output()
		if string(out) != want {
			t.Errorf("repository %s:\n got %s\nwant %s", name, out, want)
		}
	}
}

// Where the Go uploads part from the previous runtime: an upload over its limit is answered (that runtime dropped the
// connection), and a type name every JavaScript object carries is no image type.
func TestUploadRefusals(t *testing.T) {
	data := t.TempDir()
	t.Setenv("RUNTIME_UPLOADS_DIR", "")
	_ = os.WriteFile(filepath.Join(data, "state-mode.json"), []byte(`{"goSlices":["fswrite"]}`), 0o600)
	d := deps{DataDir: data, SessionOK: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: data, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f.Add(f.AsRoutes(fswriteRoutes(d)...)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	post := func(mime string, body []byte) (int, string) {
		req, _ := http.NewRequest("POST", srv.URL+"/api/upload-image", bytes.NewReader(body))
		req.Header.Set("Content-Type", mime)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := post("image/png", make([]byte, files.ImageMaxBytes+1)); code != 413 || body != `{"error":"Image exceeds 10MB"}` {
		t.Fatalf("too large: %d %s", code, body)
	}
	if code, body := post("constructor", []byte("x")); code != 400 || body != `{"error":"Unsupported image type"}` {
		t.Fatalf("constructor: %d %s", code, body)
	}
	if code, _ := post("image/png", make([]byte, files.ImageMaxBytes)); code != 200 {
		t.Fatalf("at the limit: %d", code)
	}
}
