package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const shareHost = "share.example.test"

// designRuntime is the runtime's own design slice (internal/design) behind a machine of the stack: the access snapshot,
// the guest token it signs and the public read of a project go through the same code the runtime runs.
type designRuntime struct {
	api  *design.API
	root string
}

func newDesignRuntime(t *testing.T) *designRuntime {
	t.Helper()
	root := t.TempDir()
	for _, f := range []struct{ path, body string }{
		{"shop/project/Main.dc.html", `<html><body>shop main</body></html>`},
		{"shop/project/Secret.dc.html", `<html><body>shop secret</body></html>`},
		{"shop/artifact.json", `{"title":"Shop"}`},
		{"other/project/Main.dc.html", `<html><body>someone else's</body></html>`},
		{"other/artifact.json", `{"title":"Other"}`},
	} {
		p := filepath.Join(root, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := design.New(design.Config{LibraryDir: root, ReleasesDir: t.TempDir(), Texts: design.Texts{Tag: "harnessly-design", CLI: "harnessly"}})
	t.Cleanup(l.StopWatching)
	a := &design.API{
		L: l, PlatformAPI: "/api/platform", MachineAgent: func(*http.Request) bool { return true },
		PublicToken: design.PublicConfig{Secret: []byte("pair-secret"), Salt: "harnessly", Audience: "harnessly-design-public", Header: "x-harnessly-design-token"},
	}
	return &designRuntime{api: a, root: root}
}

// handler is the runtime as the hub's tunnel reaches it: the gate takes the machine's base path off.
func (d *designRuntime) handler(machine *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/m/" + *machine
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, base)
		r2.URL.RawPath = ""
		r2.RequestURI = strings.TrimPrefix(r.RequestURI, base)
		if d.api.PublicMatch(r2) {
			d.api.Public(w, r2)
			return
		}
		http.Error(w, "not a design route", http.StatusNotFound)
	})
}

// call runs one of the runtime's agent-only routes with an operation's body, as the agent relays it.
func (d *designRuntime) call(serve func(*http.Request) design.Answer, route string, params map[string]json.RawMessage) (any, error) {
	raw, _ := json.Marshal(params)
	r := httptest.NewRequest(http.MethodPost, "/api/platform"+route, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	status, body, _ := serve(r).Split()
	var b []byte
	switch v := body.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		b, _ = json.Marshal(v)
	}
	if status != http.StatusOK {
		return nil, hubtest.Fail(hubproto.CodeFailed, string(b))
	}
	return json.RawMessage(b), nil
}

