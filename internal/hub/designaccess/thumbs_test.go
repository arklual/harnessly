package designaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// thumbLibrary is a coverLibrary that also keeps board pictures.
type thumbLibrary struct {
	coverLibrary
	thumbs map[string][]byte
}

func (l *thumbLibrary) Thumb(_ context.Context, _, project, board string, _ int64) ([]byte, string, error) {
	b := l.thumbs[project+"/"+board]
	if b == nil {
		return nil, "", nil
	}
	return b, strings.Repeat("b", 64), nil
}

// while the machine is off, the picture of a board comes from the store under the library's access; with the
// switch off the machine is asked, as before.
func TestBoardPictureFromTheStoreKeepsAccess(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "secret", r.ownerU.Email); err != nil || !done {
		t.Fatal(done, err)
	}
	if err := r.svc.write(ctx, r.owner, func(t *txn) error { return t.setTeam("secret", r.ownerU.Email, levelNone) }); err != nil {
		t.Fatal(err)
	}
	lib := &thumbLibrary{coverLibrary: coverLibrary{fakeLibrary: fakeLibrary{items: []LibraryItem{{ID: "secret", Title: "Личное", UpdatedAt: 1, Kind: "site", Rev: 1}}}},
		thumbs: map[string][]byte{"secret/Home.dc.html": []byte("\xff\xd8home")}}
	r.svc.UseView(lib)
	ask := func(u store.User, m store.Machine, query, etag string) (*httptest.ResponseRecorder, bool) {
		req := httptest.NewRequest(http.MethodGet, "/m/"+m.ID+"/api/platform/design/library/thumb?"+query, nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		return w, r.svc.ServeMachine(w, req, u, m)
	}
	if _, served := ask(r.ownerU, r.owner, "artifact=secret&board=Home.dc.html&v=1", ""); served {
		t.Fatal("served with the switch off")
	}
	r.svc.ownerOn = func(context.Context, string, string) bool { return true }
	w, served := ask(r.ownerU, r.owner, "artifact=secret&board=Home.dc.html&v=1", "")
	if !served || w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || w.Body.String() != "\xff\xd8home" ||
		w.Header().Get("Cache-Control") != "private, no-cache" || w.Header().Get("ETag") == "" {
		t.Fatalf("the owner: %v %d %v", served, w.Code, w.Header())
	}
	if w2, _ := ask(r.ownerU, r.owner, "artifact=secret&board=Home.dc.html&v=2", w.Header().Get("ETag")); w2.Code != http.StatusNotModified || w2.Body.Len() != 0 {
		t.Fatalf("again with the tag: %d", w2.Code)
	}
	for _, q := range []string{"artifact=secret&board=About.dc.html", "artifact=secret", "artifact=Bad&board=Home.dc.html"} {
		if w, served := ask(r.ownerU, r.owner, q, ""); !served || w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %v %d", q, served, w.Code)
		}
	}
	if w, served := ask(r.strangeU, r.stranger, "artifact=secret&board=Home.dc.html", ""); !served || w.Code != http.StatusNotFound || w.Body.Len() != 0 {
		t.Fatalf("a stranger: %v %d %q", served, w.Code, w.Body.String())
	}
}
