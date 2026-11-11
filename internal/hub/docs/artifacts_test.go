package docs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The documents service names a version as an object; reading a document's Markdown must not fail on it.
func TestMarkdownReadsTheVersionObject(t *testing.T) {
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/artifacts/plan/content" || r.URL.Query().Get("format") != "json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"plan","version":{"n":2,"id":"v2"},"html":"","markdown":"# Plan"}`))
	}))
	t.Cleanup(art.Close)
	md, n, ok, err := newArtifacts(art.URL, "t", art.Client()).markdown(context.Background(), "plan", "")
	if err != nil || md != "# Plan" || n != 2 || !ok {
		t.Fatalf("markdown: %q %d %v %v", md, n, ok, err)
	}
}
