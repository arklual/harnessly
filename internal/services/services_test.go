package services

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// tlsTo is a transport that reaches one test server whatever host a request names.
func tlsTo(srv *httptest.Server) http.RoundTripper {
	addr := srv.Listener.Addr().String()
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

func TestFetchRedirectsAsFetchDoes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a/b":
			w.Header().Set("Location", "../c?q=1#frag")
			w.WriteHeader(302)
		case "/c":
			if r.Header.Get("Referer") != "" {
				t.Errorf("a redirect carries no Referer")
			}
			_, _ = w.Write([]byte(r.Host + " " + r.RequestURI))
		case "/loop":
			w.Header().Set("Location", "/loop")
			w.WriteHeader(301)
		case "/elsewhere":
			w.Header().Set("Location", "ftp://files.example.com/x")
			w.WriteHeader(302)
		default:
			_, _ = w.Write([]byte(r.Host + " " + r.RequestURI))
		}
	}))
	defer srv.Close()
	s := New(Config{Transport: tlsTo(srv)})
	ctx := context.Background()
	resp, err := s.fetch(ctx, "https://h.example.com/a/b", nil, follow, time.Second, 0)
	if err != nil || resp.status != 200 || resp.url != "https://h.example.com/c?q=1" || string(resp.body) != "h.example.com /c?q=1" {
		t.Fatalf("%v %+v %s", err, resp, resp.body)
	}
	if _, err := s.fetch(ctx, "https://h.example.com/a/b", nil, refuse, time.Second, 0); err == nil {
		t.Error("a refused redirect is an error")
	}
	if _, err := s.fetch(ctx, "https://h.example.com/loop", nil, follow, time.Second, 0); err == nil {
		t.Error("more than 20 redirects is an error")
	}
	if _, err := s.fetch(ctx, "https://h.example.com/elsewhere", nil, follow, time.Second, 0); err == nil {
		t.Error("a redirect off http(s) is an error")
	}
	// A path the WHATWG serializer leaves as it is goes out unchanged.
	resp, err = s.fetch(ctx, "https://h.example.com/c%2Fd/x'(?a=%7e&b=%2F", nil, follow, time.Second, 0)
	if err != nil || string(resp.body) != "h.example.com /c%2Fd/x'(?a=%7e&b=%2F" {
		t.Fatalf("%v %s", err, resp.body)
	}
}