// designMachine registers and connects a machine that serves design access, with the real design slice behind it.
func designMachine(t *testing.T, s *hubtest.Stack, p *hubtest.Person, space string) (*hubtest.Agent, *designRuntime) {
	t.Helper()
	d := newDesignRuntime(t)
	var id string
	a := s.NewAgent(d.handler(&id))
	a.Hello.Features = append(append([]string{}, a.Hello.Features...), hubproto.FeatureDesignAccess)
	a.Handle(hubproto.OpDesignAccess, func(c hubtest.Call) (any, error) { return d.call(d.api.PostAccess, "/design/access", c.Params) })
	a.Handle(hubproto.OpDesignPublicToken, func(c hubtest.Call) (any, error) {
		return d.call(d.api.GuestToken, "/design/guest-token", c.Params)
	})
	if r := a.Register(p.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	id = a.MachineID
	a.Connect()
	return a, d
}

func created(a *hubtest.Agent, project, person string) {
	a.Emit(map[string]any{"event": hubproto.EventDesign, "design": hubproto.DesignEvent{Kind: "design.created", Project: project, Person: person}})
}

// snapshotFor is the last snapshot the machine took, as the person sees it.
func snapshotFor(a *hubtest.Agent, person, project string) (string, bool) {
	calls := a.Calls(hubproto.OpDesignAccess)
	if len(calls) == 0 {
		return "", false
	}
	var people map[string]struct {
		Projects map[string]struct{ Role string }
	}
	if err := json.Unmarshal(calls[len(calls)-1].Params["people"], &people); err != nil {
		return "", false
	}
	g, ok := people[person].Projects[project]
	return g.Role, ok
}

// The share links of a design, end to end: the owner makes a link through the machine's page API, a guest opens the
// project on the share host and only that project, a stranger is refused, the link is revoked, and everything holds
// across a restart of the hub.
func TestDesignShareLinksThroughHubAndRuntime(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost))
	owner := s.SignIn("owner@example.com")
	member := s.SignIn("member@example.com")
	stranger := s.SignIn("stranger@example.com")
	space, _ := owner.NewSpace("Studio")
	owner.Join(space, member)
	a, _ := designMachine(t, s, owner, space)
	base := "/m/" + a.MachineID + "/api/platform/design/access"

	// The runtime reports the projects; the hub records their makers and pushes the snapshot.
	created(a, "shop", "owner@example.com")
	created(a, "other", "member@example.com")
	s.Eventually("both projects recorded", func() bool {
		r1, ok1 := snapshotFor(a, "owner@example.com", "shop")
		r2, ok2 := snapshotFor(a, "member@example.com", "other")
		return ok1 && ok2 && r1 == "owner" && r2 == "owner"
	})

	// The owner runs the access of their project.
	access := func(p *hubtest.Person) (string, bool) {
		t.Helper()
		r := p.Do("GET", base+"/shop", nil)
		if r.Code != 200 {
			t.Fatalf("read access: %s", r)
		}
		var v struct {
			Role      *string
			CanManage bool
		}
		r.JSON(&v)
		if v.Role == nil {
			return "", v.CanManage
		}
		return *v.Role, v.CanManage
	}
	if role, manage := access(owner); role != "owner" || !manage {
		t.Fatalf("owner's view: %q %v", role, manage)
	}
	// A team project is open to the space and its editors share it, as on the platform; the owner closes it.
	if role, _ := access(member); role != "edit" {
		t.Fatalf("a member of a team project: %q", role)
	}
	if r := member.Do("PUT", base+"/shop/team", map[string]any{"level": "none"}); r.Code != 403 {
		t.Fatalf("an editor closes someone else's project: %s", r)
	}
	if r := owner.Do("PUT", base+"/shop/team", map[string]any{"level": "none"}); r.Code != 200 {
		t.Fatalf("owner closes the project: %s", r)
	}
	if role, manage := access(member); role != "" || manage {
		t.Fatalf("a member of a closed project: %q %v", role, manage)
	}
	s.Eventually("the closed project in the snapshot", func() bool {
		role, ok := snapshotFor(a, "member@example.com", "shop")
		return !ok || role == "" || role == "none"
	})
	if r := member.Do("POST", base+"/shop/shares", map[string]any{"rights": "view"}); r.Code != 403 {
		t.Fatalf("member makes a link to someone else's project: %s", r)
	}
	if r := stranger.Do("GET", base+"/shop", nil); r.Code < 400 {
		t.Fatalf("a stranger reads the access of a machine they may not open: %s", r)
	}
	if r := stranger.Do("POST", base+"/shop/shares", map[string]any{"rights": "view"}); r.Code < 400 {
		t.Fatalf("a stranger makes a link: %s", r)
	}

	// A link to one page of the owner's project.
	r := owner.Do("POST", base+"/shop/shares", map[string]any{"rights": "view", "title": "For the client", "pages": []string{"Main.dc.html"}})
	if r.Code != 201 {
		t.Fatalf("create link: %s", r)
	}
	var link struct {
		ID  string
		URL string
	}
	r.JSON(&link)
	if !strings.HasPrefix(link.URL, "http://"+shareHost+"/s/") || link.ID == "" {
		t.Fatalf("link: %s", r)
	}
	path := strings.TrimPrefix(link.URL, "http://"+shareHost)
	var list []map[string]any
	owner.Do("GET", base+"/shop/shares", nil).JSON(&list)
	if len(list) != 1 || list[0]["id"] != link.ID || list[0]["url"] != nil {
		t.Fatalf("links: %+v", list)
	}

	// A guest, signed in nowhere, opens the project on the share host: its page, nothing else of it, nothing of the hub.
	guest := s.Browser()
	open := func(p string) hubtest.Resp { t.Helper(); return guest.DoAt(s.URL, shareHost, "GET", p, nil) }
	if r := open(path + "/index.json"); r.Code != 200 || !strings.Contains(string(r.Body), "Main.dc.html") || strings.Contains(string(r.Body), "Secret.dc.html") {
		t.Fatalf("guest index: %s", r)
	}
	if r := open(path + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "shop main") {
		t.Fatalf("guest page: %s", r)
	}
	if r := open(path + "/f/Secret.dc.html"); r.Code == 200 {
		t.Fatalf("a page outside the link: %s", r)
	}
	if r := open(path + "/f/../../other/project/Main.dc.html"); r.Code == 200 || strings.Contains(string(r.Body), "someone else's") {
		t.Fatalf("another project through the link: %s", r)
	}
	if r := open("/api/me"); r.Code == 200 {
		t.Fatalf("the hub's API on the share host: %s", r)
	}
	if r := open("/m/" + a.MachineID + "/"); r.Code == 200 {
		t.Fatalf("the machine on the share host: %s", r)
	}
	if r := guest.Do("GET", path+"/index.json", nil); strings.Contains(string(r.Body), "boards") {
		t.Fatalf("a guest link on the hub's own origin: %s", r)
	}
	if r := open("/s/not-a-token/index.json"); r.Code != 404 {
		t.Fatalf("an unknown link: %s", r)
	}
	if n := len(a.Calls(hubproto.OpDesignPublicToken)); n == 0 {
		t.Fatal("the hub signed the guest's read itself: the runtime was never asked for a token")
	}
	for _, c := range a.Calls(hubproto.OpDesignPublicToken) {
		if c.Str("project") != "shop" {
			t.Fatalf("a token asked for %q", c.Str("project"))
		}
	}
	// The runtime's token lasts a minute (expiresAt in milliseconds): past it the hub asks for a fresh one instead of
	// carrying the dead token, which the runtime refuses and the guest sees as «the owner's machine is unavailable».
	asked := len(a.Calls(hubproto.OpDesignPublicToken))
	s.Clock.Advance(61 * time.Second)
	if r := open(path + "/f/Main.dc.html"); r.Code != 200 {
		t.Fatalf("guest page a minute later: %s", r)
	}
	if n := len(a.Calls(hubproto.OpDesignPublicToken)); n <= asked {
		t.Fatal("the hub kept a guest token past its expiry")
	}

	// A second link, then a restart of the hub: the links, the records and the snapshot come back from the database.
	r = owner.Do("POST", base+"/shop/shares", map[string]any{"rights": "view"})
	if r.Code != 201 {
		t.Fatalf("second link: %s", r)
	}
	var kept struct{ ID, URL string }
	r.JSON(&kept)
	keptPath := strings.TrimPrefix(kept.URL, "http://"+shareHost)

	s.Restart()
	owner, member = s.SignIn("owner@example.com"), s.SignIn("member@example.com")
	if r := open(path + "/index.json"); r.Code == 200 {
		t.Fatalf("a guest read while the machine is offline: %s", r)
	}
	before := len(a.Calls(hubproto.OpDesignAccess))
	a.Connect()
	s.Eventually("the snapshot pushed after the restart", func() bool { return len(a.Calls(hubproto.OpDesignAccess)) > before })
	if role, ok := snapshotFor(a, "owner@example.com", "shop"); !ok || role != "owner" {
		t.Fatalf("owner after restart: %q %v", role, ok)
	}
	if _, ok := snapshotFor(a, "stranger@example.com", "shop"); ok {
		t.Fatal("a stranger in the snapshot after restart")
	}
	if r := open(path + "/f/Main.dc.html"); r.Code != 200 || !strings.Contains(string(r.Body), "shop main") {
		t.Fatalf("guest page after restart: %s", r)
	}
	if r := member.Do("POST", base+"/shop/shares", map[string]any{"rights": "view"}); r.Code != 403 {
		t.Fatalf("member makes a link after restart: %s", r)
	}

	// Revoking: the link answers gone at once, the other link still opens, a revoked link is not brought back.
	if r := member.Do("DELETE", base+"/shop/shares/"+link.ID, nil); r.Code < 400 {
		t.Fatalf("a member revokes the owner's link: %s", r)
	}
	if r := owner.Do("DELETE", base+"/shop/shares/"+link.ID, nil); r.Code >= 300 {
		t.Fatalf("revoke: %s", r)
	}
	if r := open(path + "/index.json"); r.Code != http.StatusGone {
		t.Fatalf("a revoked link: %s", r)
	}
	if r := open(path + "/f/Main.dc.html"); r.Code != http.StatusGone || strings.Contains(string(r.Body), "shop main") {
		t.Fatalf("a revoked link's page: %s", r)
	}
	if r := open(keptPath + "/f/Main.dc.html"); r.Code != 200 {
		t.Fatalf("the other link after a revoke: %s", r)
	}
	s.Restart()
	a.Connect()
	if r := open(path + "/index.json"); r.Code != http.StatusGone {
		t.Fatalf("a revoked link after restart: %s", r)
	}
	if strings.Contains(s.Logs.String(), "level=ERROR") {
		t.Errorf("errors in the log:\n%s", s.Logs.String())
	}
}
