package mcpconn

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRealLocalServers starts real local servers of the catalog with npx and uvx (it downloads them):
// HARNESSLY_E2E_MCP=time,arxiv for those ids, or "all" for every local entry, each with made-up keys. A server that
// starts and lists its tools passes; one that stops on made-up keys is reported as "keys?"; a package that does not run
// at all fails the test.
func TestRealLocalServers(t *testing.T) {
	want := os.Getenv("HARNESSLY_E2E_MCP")
	if want == "" {
		t.Skip("set HARNESSLY_E2E_MCP to catalog ids of local servers, or all")
	}
	var entries []CatalogEntry
	for _, e := range catalog {
		if e.Transport == Stdio && (want == "all" || strings.Contains(","+want+",", ","+e.ID+",")) {
			entries = append(entries, e)
		}
	}
	s, _ := newService(t)
	var mu sync.Mutex
	report := map[string]string{}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(e CatalogEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			env := map[string]string{}
			for _, f := range e.Env {
				switch {
				case f.Default != "":
					env[f.Name] = f.Default
				case strings.Contains(f.Name, "URL") || strings.Contains(f.Name, "HOST"):
					env[f.Name] = "https://example.com"
				case strings.Contains(f.Name, "PORT"):
					env[f.Name] = "27124"
				default:
					env[f.Name] = "test-key-0000000000"
				}
			}
			srv := s.newKeyed(e, AddInput{})
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			start := time.Now()
			tools, err := s.inspectLocal(ctx, srv, env)
			line := fmt.Sprintf("ok %d tools %s", len(tools), time.Since(start).Round(time.Second))
			switch {
			case err == nil:
			case strings.Contains(err.Error(), "does not run"):
				line = "BROKEN"
			default:
				line = "keys? " + err.Error()
			}
			mu.Lock()
			report[e.ID] = line
			mu.Unlock()
		}(e)
	}
	wg.Wait()
	ids := make([]string, 0, len(report))
	for id := range report {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t.Logf("%-28s %s", id, report[id])
		if report[id] == "BROKEN" {
			t.Errorf("%s: the package does not run", id)
		}
	}
}
