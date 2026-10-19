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

	"harnessly/internal/jsvalue"
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
