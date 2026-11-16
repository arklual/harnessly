package api_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// a project in the hub's store is read for a guest from the store, not from the author's machine. The author
// makes a live link and a pinned one while the machine is on; the machine goes off; both open, the live one follows
// the store's new revision, the pinned one stays where it was, the version and the events come from the hub, and the
// machine is asked nothing.
func TestDesignGuestLinksReadTheStoreWithTheMachineOff(t *testing.T) {
	guestLinksWithTheMachineOff(t, "off")
}

// The same with HUB_DESIGN_DERIVED: the answers kept in the store are given with the same validators and
// the same caching as the ones worked out from a folder.
func TestDesignGuestLinksReadTheKeptAnswersWithTheMachineOff(t *testing.T) {
	guestLinksWithTheMachineOff(t, "on")
}

func guestLinksWithTheMachineOff(t *testing.T, derived string) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost), hubtest.Env("HUB_DESIGN_DERIVED", derived))
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Studio")
	a, _ := designMachine(t, s, owner, space)
	created(a, "shop", "owner@example.com")
	s.Eventually("the project recorded", func() bool { r, ok := snapshotFor(a, "owner@example.com", "shop"); return ok && r == "owner" })

	ctx := context.Background()
	room, err := s.Hub.Store.DesignRoom(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	put := func(path, body string) int64 {
		t.Helper()
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		if err := room.PutBlob(ctx, sha, bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
		var base int64
		if cur, err := room.Current(ctx, path); err == nil && cur != nil {
			base = cur.Rev
		}
		e, conflict, err := room.Commit(ctx, filestore.CommitRequest{Path: path, Base: base, Sha: sha, Size: int64(len(body)), Author: "owner"}, "vm-owner")
		if err != nil || conflict {
			t.Fatalf("commit %s: %v %v", path, err, conflict)
		}
		return e.Rev
	}
	put("shop/artifact.json", `{"title":"Shop"}`)
	put("shop/project/Secret.dc.html", `<html><body>store secret</body></html>`)
	v1 := put("shop/project/Main.dc.html", `<html><body>store v1</body></html>`)

	base := "/m/" + a.MachineID + "/api/platform/design/access"
	link := func(body map[string]any) string {
		t.Helper()
		r := owner.Do("POST", base+"/shop/shares", body)
		if r.Code != 201 {
			t.Fatalf("create link: %s", r)
		}
		var l struct{ URL string }
		r.JSON(&l)
		return strings.TrimPrefix(l.URL, "http://"+shareHost)
	}
	live := link(map[string]any{"rights": "view", "pages": []string{"Main.dc.html"}})
	pinned := link(map[string]any{"rights": "view", "version": "pinned"})
	whole := link(map[string]any{"rights": "view", "view": map[string]any{"pdf": true}})

	// The author's machine goes off.
	a.Close()
	s.Eventually("the machine offline", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })
	tokens := len(a.Calls(hubproto.OpDesignPublicToken))

	guest := s.Browser()
	open := func(p string) hubtest.Resp { t.Helper(); return guest.DoAt(s.URL, shareHost, "GET", p, nil) }
	if r := open(live + "/index.json"); r.Code != 200 || !strings.Contains(string(r.Body), "Main.dc.html") || strings.Contains(string(r.Body), "Secret.dc.html") {
		t.Fatalf("live index with the machine off: %s", r)
	}
	if r := open(live + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "store v1") {
		t.Fatalf("live page with the machine off: %s", r)
	}
	if r := open(live + "/f/Secret.dc.html"); r.Code == 200 {
		t.Fatalf("a page outside the live link: %s", r)
	}
	if r := open(pinned + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "store v1") {
		t.Fatalf("pinned page with the machine off: %s", r)
	}
	// The deck's PDF a machine published reached the store: a guest downloads it with the machine off.
	put("shop/.media/published/deck.pdf", "%PDF-1.4 from the store")
	if r := open(whole + "/m/deck.pdf"); r.Code != 200 || !strings.Contains(string(r.Body), "from the store") {
		t.Fatalf("the deck's PDF with the machine off: %s", r)
	}

	// a file of a live link carries its version as the ETag, and a guest that holds it is not sent it again.
	put("shop/project/kit.css", "body{margin:0}")
	first := open(whole + "/f/kit.css")
	tag := first.Header.Get("ETag")
	if first.Code != 200 || tag == "" {
		t.Fatalf("a live link's file without an ETag: %s %v", first, first.Header)
	}
	if r := guest.DoAt(s.URL, shareHost, "GET", whole+"/f/kit.css", nil, "If-None-Match", tag); r.Code != http.StatusNotModified || r.Header.Get("ETag") != tag {
		t.Fatalf("a file the guest holds was sent again: %s %v", r, r.Header)
	}

	// a board's page names each of its files with its version (?tkv=…). Asked for under it, the file is kept
	// by the guest's browser for good; a new revision changes the address of the edited file only.
	put("shop/project/pic.png", "png v1")
	put("shop/project/Assets.dc.html", `<html><head><link rel="stylesheet" href="kit.css"></head><body><img src="pic.png"></body></html>`)
	versioned := regexp.MustCompile(`(?:href|src)="((kit\.css|pic\.png)\?tkv=[^"#]+)"`)
	assets := func() map[string]string {
		t.Helper()
		r := open(whole + "/f/Assets.dc.html")
		if r.Code != 200 {
			t.Fatalf("a board's page with the machine off: %s", r)
		}
		out := map[string]string{}
		for _, m := range versioned.FindAllStringSubmatch(string(r.Body), -1) {
			out[m[2]] = m[1]
		}
		if len(out) != 2 {
			t.Fatalf("the page does not name its files' versions: %s", r.Body)
		}
		return out
	}
	before := assets()
	for _, f := range before {
		if r := open(whole + "/f/" + f); r.Code != 200 || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
			t.Fatalf("a file under its version is not kept for good: %s %v", r, r.Header)
		}
	}
	for _, f := range []string{"kit.css", "kit.css?tkv=stale"} {
		if r := open(whole + "/f/" + f); r.Code != 200 || strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
			t.Fatalf("%s is kept for good: %v", f, r.Header)
		}
	}
	put("shop/project/pic.png", "png v2, edited")
	after := assets()
	if after["kit.css"] != before["kit.css"] || after["pic.png"] == before["pic.png"] {
		t.Fatalf("a new revision moved the files: before %v, after %v", before, after)
	}

	// The project changes in the store (a teammate's machine, the editor through the hub): the live link follows.
	v2 := put("shop/project/Main.dc.html", `<html><body>store v2</body></html>`)
	if r := open(live + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "store v2") {
		t.Fatalf("live page after a new revision: %s", r)
	}
	if r := open(pinned + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "store v1") {
		t.Fatalf("the pinned page moved: %s", r)
	}
	var ver struct {
		Version int64
		Rev     string
		Pinned  bool
	}
	open(live + "/version").JSON(&ver)
	if ver.Version != v2 || ver.Rev != design.StoreRevID(v2) || ver.Pinned {
		t.Fatalf("live version %+v, want %d", ver, v2)
	}
	open(pinned + "/version").JSON(&ver)
	if !ver.Pinned || ver.Version < v1 || ver.Version >= v2 {
		t.Fatalf("pinned version %+v, want between %d and %d", ver, v1, v2)
	}

	// The live link's events: the revision now, then the next one as it lands.
	req, _ := http.NewRequest("GET", s.URL+live+"/events", nil)
	req.Host = shareHost
	ectx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := http.DefaultClient.Do(req.WithContext(ectx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events: %d %v", res.StatusCode, res.Header)
	}
	lines := bufio.NewScanner(res.Body)
	next := func() string {
		for lines.Scan() {
			if v, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				return v
			}
		}
		return ""
	}
	if got := next(); got != itoa(v2) {
		t.Fatalf("first event %q, want %d", got, v2)
	}
	v3 := put("shop/project/Main.dc.html", `<html><body>store v3</body></html>`)
	if got := next(); got != itoa(v3) {
		t.Fatalf("next event %q, want %d", got, v3)
	}
	if r := open(pinned + "/events"); r.Code != 204 {
		t.Fatalf("a pinned link's events: %s", r)
	}

	if n := len(a.Calls(hubproto.OpDesignPublicToken)); n != tokens {
		t.Fatalf("the author's machine was asked %d times while a guest read the store", n-tokens)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// the team reads and edits a project of the store through the hub with the author's machine off: the role is
// the hub's, a write on a stale revision is refused with the file's revision now, a viewer and a non-owner touching
// the owner file are refused, and the edit is the project's next revision, under the person who made it.
func TestDesignTeamEditsTheStoreThroughTheHubWithTheMachineOff(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost))
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}
	owner := s.SignIn("owner@example.com")
	mate := s.SignIn("mate@example.com")
	space, _ := owner.NewSpace("Studio")
	owner.Join(space, mate)
	a, _ := designMachine(t, s, owner, space)
	created(a, "shop", "owner@example.com")
	s.Eventually("the project recorded", func() bool { r, ok := snapshotFor(a, "owner@example.com", "shop"); return ok && r == "owner" })

	ctx := context.Background()
	room, err := s.Hub.Store.DesignRoom(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	put := func(path, body string) int64 {
		t.Helper()
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		if err := room.PutBlob(ctx, sha, bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
		e, conflict, err := room.Commit(ctx, filestore.CommitRequest{Path: path, Sha: sha, Size: int64(len(body)), Author: "owner"}, "vm-owner")
		if err != nil || conflict {
			t.Fatalf("commit %s: %v %v", path, err, conflict)
		}
		return e.Rev
	}
	v1 := put("shop/project/Main.dc.html", `<html><body>store v1</body></html>`)

	team := "/m/" + a.MachineID + "/api/platform/access/design/shop/team"
	if r := owner.Do("PUT", team, map[string]any{"role": "edit"}); r.Code != 200 {
		t.Fatalf("the team made editors: %s", r)
	}
	a.Close()
	s.Eventually("the machine offline", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })

	base := "/m/" + a.MachineID + "/api/platform/design/store/shop"
	var head struct{ Version int64 }
	if r := mate.Do("GET", base+"/head", nil); r.Code != 200 {
		t.Fatalf("head: %s", r)
	} else if r.JSON(&head); head.Version != v1 {
		t.Fatalf("head %d, want %d", head.Version, v1)
	}
	if r := mate.Do("GET", base+"/f/Main.dc.html", nil); r.Code != 200 || !strings.Contains(string(r.Body), "store v1") {
		t.Fatalf("the mate reads the board: %s", r)
	}
	if r := mate.Do("GET", base+"/f/Main.dc.html", nil); !strings.HasPrefix(r.Header.Get("Content-Security-Policy"), "sandbox") ||
		strings.Contains(r.Header.Get("Content-Security-Policy"), "allow-same-origin") || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("a board on the hub's origin is not sandboxed: %v", r.Header)
	}
	// A project the hub has no row for stays the machines': nothing of it through the hub.
	put("ghost/project/Main.dc.html", `<html>ghost</html>`)
	if r := mate.Do("PUT", "/m/"+a.MachineID+"/api/platform/design/store/ghost/file/project/x.html", "x", "X-Store-Base", "0"); r.Code != 404 {
		t.Fatalf("a write into an unrecorded project: %s", r)
	}

	file := base + "/file/project/Main.dc.html"
	var w struct{ Rev, Version int64 }
	r := mate.Do("PUT", file, `<html><body>mate's edit</body></html>`, "X-Store-Base", itoa(v1))
	if r.Code != 200 {
		t.Fatalf("the mate's edit: %s", r)
	}
	r.JSON(&w)
	if w.Rev <= v1 || w.Version != w.Rev {
		t.Fatalf("the edit's revision %+v after %d", w, v1)
	}
	if r := owner.Do("GET", base+"/f/Main.dc.html", nil); r.Code != 200 || !strings.Contains(string(r.Body), "mate's edit") {
		t.Fatalf("the owner reads the mate's edit: %s", r)
	}
	es, err := room.HistoryUnder(ctx, "shop/", 1)
	if err != nil || len(es) == 0 || es[0].Author != "mate@example.com" || es[0].Machine != "hub" {
		t.Fatalf("the revision's author: %+v %v", es, err)
	}

	// A write made on the revision before: refused, with the file's revision now.
	var c struct {
		Code string
		Rev  int64
	}
	r = owner.Do("PUT", file, `<html><body>stale</body></html>`, "X-Store-Base", itoa(v1))
	if r.Code != 409 {
		t.Fatalf("a stale write: %s", r)
	}
	if r.JSON(&c); c.Code != "conflict" || c.Rev != w.Rev {
		t.Fatalf("the conflict says %+v, want rev %d", c, w.Rev)
	}
	if r := mate.Do("PUT", file, "x", "X-Store-Base", "nope"); r.Code != 400 {
		t.Fatalf("a write without a base: %s", r)
	}
	if r := mate.Do("PUT", base+"/file/.owner.json", `{"person":"mate@example.com"}`, "X-Store-Base", "0"); r.Code != 403 {
		t.Fatalf("an editor writes the owner file: %s", r)
	}
	if r := mate.Do("PUT", base+"/file/..%2Fother%2Fx.html", "x", "X-Store-Base", "0"); r.Code < 400 {
		t.Fatalf("a write outside the project: %s", r)
	}

	// The team made viewers: the mate reads and may not write.
	if r := owner.Do("PUT", team, map[string]any{"role": "view"}); r.Code != 200 {
		t.Fatalf("the team made viewers: %s", r)
	}
	if r := mate.Do("PUT", file, "x", "X-Store-Base", itoa(w.Rev)); r.Code != 403 {
		t.Fatalf("a viewer writes: %s", r)
	}
	if r := mate.Do("GET", base+"/index.json", nil); r.Code != 200 || !strings.Contains(string(r.Body), "Main.dc.html") {
		t.Fatalf("a viewer reads the boards: %s", r)
	}
	// Closed to the team: the project is not there for the mate.
	if r := owner.Do("PUT", team, map[string]any{"role": nil}); r.Code != 200 {
		t.Fatalf("the project closed to the team: %s", r)
	}
	if r := mate.Do("GET", base+"/head", nil); r.Code != 404 {
		t.Fatalf("a closed project read: %s", r)
	}
}

