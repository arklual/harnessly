package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The golden files come from the previous runtime's own handlers (testdata/gen), run against the same fixtures.

type fixture struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
	BodyB64 string            `json:"bodyB64"`
	Auth    []string          `json:"auth"`
	Drop    bool              `json:"drop"`
	Gen     *struct {
		Kind  string `json:"kind"`
		Count int    `json:"count"`
		Lines int    `json:"lines"`
	} `json:"gen"`
}

func (f *fixture) body() []byte {
	switch {
	case f.Gen != nil && f.Gen.Kind == "bytes":
		b := make([]byte, f.Gen.Count)
		for i := range b {
			b[i] = byte(i % 251)
		}
		return b
	case f.Gen != nil && f.Gen.Kind == "files":
		var sb strings.Builder
		sb.WriteByte('[')
		for i := 0; i < f.Gen.Count; i++ {
			if i > 0 {
				sb.WriteByte(',')
			}
			lines := make([]string, f.Gen.Lines)
			for j := range lines {
				lines[j] = "+line " + itoa(j) + " of file " + itoa(i)
			}
			diff, _ := json.Marshal("@@ -1 +1," + itoa(f.Gen.Lines) + " @@\n" + strings.Join(lines, "\n") + "\n")
			name := `"gen/f` + itoa(i) + `.txt"`
			sb.WriteString(`{"old_path":` + name + `,"new_path":` + name + `,"new_file":false,"deleted_file":false,"renamed_file":false,"diff":` + string(diff) + `}`)
		}
		sb.WriteByte(']')
		return []byte(sb.String())
	case f.BodyB64 != "":
		b, _ := base64.StdEncoding.DecodeString(f.BodyB64)
		return b
	case f.Body != nil:
		return []byte(*f.Body)
	}
	return nil
}

// fakeServices plays every external host from the fixtures, over TLS, whatever name a request dials.
func fakeServices(t *testing.T, fixtures map[string]*fixture) http.RoundTripper {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.RequestURI
		if strings.HasPrefix(target, "https://") {
			if u, err := url.Parse(target); err == nil {
				target = strings.TrimPrefix(target, "https://"+u.Host)
			}
		}
		f := fixtures[r.Host+" "+target]
		if f == nil {
			t.Logf("no fixture for %s %s (an icon fetched ahead of need)", r.Host, target)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(404)
			_, _ = w.Write([]byte("no fixture"))
			return
		}
		if f.Drop {
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					_ = c.Close()
				}
			}
			return
		}
		if len(f.Auth) == 2 && r.Header.Get(f.Auth[0]) != f.Auth[1] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
			return
		}
		for k, v := range f.Headers {
			w.Header().Set(k, v)
		}
		status := f.Status
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
		_, _ = w.Write(f.body())
	}))
	srv.EnableHTTP2 = false
	srv.StartTLS()
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
	}
}

