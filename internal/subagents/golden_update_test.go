package subagents

import (
	"os"
	"strconv"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// updatingGolden is HARNESSLY_UPDATE_GOLDEN=1: a golden test writes what this code answers into its golden file, in the
// file's own format, instead of comparing. Run it when the answers change on purpose, and read the diff.
func updatingGolden() bool { return os.Getenv("HARNESSLY_UPDATE_GOLDEN") != "" }

// writeGolden writes doc as the generator did: JSON.stringify(doc, null, 1) and a newline.
func writeGolden(t *testing.T, path string, doc any) {
	t.Helper()
	if err := os.WriteFile(path, append(jsvalue.Stringify(doc, " "), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// updateRequest puts an answer into its request of the golden file: the body, and the headers that follow from it. A
// body holding this machine's paths (scrubbed is not body) keeps its etag, which the test only checks against the body.
// The length moves by what the body gained.
func updateRequest(r *jsvalue.Object, oldAnswer, scrubbed string, body []byte, etag string) {
	r.Set("answer", scrubbed)
	h := r.Object("answerHeaders")
	if h == nil {
		return
	}
	if h.Has("etag") && scrubbed == string(body) {
		h.Set("etag", etag)
	}
	if n, err := strconv.Atoi(h.String("content-length")); err == nil {
		h.Set("content-length", strconv.Itoa(n+len(scrubbed)-len(oldAnswer)))
	}
}
