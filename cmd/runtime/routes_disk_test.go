package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every path the Disk tab calls (diskPaths, which tools/web-routes-check.mjs reads) reaches the disk routes.
func TestDiskRoutesTakeEveryDiskPath(t *testing.T) {
	t.Setenv("RUNTIME_DISK_ROOTS", "Main="+t.TempDir())
	routes := diskBodyRoutes(deps{DataDir: t.TempDir()})
	if len(routes) == 0 {
		t.Fatal("no disk routes with RUNTIME_DISK_ROOTS set")
	}
	for _, p := range diskPaths {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			r := httptest.NewRequest(method, p, nil)
			taken := 0
			for _, route := range routes {
				if route.Match(r) {
					taken++
				}
			}
			if taken != 1 {
				t.Errorf("%s %s is taken by %d disk routes, want 1", method, p, taken)
			}
		}
	}
}