type goldenCase struct {
	Setup *struct {
		Env   map[string]*string `json:"env"`
		Files map[string]*string `json:"files"`
	} `json:"setup"`
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	Body          *string           `json:"body"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        *string           `json:"answer"`
	AnswerB64     string            `json:"answerB64"`
	AnswerSha256  string            `json:"answerSha256"`
	AnswerLength  int               `json:"answerLength"`
}

func readJSON(t *testing.T, name string, v any) {
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

var cookieRe = regexp.MustCompile(`(?:^|;\s*)session-token=good(?:;|$)`)

func TestGoldenRoutes(t *testing.T) {
	var fixtures map[string]*fixture
	readJSON(t, "fixtures.json", &fixtures)
	var golden struct {
		Cases []goldenCase `json:"cases"`
		Files struct {
			Organizer *string `json:"organizer"`
		} `json:"files"`
	}
	readJSON(t, "golden.json", &golden)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	for _, k := range []string{"YOUTRACK_TOKEN", "GITLAB_TOKEN", "GITLAB_SELF_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := exec.Command("sh", "testdata/gen/repos.sh", root).CombinedOutput(); err != nil {
		t.Fatalf("repos.sh: %v %s", err, out)
	}
	s := New(Config{
		Home: home, SecretDirs: []string{".config/acme"}, GitLabHosts: []string{"gitlab.example.com", "gitlab.com"},
		TrackerURL: "https://tracker.example.com", TrackerHosts: []string{"tracker.example.com", "youtrack.example.com"},
		DocsHost: "docs.example.com", ReaderHost: "platform.example.com", ReaderPath: "/agents/docs/",
		TaskKey: regexp.MustCompile(`^(ACME|DEMO|OPS)-[0-9]{1,6}$`), AvatarPath: "/api/platform/link-meta/avatar",
		IconAgent: "ExampleBot favicon", OrganizerFile: filepath.Join(home, ".acme-data", "organizer.json"),
		Transport: fakeServices(t, fixtures),
	})
	h := &Handlers{S: s, Session: func(r *http.Request) bool { return cookieRe.MatchString(r.Header.Get("Cookie")) },
		Now: func() time.Time { return time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC) }}
	boot := &Bootstrap{DataDir: filepath.Join(home, ".acme-data"), ConfigFile: filepath.Join(home, ".acme-data", "config.json"), BindLocal: true,
		Owner: func(r *http.Request) any {
			if h.Session(r) {
				return "person@example.com"
			}
			return nil
		},
		Tools: func() bool { return true }, Now: h.Now}
	routes := map[string]http.HandlerFunc{
		"bootstrap": boot.Serve,
		"gitlab":    h.GitLab, "gitlab-view": h.GitLabView, "link-meta": h.LinkMeta, "link-meta/projects": h.Projects,
		"link-meta/avatar": h.Avatar, "favicon": h.Favicon, "task": h.Task, "organizer": h.Organizer,
	}
	fill := func(s string) string { return strings.ReplaceAll(s, "$ROOT", root) }
	lastEtag := ""
	for i, c := range golden.Cases {
		if c.Setup != nil {
			for k, v := range c.Setup.Env {
				if v == nil {
					t.Setenv(k, "")
				} else {
					t.Setenv(k, *v)
				}
			}
			for name, content := range c.Setup.Files {
				file := filepath.Join(root, name)
				if content == nil {
					_ = os.Remove(file)
					continue
				}
				_ = os.MkdirAll(filepath.Dir(file), 0o755)
				if err := os.WriteFile(file, []byte(fill(*content)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		var body *bytes.Reader
		if c.Body != nil {
			body = bytes.NewReader([]byte(fill(*c.Body)))
		} else {
			body = bytes.NewReader(nil)
		}
		r := httptest.NewRequest(c.Method, fill(c.URL), body)
		for k, v := range c.Headers {
			if k == "if-none-match" && v == "ETAG" {
				v = lastEtag
			}
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		path := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), "/api/platform/"), "/")
		if !h.Session(r) && r.Header.Get("x-cli-token") != "cli-secret" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
		} else {
			routes[path](w, r)
		}
		res := w.Result()
		got, _ := io.ReadAll(res.Body)
		if c.Method == http.MethodHead {
			got = nil
		}
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		name := c.Method + " " + c.URL
		if res.StatusCode != c.Status {
			t.Errorf("#%d %s: status %d, want %d (%s)", i, name, res.StatusCode, c.Status, clipText(got))
			continue
		}
		// Pushes with the same date (none, for a branch GitLab lacks) came in the order their requests finished.
		ties := path == "gitlab" && res.StatusCode == 200 && c.Answer != nil
		if ties {
			got = []byte(untie(t, got))
			untied := untie(t, []byte(*c.Answer))
			c.Answer = &untied
		}
		for k, want := range c.AnswerHeaders {
			gotH := res.Header.Get(k)
			if k == "content-length" && gotH == "" || ties && k == "etag" {
				continue
			}
			if strings.ReplaceAll(gotH, root, "$ROOT") != want {
				t.Errorf("#%d %s: header %s %q, want %q", i, name, k, gotH, want)
			}
		}
		for k := range res.Header {
			lk := strings.ToLower(k)
			if _, ok := c.AnswerHeaders[lk]; !ok && lk != "content-length" && lk != "date" {
				t.Errorf("#%d %s: extra header %s", i, name, k)
			}
		}
		switch {
		case c.AnswerSha256 != "":
			sum := sha256.Sum256(got)
			if hex.EncodeToString(sum[:]) != c.AnswerSha256 {
				t.Errorf("#%d %s: body of %d bytes differs (want %d bytes)", i, name, len(got), c.AnswerLength)
			}
		case c.AnswerB64 != "":
			want, _ := base64.StdEncoding.DecodeString(c.AnswerB64)
			if !bytes.Equal(got, want) {
				t.Errorf("#%d %s: body differs", i, name)
			}
		case c.Answer != nil:
			if gotS := strings.ReplaceAll(string(got), root, "$ROOT"); gotS != *c.Answer {
				t.Errorf("#%d %s:\n got %s\nwant %s", i, name, clipText([]byte(gotS)), clipText([]byte(*c.Answer)))
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(home, ".acme-data", "organizer.json"))
	switch {
	case golden.Files.Organizer == nil && err == nil:
		t.Errorf("organizer file written, want none")
	case golden.Files.Organizer != nil && string(raw) != *golden.Files.Organizer:
		t.Errorf("organizer file:\n got %s\nwant %s", raw, *golden.Files.Organizer)
	}
}

// untie orders pushes of equal dates by host, project and branch.
func untie(t *testing.T, raw []byte) string {
	var v struct {
		Pushes []map[string]any `json:"pushes"`
		Mrs    json.RawMessage  `json:"mrs"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	key := func(p map[string]any) string { return fmt.Sprint(p["host"], "/", p["project"], "@", p["branch"]) }
	for i := 0; i < len(v.Pushes); {
		j := i
		for j < len(v.Pushes) && fmt.Sprint(v.Pushes[j]["pushedAt"]) == fmt.Sprint(v.Pushes[i]["pushedAt"]) {
			j++
		}
		group := v.Pushes[i:j]
		sort.Slice(group, func(a, b int) bool { return key(group[a]) < key(group[b]) })
		i = j
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func clipText(b []byte) string {
	if len(b) > 1500 {
		return string(b[:1500]) + "…"
	}
	return string(b)
}