// the deck editor saves through the hub with the machine off: the settings are a store revision under the
// person, the hub's read of the deck shows them, a change another machine made meanwhile is kept, and a viewer of the
// project may not save.
func TestDesignDeckEditsSaveThroughTheHubWithTheMachineOff(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost))
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}
	owner := s.SignIn("owner@example.com")
	mate := s.SignIn("mate@example.com")
	space, _ := owner.NewSpace("Studio")
	owner.Join(space, mate)
	a, _ := designMachine(t, s, owner, space)
	created(a, "shop", "owner@example.com")
	s.Eventually("the project recorded", func() bool { r, ok := snapshotFor(a, "owner@example.com", "shop"); return ok && r == "owner" })

	ctx := context.Background()
	room, err := s.Hub.Store.DesignRoom(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	put := func(path, body string) int64 {
		t.Helper()
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		if err := room.PutBlob(ctx, sha, bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
		var base int64
		if cur, err := room.Current(ctx, path); err == nil && cur != nil {
			base = cur.Rev
		}
		e, conflict, err := room.Commit(ctx, filestore.CommitRequest{Path: path, Base: base, Sha: sha, Size: int64(len(body)), Author: "owner"}, "vm-owner")
		if err != nil || conflict {
			t.Fatalf("commit %s: %v %v", path, err, conflict)
		}
		return e.Rev
	}
	put("shop/project/Main.dc.html", `<html><body>slide</body></html>`)
	if r := owner.Do("PUT", "/m/"+a.MachineID+"/api/platform/access/design/shop/team", map[string]any{"role": "edit"}); r.Code != 200 {
		t.Fatalf("the team made editors: %s", r)
	}
	a.Close()
	s.Eventually("the machine offline", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })

	deck := "/m/" + a.MachineID + "/api/platform/design/deck"
	before, _ := room.HistoryUnder(ctx, "shop/", 1)
	r := mate.Do("PUT", deck+"/settings", map[string]any{"artifact": "shop", "settings": map[string]any{"script": true}})
	if r.Code != 200 || !strings.Contains(string(r.Body), `"script":true`) {
		t.Fatalf("the settings saved through the hub: %s", r)
	}
	after, _ := room.HistoryUnder(ctx, "shop/", 1)
	if len(after) == 0 || after[0].Rev <= before[0].Rev || after[0].Author != "mate@example.com" || after[0].Machine != "hub" {
		t.Fatalf("the edit's revision: %+v", after)
	}
	if r := owner.Do("GET", deck+"?artifact=shop", nil); r.Code != 200 || !strings.Contains(string(r.Body), `"script":true`) {
		t.Fatalf("the hub's read of the deck: %s", r)
	}

	// Another machine changes a board meanwhile; the next save keeps it.
	put("shop/project/Main.dc.html", `<html><body>from a machine</body></html>`)
	if r := mate.Do("PUT", deck+"/settings", map[string]any{"artifact": "shop", "settings": map[string]any{"script": false}}); r.Code != 200 {
		t.Fatalf("the second save: %s", r)
	}
	if cur, err := room.Current(ctx, "shop/project/Main.dc.html"); err != nil || cur.Author != "owner" {
		t.Fatalf("the board another machine wrote was overwritten: %+v %v", cur, err)
	}

	if r := owner.Do("PUT", "/m/"+a.MachineID+"/api/platform/access/design/shop/team", map[string]any{"role": "view"}); r.Code != 200 {
		t.Fatalf("the team made viewers: %s", r)
	}
	if r := mate.Do("PUT", deck+"/settings", map[string]any{"artifact": "shop", "settings": map[string]any{"script": true}}); r.Code != 403 {
		t.Fatalf("a viewer saves: %s", r)
	}
}