// Two polls at once share one GitLab request; an answer is reused only for a few seconds.
func TestGitLabAnswersAreSharedBriefly(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"iid":1}`))
	}))
	defer srv.Close()
	s := New(Config{Transport: tlsTo(srv), GitLabHosts: []string{"gitlab.example.com"}})
	a := &access{host: "gitlab.example.com", token: "t"}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := s.api(context.Background(), a, "/projects/1/merge_requests/1"); err != nil || opt(v, "iid") != 1.0 {
				t.Errorf("%v %v", v, err)
			}
		}()
	}
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
	other := &access{host: "gitlab.example.com", token: "other"}
	_, _ = s.api(context.Background(), other, "/projects/1/merge_requests/1")
	if n := hits.Load(); n != 2 {
		t.Fatalf("another token shares nothing: %d requests", n)
	}
	s.gitlab.mu.Lock()
	for _, e := range s.gitlab.answers {
		e.at = e.at.Add(-apiFresh - time.Second)
	}
	s.gitlab.mu.Unlock()
	_, _ = s.api(context.Background(), a, "/projects/1/merge_requests/1")
	if n := hits.Load(); n != 3 {
		t.Fatalf("a stale answer is read again: %d requests", n)
	}
}

// Without a transcript nothing of the session counts, so no git runs at all; the answer is the previous runtime's.
func TestNoTranscriptNeedsNoGit(t *testing.T) {
	s := New(Config{GitLabHosts: []string{"gitlab.example.com"}})
	t.Setenv("PATH", t.TempDir())
	out, err := s.SessionGitLab(context.Background(), []string{"/nonexistent/x", "/"}, nil, nil)
	if err != nil || string(stringify(out)) != `{"pushes":[],"mrs":[]}` {
		t.Fatalf("%v %s", err, stringify(out))
	}
}

func TestSecretPrecedence(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "acme")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "gitlab-token"), []byte("  \n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "glab-token"), []byte(" from-glab \n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "gitlab-self-token"), []byte("self\n"), 0o600)
	s := New(Config{Home: home, SecretDirs: []string{".config/other", ".config/acme"}, GitLabHosts: []string{"gitlab.example.com", "gitlab.com"}})
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_SELF_TOKEN", "")
	if got := s.gitlabToken("gitlab.com"); got != "from-glab" {
		t.Errorf("an empty file falls through to the next name: %q", got)
	}
	if got := s.gitlabToken("gitlab.example.com"); got != "self" {
		t.Errorf("the self-hosted GitLab reads its own token: %q", got)
	}
	t.Setenv("GITLAB_SELF_TOKEN", " env ")
	if got := s.gitlabToken("gitlab.example.com"); got != "env" {
		t.Errorf("the environment comes first: %q", got)
	}
	if got := s.trackerToken(); got != "" {
		t.Errorf("no tracker named, no token: %q", got)
	}
}

func stringify(v any) []byte {
	if v == nil {
		return []byte("null")
	}
	return jsvalue.Stringify(v, "")
}

// A merge request is there when its GitLab gives it to the host's own token: the project path is escaped, a renamed
// project answers under its new path, and every kind of "no" is false.
func TestMergeRequestAsksTheGitLabWithItsToken(t *testing.T) {
	var asked []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.EscapedPath()+" "+r.Header.Get("Private-Token"))
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/team%2Fapp/merge_requests/80":
			_, _ = w.Write([]byte(`{"iid":80,"title":"Fix the queue","web_url":"https://gitlab.example.com/team/app-new/-/merge_requests/80"}`))
		case "/api/v4/projects/team%2Fapp/merge_requests/81":
			_, _ = w.Write([]byte(`{"iid":7,"web_url":"https://gitlab.example.com/team/app/-/merge_requests/7"}`))
		case "/api/v4/projects/team%2Fapp/merge_requests/82":
			_, _ = w.Write([]byte(`{"iid":82,"web_url":"https://elsewhere.example/team/app/-/merge_requests/82"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	t.Setenv("GITLAB_SELF_TOKEN", "self-token")
	s := New(Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL})
	ctx := context.Background()
	info, ok := s.MergeRequest(ctx, "gitlab.example.com", "team/app", 80)
	if !ok || info.Title != "Fix the queue" || info.URL != "https://gitlab.example.com/team/app-new/-/merge_requests/80" {
		t.Fatalf("%+v %v", info, ok)
	}
	if len(asked) != 1 || asked[0] != "/api/v4/projects/team%2Fapp/merge_requests/80 self-token" {
		t.Fatalf("asked %q", asked)
	}
	for name, c := range map[string]struct {
		host, project string
		iid           int
	}{
		"missing":          {"gitlab.example.com", "team/app", 99},
		"another number":   {"gitlab.example.com", "team/app", 81},
		"another server":   {"gitlab.example.com", "team/app", 82},
		"not our GitLab":   {"other.example.com", "team/app", 80},
		"no project":       {"gitlab.example.com", "", 80},
		"no such number":   {"gitlab.example.com", "team/app", 0},
		"unreadable group": {"gitlab.example.com", "nobody/nothing", 1},
	} {
		if _, ok := s.MergeRequest(ctx, c.host, c.project, c.iid); ok {
			t.Errorf("%s: found", name)
		}
	}
	t.Setenv("GITLAB_SELF_TOKEN", "")
	if _, ok := New(Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL}).MergeRequest(ctx, "gitlab.example.com", "team/app", 80); ok {
		t.Error("found without a token")
	}
}

