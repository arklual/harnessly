package api

import (
	"strings"
	"testing"
)

func TestWithFramesAddsOnlyTheNamedOrigins(t *testing.T) {
	csp := contentSecurityPolicy([]byte("<html></html>"))
	if got := withFrames(csp, nil); got != csp || strings.Contains(got, "frame-src") {
		t.Errorf("no origins: %q", got)
	}
	got := withFrames(csp, []string{"https://views.example.com"})
	if !strings.HasSuffix(got, "; frame-src https://views.example.com") || !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("one origin: %q", got)
	}
}
