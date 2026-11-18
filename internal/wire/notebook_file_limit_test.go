package wire_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type notebookSaveSource struct {
	wire.FilesSource
	calls int
}

func (s *notebookSaveSource) Save(_ context.Context, _, path, content, _ string) (wire.FileContent, error) {
	s.calls++
	return wire.FileContent{Path: path, Content: content, Revision: "saved", Bytes: len(content)}, nil
}

func TestNotebookFileSaveEnvelopeLimitIsScoped(t *testing.T) {
	g := newRig(t, time.Hour)
	source := &notebookSaveSource{FilesSource: g.fake.Files()}
	g.srv.Files = source
	// JSON escapes each byte sixfold: decoded text just over 1 MB exceeds the old envelope cap.
	content := strings.Repeat("\x01", 1_100_000)
	body := wire.FileSave{Content: content, Revision: "before"}
	url := "/api/v2/conversations/tab-k3Jx9QmZ/file?path="
	if code := g.do(http.MethodPut, url+"report.IPYNB", body, nil); code != http.StatusOK || source.calls != 1 {
		t.Fatalf("notebook save: status=%d calls=%d", code, source.calls)
	}
	if code := g.do(http.MethodPut, url+"report.txt", body, nil); code != http.StatusBadRequest || source.calls != 1 {
		t.Fatalf("ordinary file envelope limit raised: status=%d calls=%d", code, source.calls)
	}
}
