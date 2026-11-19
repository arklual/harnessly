package mcpconn

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestRealRemoteServers probes every remote server of the catalog as the page does when it connects one:
// HARNESSLY_E2E_REMOTE=1. A server that does not answer as MCP, or signs in otherwise than the entry says, is listed.
func TestRealRemoteServers(t *testing.T) {
	if os.Getenv("HARNESSLY_E2E_REMOTE") == "" {
		t.Skip("set HARNESSLY_E2E_REMOTE=1")
	}
	s, _ := newService(t)
	var mu sync.Mutex
	bad := map[string]string{}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, e := range catalog {
		if e.Transport != HTTP && e.Transport != SSE {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(e CatalogEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			p, err := s.probe(ctx, e.URL, e.Transport)
			why := ""
			switch {
			case err != nil:
				why = "probe: " + err.Error()
			case p.Auth == AuthOAuth && p.OAuthUnavailable && e.Auth == AuthOAuth:
				why = "oauth without registration"
			case e.Auth == AuthOAuth && p.Auth == AuthHeader:
				why = "expected oauth, got " + string(p.Auth)
			}
			if why != "" {
				mu.Lock()
				bad[e.ID] = why
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	ids := make([]string, 0, len(bad))
	for id := range bad {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t.Errorf("%-26s %s", id, bad[id])
	}
}
