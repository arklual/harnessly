package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// The one access API (<platform api>/access/design/{id}, /access/me, /access/directory) on a real hub and a real
// runtime: the view, grants to people and teams, a request and its decision with the mails, links without sign-in with
// their kept addresses, the refusals, and the same after a restart of the hub.

type accessView struct {
	Stored    bool
	Role      *string
	CanManage bool
	Manager   bool
	Steward   bool
	Owner     *struct {
		Email *string
		Name  string
	}
	HomeTeam *struct {
		TeamID string
		Name   string
		Role   *string
	}
	EditorsCanShare bool
	Grants          []struct {
		Type, ID, Name, Sub, Role string
		CanShare                  bool
		ExpiresAt                 *string
	}
	Requests  []struct{ ID, Email, Level, Message string }
	Links     []accessLink
	MyRequest *struct{ ID, Level, Status string }
}

type accessLink struct {
	ID, ForWhom, Rights, CreatedBy string
	DisabledAt                     *string
	Opens                          int
	Copyable                       bool
}

func roleOf(v accessView) string {
	if v.Role == nil {
		return ""
	}
	return *v.Role
}

func TestDesignResourceAccessThroughHubAndRuntime(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SHARE_URL", "http://"+shareHost))
	owner := s.SignIn("owner@example.com")
	member := s.SignIn("member@example.com")
	stranger := s.SignIn("stranger@example.com")
	space, _ := owner.NewSpace("Studio")
	owner.Join(space, member)
	a, _ := designMachine(t, s, owner, space)
	api := "/m/" + a.MachineID + "/api/platform/access"
	base := api + "/design"

	created(a, "shop", "owner@example.com")
	s.Eventually("the project recorded", func() bool {
		role, ok := snapshotFor(a, "owner@example.com", "shop")
		return ok && role == "owner"
	})

	view := func(p *hubtest.Person, project string) accessView {
		t.Helper()
		r := p.Do("GET", base+"/"+project, nil)
		if r.Code != 200 {
			t.Fatalf("view of %s: %s", project, r)
		}
		var v accessView
		r.JSON(&v)
		return v
	}
	must := func(want int, r hubtest.Resp, what string) hubtest.Resp {
		t.Helper()
		if r.Code != want {
			t.Fatalf("%s: want %d, got %s", what, want, r)
		}
		return r
	}

	// Who the person is and whom they may add.
	var me struct {
		Teams []struct {
			TeamID string
			Lead   bool
		}
	}
	must(200, owner.Do("GET", api+"/me", nil), "me").JSON(&me)
	if len(me.Teams) != 1 || me.Teams[0].TeamID != "space:"+space || !me.Teams[0].Lead {
		t.Fatalf("owner's teams: %+v", me)
	}
	var dir struct {
		People []struct{ Email, Name string }
		Teams  []struct {
			TeamID  string
			Members int
		}
	}
	must(200, owner.Do("GET", api+"/directory?q=MEM", nil), "directory").JSON(&dir)
	if len(dir.People) != 1 || dir.People[0].Email != "member@example.com" {
		t.Fatalf("directory people: %+v", dir)
	}
	must(200, owner.Do("GET", api+"/directory?q=", nil), "directory").JSON(&dir)
	for _, p := range dir.People {
		if p.Email == "owner@example.com" || p.Email == "stranger@example.com" {
			t.Fatalf("directory lists %s", p.Email)
		}
	}
	if len(dir.Teams) != 1 || dir.Teams[0].TeamID != "space:"+space || dir.Teams[0].Members != 2 {
		t.Fatalf("directory teams: %+v", dir.Teams)
	}

	// A stranger gets nothing of a machine they may not open; another kind is left to the older dialog.
	for _, path := range []string{base + "/shop", api + "/me", api + "/directory?q=a"} {
		if r := stranger.Do("GET", path, nil); r.Code < 400 {
			t.Fatalf("a stranger reads %s: %s", path, r)
		}
	}
	if r := owner.Do("GET", api+"/doc/x", nil); r.Code == 200 {
		t.Fatalf("a kind the hub does not run: %s", r)
	}

	// The owner's view, then the project closed to the space.
	v := view(owner, "shop")
	if !v.Stored || roleOf(v) != "owner" || !v.CanManage || !v.Manager || v.HomeTeam == nil || v.HomeTeam.TeamID != "space:"+space ||
		v.Owner == nil || v.Owner.Email == nil || *v.Owner.Email != "owner@example.com" || v.Links == nil {
		t.Fatalf("owner's view: %+v", v)
	}
	must(200, owner.Do("PUT", base+"/shop/settings", map[string]any{"editorsCanShare": false}), "settings")
	if r := member.Do("PUT", base+"/shop/team", map[string]any{"role": nil}); r.Code != 403 {
		t.Fatalf("an editor closes someone else's project: %s", r)
	}
	must(200, owner.Do("PUT", base+"/shop/team", map[string]any{"role": nil}), "close")
	if v := view(owner, "shop"); v.HomeTeam == nil || v.HomeTeam.Role != nil {
		t.Fatalf("closed project's team: %+v", v.HomeTeam)
	}
	mv := view(member, "shop")
	if roleOf(mv) != "" || mv.CanManage || mv.Grants != nil || mv.Links != nil || mv.Owner == nil || mv.Owner.Email != nil {
		t.Fatalf("a member of a closed project sees: %+v", mv)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/log"}, {"GET", "/links"}, {"POST", "/links"}, {"PUT", "/grants/user/member@example.com"}, {"PUT", "/settings"},
	} {
		if r := member.Do(c.method, base+"/shop"+c.path, map[string]any{"role": "edit", "forWhom": "x", "rights": "view"}); r.Code != 403 {
			t.Fatalf("a member of a closed project: %s %s: %s", c.method, c.path, r)
		}
	}

	// A request for access, the mail to the owner, the decision, the mail back.
	if r := member.Do("POST", base+"/shop/requests", map[string]any{"level": "edit", "message": "Нужно поправить шапку"}); r.Code != 200 {
		t.Fatalf("request: %s", r)
	}
	if v := view(member, "shop"); v.MyRequest == nil || v.MyRequest.Level != "edit" || v.MyRequest.Status != "open" {
		t.Fatalf("my request: %+v", v.MyRequest)
	}
	s.Eventually("the owner mailed about the request", func() bool {
		for _, n := range s.Mail.Notices() {
			if n.To == "owner@example.com" && strings.Contains(n.Subject, "просит правку «shop»") && n.Body == "Нужно поправить шапку" &&
				strings.HasSuffix(n.Link, "/m/"+a.MachineID+"/designly/shop?access=requests") {
				return true
			}
		}
		return false
	})
	v = view(owner, "shop")
	if len(v.Requests) != 1 || v.Requests[0].Email != "member@example.com" {
		t.Fatalf("requests: %+v", v.Requests)
	}
	if r := owner.Do("POST", base+"/shop/requests/"+v.Requests[0].ID, map[string]any{"decision": "maybe"}); r.Code != 400 {
		t.Fatalf("a bad decision: %s", r)
	}
	must(200, owner.Do("POST", base+"/shop/requests/"+v.Requests[0].ID,
		map[string]any{"decision": "allow", "role": "edit", "canShare": false, "ttl": nil}), "allow")
	s.Eventually("the member mailed about the decision", func() bool {
		for _, n := range s.Mail.Notices() {
			if n.To == "member@example.com" && n.Subject == "Доступ выдан" {
				return true
			}
		}
		return false
	})
	for _, n := range s.Mail.Notices() {
		if n.To == "stranger@example.com" {
			t.Fatalf("a stranger mailed: %+v", n)
		}
	}
	mv = view(member, "shop")
	if roleOf(mv) != "edit" || mv.CanManage || mv.MyRequest != nil {
		t.Fatalf("member after allow: %+v", mv)
	}
	if r := member.Do("PUT", base+"/shop/grants/user/friend@example.com", map[string]any{"role": "view"}); r.Code != 403 {
		t.Fatalf("an editor shares while editors may not: %s", r)
	}

	// Editors may share: a view grant, but not «may share» and not their own access.
	must(200, owner.Do("PUT", base+"/shop/settings", map[string]any{"editorsCanShare": true}), "settings")
	if r := member.Do("PUT", base+"/shop/settings", map[string]any{"editorsCanShare": false}); r.Code != 403 {
		t.Fatalf("an editor changes the settings: %s", r)
	}
	mv = view(member, "shop")
	if !mv.CanManage || mv.Manager {
		t.Fatalf("an editor who may share: %+v", mv)
	}
	must(200, member.Do("PUT", base+"/shop/grants/user/friend@example.com", map[string]any{"role": "view", "canShare": false, "ttl": nil}), "editor grants")
	if r := member.Do("PUT", base+"/shop/grants/user/friend@example.com", map[string]any{"role": "edit", "canShare": true, "ttl": nil}); r.Code != 403 {
		t.Fatalf("an editor gives «may share»: %s", r)
	}
	if r := member.Do("DELETE", base+"/shop/grants/user/member@example.com", nil); r.Code != 403 {
		t.Fatalf("an editor removes themselves: %s", r)
	}
	if r := member.Do("PUT", base+"/shop/grants/user/not-an-address", map[string]any{"role": "view"}); r.Code != 400 {
		t.Fatalf("a bad address: %s", r)
	}
	if r := owner.Do("PUT", base+"/shop/grants/team/space:"+space, map[string]any{"role": "view"}); r.Code != 409 {
		t.Fatalf("a grant to the home team: %s", r)
	}
	must(200, owner.Do("PUT", base+"/shop/grants/user/friend@example.com", map[string]any{"role": "edit", "canShare": true, "ttl": "7d"}), "owner grants")
	v = view(owner, "shop")
	var friend bool
	for _, g := range v.Grants {
		if g.Type == "user" && g.ID == "friend@example.com" {
			friend = g.Role == "edit" && g.CanShare && g.ExpiresAt != nil
		}
	}
	if !friend {
		t.Fatalf("friend's grant: %+v", v.Grants)
	}

	// Links without sign-in: made, opened, copied again, switched off and on, reissued.
	if r := owner.Do("POST", base+"/shop/links", map[string]any{"rights": "view"}); r.Code != 400 {
		t.Fatalf("a link for nobody: %s", r)
	}
	if r := owner.Do("POST", base+"/shop/links", map[string]any{"forWhom": "x", "rights": "edit"}); r.Code != 400 {
		t.Fatalf("an editing link: %s", r)
	}
	var made struct {
		Link accessLink
		URL  string
	}
	must(http.StatusCreated, owner.Do("POST", base+"/shop/links", map[string]any{
		"forWhom": "Клиент", "rights": "view", "ttl": nil, "options": nil, "version": "latest", "pages": nil,
	}), "create link").JSON(&made)
	if !strings.HasPrefix(made.URL, "http://"+shareHost+"/s/") || !made.Link.Copyable || made.Link.ForWhom != "Клиент" || made.Link.DisabledAt != nil {
		t.Fatalf("made: %+v", made)
	}
	guest := s.Browser()
	open := func(u string) int {
		t.Helper()
		return guest.DoAt(s.URL, shareHost, "GET", strings.TrimPrefix(u, "http://"+shareHost)+"/f/Main.dc.html", nil).Code
	}
	if c := open(made.URL); c != 200 {
		t.Fatalf("guest opens the link: %d", c)
	}
	address := func() *string {
		t.Helper()
		var out struct{ URL *string }
		must(200, owner.Do("GET", base+"/shop/links/"+made.Link.ID+"/address", nil), "address").JSON(&out)
		return out.URL
	}
	if u := address(); u == nil || *u != made.URL {
		t.Fatalf("the address again: %v", u)
	}
	linkPath := base + "/shop/links/" + made.Link.ID
	if r := member.Do("PATCH", linkPath, map[string]any{"disabled": true}); r.Code != 403 {
		t.Fatalf("an editor switches off the owner's link: %s", r)
	}
	if r := member.Do("GET", linkPath+"/address", nil); r.Code != 403 {
		t.Fatalf("an editor copies the owner's link: %s", r)
	}
	var changed struct{ Link accessLink }
	must(200, owner.Do("PATCH", linkPath, map[string]any{"disabled": true}), "switch off").JSON(&changed)
	if changed.Link.DisabledAt == nil {
		t.Fatalf("switched off: %+v", changed)
	}
	if c := open(made.URL); c != http.StatusGone {
		t.Fatalf("a switched-off link: %d", c)
	}
	if r := owner.Do("PATCH", linkPath, map[string]any{"forWhom": "Другой"}); r.Code != 409 {
		t.Fatalf("a change of a switched-off link: %s", r)
	}
	must(200, owner.Do("PATCH", linkPath, map[string]any{"disabled": false, "forWhom": "Клиенту"}), "switch on").JSON(&changed)
	if changed.Link.DisabledAt != nil || changed.Link.ForWhom != "Клиенту" {
		t.Fatalf("switched on: %+v", changed)
	}
	if c := open(made.URL); c != 200 {
		t.Fatalf("a link switched on again: %d", c)
	}
	var again struct{ URL string }
	must(200, owner.Do("POST", linkPath+"/address", nil), "reissue").JSON(&again)
	if again.URL == made.URL || !strings.HasPrefix(again.URL, "http://"+shareHost+"/s/") {
		t.Fatalf("reissued: %+v", again)
	}
	if c := open(made.URL); c == 200 {
		t.Fatalf("the old address after a reissue: %d", c)
	}
	if c := open(again.URL); c != 200 {
		t.Fatalf("the new address: %d", c)
	}
	var log struct{ Log []map[string]any }
	must(200, owner.Do("GET", base+"/shop/log", nil), "log").JSON(&log)
	actions := map[string]bool{}
	for _, e := range log.Log {
		if act, ok := e["action"].(string); ok {
			actions[act] = true
		}
	}
	for _, want := range []string{"share.create", "share.revoke", "share.enable", "share.reissue"} {
		if !actions[want] {
			t.Fatalf("no %s in the log: %v", want, actions)
		}
	}

	// A project nobody recorded: its view, nothing to change in it.
	if v := view(owner, "nothing"); v.Stored {
		t.Fatalf("an unknown project: %+v", v)
	}
	if r := owner.Do("POST", base+"/nothing/links", map[string]any{"forWhom": "x", "rights": "view"}); r.Code != 404 {
		t.Fatalf("a link to an unknown project: %s", r)
	}

	// After a restart of the hub: the grants, the links and their kept addresses are there.
	s.Restart()
	owner = s.SignIn("owner@example.com")
	a.Connect()
	v = view(owner, "shop")
	if len(v.Links) != 1 || !v.Links[0].Copyable || v.Links[0].ForWhom != "Клиенту" || len(v.Grants) < 2 {
		t.Fatalf("after restart: %+v", v)
	}
	if u := address(); u == nil || *u != again.URL {
		t.Fatalf("the address after restart: %v", u)
	}
	s.Eventually("the guest opens after the restart", func() bool { return open(again.URL) == 200 })
	if c := open(made.URL); c == 200 {
		t.Fatalf("the reissued address after restart: %d", c)
	}
	if strings.Contains(s.Logs.String(), "level=ERROR") {
		t.Errorf("errors in the log:\n%s", s.Logs.String())
	}
}
