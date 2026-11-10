package appsview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// an app installed for a space opens at the hub for a member with every machine off: the page and the view's
// files from the package the team keeps in the store, the person's own area and the space's in the store; what needs
// a machine (a chat's area, the install's) is refused, and an app the space did not install is not there.
func TestASpaceAppOpensForAMemberFromTheStore(t *testing.T) {
	srv, err := filestore.OpenFolder(t.TempDir(), func(r *http.Request, coll string) (filestore.Access, error) {
		return filestore.Access{Member: "machine", Room: "apps-sp1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	client := filesync.NewClient("http://store.invalid", filestore.KindApps, nil, &http.Client{Transport: storelink.Handler(srv.Handler())})
	ctx := context.Background()

	// A machine of the space put the team there: the install and its package, then it went off.
	pkg := filepath.Join(t.TempDir(), "pkg")
	if err := apps.Scaffold(pkg, "acme.demo", "Demo", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "ui", "app.js"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, sum, err := apps.Pack(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(ctx, apps.TeamArchivePath("acme.demo", "0.1.0"), archive, "test"); err != nil {
		t.Fatal(err)
	}
	team, _ := json.Marshal(map[string]any{
		"space": "sp1", "at": 1,
		"catalog":  []map[string]any{{"id": "acme.demo", "name": "Demo", "version": "0.1.0", "targets": []string{"pane", "app"}, "sum": sum}},
		"installs": []map[string]any{{"id": "acme.demo", "version": "0.1.0", "sum": sum}},
	})
	if _, err := client.Put(ctx, apps.TeamStateFile, team, "test"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(pkg); err != nil {
		t.Fatal(err)
	}

	v := New(Options{Rooms: rooms{srv: srv, space: "sp1"}, Dir: t.TempDir()})
	list, err := v.SpaceApps(ctx, "sp1")
	if err != nil || len(list) != 1 || list[0].ID != "acme.demo" || list[0].Name != "Demo" || list[0].Path != "/apps/space/sp1/acme.demo/" {
		t.Fatalf("the space's apps: %+v %v", list, err)
	}
	do := func(method, path, body, site string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		v.ServeMember(w, req, "sp1", "p-ann")
		return w
	}
	base := "/apps/space/sp1/acme.demo/"
	if w := do(http.MethodGet, "/apps/space/sp1/acme.demo", "", ""); w.Code != http.StatusFound || w.Header().Get("Location") != "acme.demo/" {
		t.Fatalf("no slash: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do(http.MethodGet, base, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `src="acme.demo/ui/index.html"`) || !strings.Contains(w.Body.String(), "&#34;member&#34;:true") {
		t.Fatalf("page: %d %s", w.Code, w.Body.String())
	}
	w := do(http.MethodGet, base+"acme.demo/ui/app.js", "", "")
	if csp := w.Header().Get("Content-Security-Policy"); w.Code != 200 || !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(w.Body.String(), "export {}") {
		t.Fatalf("the view's file: %d csp=%q", w.Code, csp)
	}

	storage := base + "__guest/storage"
	if w := do(http.MethodPost, storage, `{"op":"set","scope":"user","key":"k","value":1}`, "same-origin"); w.Code != 200 {
		t.Fatalf("user set: %d %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, storage, `{"op":"get","scope":"user","key":"k"}`, "same-origin"); w.Code != 200 || !strings.Contains(w.Body.String(), `"value":1`) {
		t.Fatalf("user get: %d %s", w.Code, w.Body.String())
	}
	if e, err := client.Current(ctx, "storage/acme.demo/user/p-ann.json"); err != nil || e == nil {
		t.Fatalf("the person's area is not in the store: %v %v", e, err)
	}
	if w := do(http.MethodPost, storage, `{"op":"get","scope":"space","key":"k"}`, "same-origin"); w.Code != 200 {
		t.Fatalf("space get: %d %s", w.Code, w.Body.String())
	}
	// The app does not ask for storage.space.write: the bridge's own rule holds here too.
	if w := do(http.MethodPost, storage, `{"op":"set","scope":"space","key":"k","value":1}`, "same-origin"); w.Code != http.StatusForbidden {
		t.Fatalf("space set without the right: %d %s", w.Code, w.Body.String())
	}
	for _, scope := range []string{"chat", "doc", "install", ""} {
		if w := do(http.MethodPost, storage, `{"op":"get","scope":"`+scope+`","key":"k"}`, "same-origin"); w.Code != http.StatusForbidden {
			t.Fatalf("scope %q: %d", scope, w.Code)
		}
	}
	for _, site := range []string{"", "cross-site", "same-site"} {
		if w := do(http.MethodPost, storage, `{"op":"get","scope":"user","key":"k"}`, site); w.Code != http.StatusForbidden {
			t.Fatalf("a call from %q: %d", site, w.Code)
		}
	}

	// Another space in the path, and an app the space did not install, are not there.
	if w := do(http.MethodGet, "/apps/space/sp2/acme.demo/", "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("another space: %d", w.Code)
	}
	if w := do(http.MethodGet, "/apps/space/sp1/acme.other/", "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("an app not installed: %d", w.Code)
	}
}
