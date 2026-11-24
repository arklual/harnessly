package proxy

import (
	"strings"
	"testing"
)

func TestGuestTranscriptEmitsOnlyInertMarkup(t *testing.T) {
	input := `<html><head><style>discarded</style></head><body><article id=x><h2>Assistant</h2><p>Read <a href="/api/me">this</a></p><script>discarded</script><img src="/private"><iframe src="/machine"></iframe><pre onclick="discarded">&lt;script&gt;</pre></article></body></html>`
	var out strings.Builder
	writeGuestTranscript(&out, strings.NewReader(input))
	for _, bad := range []string{"discarded", "/api/", "/private", "/machine", "<iframe", "<img", "<script", "onclick", "href="} {
		if strings.Contains(out.String(), bad) {
			t.Fatalf("emitted %q", bad)
		}
	}
	for _, good := range []string{"<h2>Assistant</h2>", "<p>Read this</p>", "<pre>&lt;script&gt;</pre>"} {
		if !strings.Contains(out.String(), good) {
			t.Fatalf("missing %q", good)
		}
	}
}