// The GitLab's search is read with the host's token and only the projects whose last segment is the name count.
func TestProjectPathsKeepsTheProjectsCalledSo(t *testing.T) {
	var asked []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI()+" "+r.Header.Get("Private-Token"))
		_, _ = w.Write([]byte(`[{"path":"harnessly","path_with_namespace":"harnessly/harnessly"},{"path":"harnessly-docs","path_with_namespace":"harnessly/harnessly-docs"},{"path":"Harnessly","path_with_namespace":"old/Harnessly"}]`))
	}))
	defer api.Close()
	t.Setenv("GITLAB_SELF_TOKEN", "self-token")
	s := New(Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL})
	got := s.ProjectPaths(context.Background(), "gitlab.example.com", "harnessly")
	if len(got) != 2 || got[0] != "harnessly/harnessly" || got[1] != "old/Harnessly" {
		t.Fatalf("%q", got)
	}
	if len(asked) != 1 || asked[0] != "/api/v4/projects?simple=true&search=harnessly&per_page=20 self-token" {
		t.Fatalf("asked %q", asked)
	}
	for _, c := range []struct{ host, name string }{{"other.example.com", "harnessly"}, {"gitlab.example.com", ""}, {"gitlab.example.com", "a/b"}, {"gitlab.example.com", "a&b"}} {
		if got := s.ProjectPaths(context.Background(), c.host, c.name); got != nil {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestArtifactTitleIsReadFromTheHeadOnly(t *testing.T) {
	var sent atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>План выката</title><meta name=\"description\" content=\"Что едет\"></head><body>"))
		// 256 MiB behind the head, written until the client hangs up. What the server pushes before the hang-up
		// lands in socket buffers (a few MiB, more under load); only a client reading the whole body gets near the end.
		chunk := make([]byte, 64<<10)
		for i := 0; i < 4096; i++ {
			n, err := w.Write(chunk)
			sent.Add(int64(n))
			if err != nil || r.Context().Err() != nil {
				return
			}
		}
	}))
	defer srv.Close()
	s := New(Config{Transport: tlsTo(srv)})
	meta, err := s.fetchArtifact(context.Background(), "https://docs.example.com/a/plan")
	if err != nil {
		t.Fatal(err)
	}
	if opt(meta, "title") != "План выката" || opt(meta, "description") != "Что едет" {
		t.Fatalf("meta = %v", meta)
	}
	if sent.Load() >= 64<<20 {
		t.Fatalf("the whole page went through (%d bytes): only its head was needed", sent.Load())
	}
}

// An answer past its five minutes is given at once and read again behind: a panel reopened later does not wait.
func TestLinkMetaOldAnswerIsGivenAtOnceAndRefreshed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		if n > 1 {
			time.Sleep(300 * time.Millisecond)
		}
		_, _ = w.Write([]byte("<head><title>Версия " + string(rune('0'+n)) + "</title></head>"))
	}))
	defer srv.Close()
	s := New(Config{Transport: tlsTo(srv)})
	target := &linkTarget{kind: "artifact", url: "https://docs.example.com/a/plan"}
	if got := opt(s.linkMeta(context.Background(), target), "title"); got != "Версия 1" {
		t.Fatalf("first = %v", got)
	}
	s.meta.mu.Lock()
	for k, e := range s.meta.entries {
		e.at = e.at.Add(-metaFresh - time.Minute)
		s.meta.entries[k] = e
	}
	s.meta.mu.Unlock()
	began := time.Now()
	if got := opt(s.linkMeta(context.Background(), target), "title"); got != "Версия 1" {
		t.Fatalf("old = %v", got)
	}
	if took := time.Since(began); took > 150*time.Millisecond {
		t.Fatalf("the old answer waited for the read: %s", took)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if opt(s.linkMeta(context.Background(), target), "title") == "Версия 2" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the link was not read again behind the old answer")
}
