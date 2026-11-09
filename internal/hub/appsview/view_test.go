package appsview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// rooms is a hub's store with one space.
type rooms struct {
	srv   *filestore.Server
	space string
}

func (r rooms) AppsRoom(ctx context.Context, spaceID string) (*filestore.Store, error) {
	return r.srv.Room(ctx, "apps-"+spaceID)
}

func (r rooms) AppsSpaces(context.Context) ([]string, error) { return []string{r.space}, nil }

// a guest link opens from the hub's store with the machine that made it gone: the page, the view's file and
// the link's own storage; a revoked link ends there too.
func TestGuestLinkOpensFromTheStoreWithTheMachineOff(t *testing.T) {
	srv, err := filestore.OpenFolder(t.TempDir(), func(r *http.Request, coll string) (filestore.Access, error) {
		return filestore.Access{Member: "machine", Room: "apps-sp1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	client := filesync.NewClient("http://store.invalid", filestore.KindApps, nil, &http.Client{Transport: storelink.Handler(srv.Handler())})

	// The machine: an app with a view for the app target, a writable link to it, put into the store.
	pkg := filepath.Join(t.TempDir(), "pkg")
	if err := apps.Scaffold(pkg, "acme.demo", "Demo", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "ui", "app.js"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	reg := &apps.Registry{Root: filepath.Join(data, "apps")}
	if _, _, err := reg.Install(pkg, apps.InstallOptions{HostVersion: apps.TemplateVersion}); err != nil {
		t.Fatal(err)
	}
	storage := &apps.Storage{Root: filepath.Join(data, "storage")}
	machine := apps.NewGuests(reg, storage)
	token, link, err := machine.Create("acme.demo", "", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := machine.PublishGuests(ctx, client, "p-1", nil); err != nil {
		t.Fatal(err)
	}

	// The machine is gone: its folders with it.
	if err := os.RemoveAll(data); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(pkg); err != nil {
		t.Fatal(err)
	}
	v := New(Options{Rooms: rooms{srv: srv, space: "sp1"}, Dir: t.TempDir()})
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		v.ServeHTTP(w, req)
		return w
	}
	base := apps.GuestPrefix + token + "/"
	if w := do(http.MethodGet, base, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `src="acme.demo/ui/index.html"`) {
		t.Fatalf("page: %d %s", w.Code, w.Body.String())
	}
	// The app's own bytes run on an opaque origin even when opened directly, never on the hub's (the app's CSP is
	// a sandbox without allow-same-origin).
	for _, f := range []string{"acme.demo/ui/app.js", "acme.demo/ui/index.html"} {
		w := do(http.MethodGet, base+f, "")
		csp := w.Header().Get("Content-Security-Policy")
		if w.Code != 200 || !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("the view's file %s: %d csp=%q", f, w.Code, csp)
		}
	}
	if w := do(http.MethodGet, base+"acme.demo/ui/app.js", ""); !strings.Contains(w.Body.String(), "export {}") {
		t.Fatalf("the view's file: %s", w.Body.String())
	}
	if w := do(http.MethodPost, base+"__guest/storage", `{"op":"set","key":"k","value":1}`); w.Code != 200 {
		t.Fatalf("storage set: %d %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, base+"__guest/storage", `{"op":"get","key":"k"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `1`) {
		t.Fatalf("storage get: %d %s", w.Code, w.Body.String())
	}
	if e, err := client.Current(ctx, "storage/acme.demo/doc/guest-"+link.ID+".json"); err != nil || e == nil {
		t.Fatalf("the link's area is not in the store: %v %v", e, err)
	}
	if w := do(http.MethodGet, apps.GuestPrefix+"not-a-token/", ""); w.Code != http.StatusNotFound {
		t.Fatalf("a made-up token: %d", w.Code)
	}

	// Revoked on a machine (here: the store's file gone), the link ends at the hub too.
	e, _ := client.Current(ctx, apps.GuestPath("p-1", link.Hash))
	if _, err := client.Commit(ctx, filestore.CommitRequest{Path: e.Path, Base: e.Rev, Deleted: true}); err != nil {
		t.Fatal(err)
	}
	if w := do(http.MethodGet, base, ""); w.Code != http.StatusNotFound {
		t.Fatalf("a revoked link: %d", w.Code)
	}
}
