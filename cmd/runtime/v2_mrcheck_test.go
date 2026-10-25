package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/services"
)

// The GitLab is asked once for a merge request however many ask at once, and again only after what it said is stale;
// the requests in flight stay within mrParallel.
func TestMergeRequestCheckerSharesAndBoundsRequests(t *testing.T) {
	var hits, inflight, peak atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inflight.Add(-1)
		if r.URL.EscapedPath() == "/api/v4/projects/team%2Fp0/merge_requests/1" {
			_, _ = w.Write([]byte(`{"iid":1,"title":"One","web_url":"https://gitlab.example.com/team/p0/-/merge_requests/1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()
	t.Setenv("GITLAB_SELF_TOKEN", "t")
	c := newMRChecker(services.New(services.Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL}))

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if info, ok := c.lookup(context.Background(), "gitlab.example.com", "team/p0", 1); !ok || info.title != "One" || info.url == "" {
				t.Errorf("%+v %v", info, ok)
			}
		}()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("%d requests for one merge request", hits.Load())
	}
	if _, ok := c.lookup(context.Background(), "gitlab.example.com", "team/p0", 2); ok {
		t.Error("a merge request that is not there")
	}
	c.lookup(context.Background(), "gitlab.example.com", "team/p0", 2)
	if hits.Load() != 2 {
		t.Fatalf("a no is asked again at once: %d requests", hits.Load())
	}

	hits.Store(0)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.lookup(context.Background(), "gitlab.example.com", "team/other"+string(rune('a'+i)), 3)
		}()
	}
	wg.Wait()
	if hits.Load() != 20 || peak.Load() > mrParallel {
		t.Fatalf("%d requests, %d at once (limit %d)", hits.Load(), peak.Load(), mrParallel)
	}

	// An asker that gives up does not take the answer from the others.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := c.lookup(ctx, "gitlab.example.com", "team/slow", 4); ok {
		t.Error("a cancelled ask answered")
	}
}