// HRN-1321, HRN-1318: a link revoked while the author's machine is off ends at the guest's next request, and the ETag
// the guest holds does not get them a 304 of a link that is gone.
func TestDesignGuestLinkRevokedWithTheMachineOffEndsAtOnce(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost))
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Studio")
	a, _ := designMachine(t, s, owner, space)
	created(a, "shop", "owner@example.com")
	s.Eventually("the project recorded", func() bool { r, ok := snapshotFor(a, "owner@example.com", "shop"); return ok && r == "owner" })
	ctx := context.Background()
	room, err := s.Hub.Store.DesignRoom(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"shop/artifact.json":        `{"title":"Shop"}`,
		"shop/project/Main.dc.html": `<html><body>store v1</body></html>`,
		"shop/project/kit.css":      "body{margin:0}",
	} {
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		if err := room.PutBlob(ctx, sha, bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
		if _, conflict, err := room.Commit(ctx, filestore.CommitRequest{Path: path, Sha: sha, Size: int64(len(body)), Author: "owner"}, "vm-owner"); err != nil || conflict {
			t.Fatalf("commit %s: %v %v", path, err, conflict)
		}
	}
	base := "/m/" + a.MachineID + "/api/platform/design/access"
	r := owner.Do("POST", base+"/shop/shares", map[string]any{"rights": "view"})
	if r.Code != 201 {
		t.Fatalf("create link: %s", r)
	}
	var l struct{ ID, URL string }
	r.JSON(&l)
	link := strings.TrimPrefix(l.URL, "http://"+shareHost)

	a.Close()
	s.Eventually("the machine offline", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })

	guest := s.Browser()
	first := guest.DoAt(s.URL, shareHost, "GET", link+"/f/kit.css", nil)
	tag := first.Header.Get("ETag")
	if first.Code != 200 || tag == "" {
		t.Fatalf("the link with the machine off: %s %v", first, first.Header)
	}
	// The owner revokes the link; their machine is still off.
	if r := owner.Do("DELETE", base+"/shop/shares/"+l.ID, nil); r.Code >= 300 {
		t.Fatalf("revoke with the machine off: %s", r)
	}
	for _, p := range []string{"", "/index.json", "/f/kit.css", "/f/Main.dc.html"} {
		if r := guest.DoAt(s.URL, shareHost, "GET", link+p, nil, "If-None-Match", tag); r.Code < 400 {
			t.Fatalf("a revoked link still answers %q: %s", p, r)
		}
	}
}
