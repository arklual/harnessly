package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
)

type shareRig struct {
	t    *testing.T
	reg  *apps.Registry
	d    deps
	g    *apps.Guests
	call func(method, path, body string, h map[string]string) (int, string)
}

func newShareRig(t *testing.T) *shareRig {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK:    func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" },
		MachineAgent: func(r *http.Request) bool { return r.Header.Get("X-Test-Agent") == "yes" },
		Claims:       func(r *http.Request) map[string]any { return map[string]any{"sub": "ann@team.test"} }}
	reg := &apps.Registry{Root: filepath.Join(dir, "apps")}
	guests := apps.NewGuests(reg, apps.OpenStorage(dir))
	routes := appShareRoutes(d, reg, guests, "0.17.0")
	rig := &shareRig{t: t, reg: reg, d: d, g: guests}
	rig.call = func(method, path, body string, h map[string]string) (int, string) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				return w.Code, w.Body.String()
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, ""
	}
	return rig
}

var (
	shareCLI  = map[string]string{"x-cli-token": "tok"}
	sharePage = map[string]string{"X-Test-Session": "yes"}
	shareBot  = map[string]string{"X-Test-Agent": "yes"}
)

func panePkg(t *testing.T, version string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(dir, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["version"] = version
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func asJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out
}

func TestShareRoutesNeedTheRightCaller(t *testing.T) {
	r := newShareRig(t)
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/cli/apps/publish"}, {"POST", "/api/cli/apps/promote"}, {"POST", "/api/cli/apps/rollback"},
		{"GET", "/api/cli/apps/channels"}, {"POST", "/api/cli/apps/share"}, {"GET", "/api/cli/apps/team"},
	} {
		if code, _ := r.call(c.method, c.path, `{}`, nil); code != http.StatusForbidden {
			t.Errorf("%s no token: %d", c.path, code)
		}
		if code, _ := r.call(c.method, c.path, `{}`, sharePage); code != http.StatusForbidden {
			t.Errorf("%s a page session is not the cli: %d", c.path, code)
		}
	}
	if code, _ := r.call("GET", "/api/platform/apps/team", "", nil); code != http.StatusUnauthorized {
		t.Errorf("team page, no session: %d", code)
	}
	for _, p := range []string{"/api/platform/apps/team/sync", "/api/platform/apps/team/snapshot"} {
		if code, _ := r.call("POST", p, `{}`, sharePage); code != http.StatusForbidden {
			t.Errorf("%s as a page: %d", p, code)
		}
		if code, _ := r.call("POST", p, `{}`, shareCLI); code != http.StatusForbidden {
			t.Errorf("%s as the cli: %d", p, code)
		}
	}
}

func TestChannelsOverTheCLI(t *testing.T) {
	r := newShareRig(t)
	v1, v2 := panePkg(t, "1.0.0"), panePkg(t, "1.1.0")
	post := func(path, body string) (int, map[string]any) {
		code, out := r.call("POST", path, body, shareCLI)
		return code, asJSON(t, out)
	}
	if code, out := post("/api/cli/apps/publish", `{"dir":"`+v1+`","channel":"beta"}`); code != 200 || out["version"] != "1.0.0-beta.1" {
		t.Fatalf("publish: %d %v", code, out)
	}
	if code, out := post("/api/cli/apps/promote", `{"id":"acme.demo","version":"1.0.0","channel":"stable"}`); code != 200 || out["version"] != "1.0.0" {
		t.Fatalf("promote: %d %v", code, out)
	}
	if code, out := post("/api/cli/apps/follow", `{"id":"acme.demo","channel":"stable"}`); code != 200 {
		t.Fatalf("follow: %d %v", code, out)
	}
	agreeAll(t, r.reg)
	if code, out := post("/api/cli/apps/publish", `{"dir":"`+v2+`","channel":"stable"}`); code != 200 || out["version"] != "1.1.0" {
		t.Fatalf("publish stable: %d %v", code, out)
	}
	if code, _ := post("/api/cli/apps/publish", `{"dir":"`+v2+`","channel":"stable"}`); code != http.StatusConflict {
		t.Fatalf("a number twice: %d", code)
	}
	if code, out := post("/api/cli/apps/sync", `{}`); code != 200 || len(out["moved"].([]any)) != 1 {
		t.Fatalf("sync: %d %v", code, out)
	}
	if got, _ := r.reg.Get("acme.demo"); got.Version != "1.1.0" || got.Channel != "stable" {
		t.Fatalf("installed %+v", got)
	}
	code, out := post("/api/cli/apps/rollback", `{"id":"acme.demo","channel":"stable"}`)
	if code != 200 || out["version"] != "1.0.0" || len(out["moved"].([]any)) != 1 {
		t.Fatalf("rollback: %d %v", code, out)
	}
	if got, _ := r.reg.Get("acme.demo"); got.Version != "1.0.0" {
		t.Fatalf("after rollback %+v", got)
	}
	if code, _ := post("/api/cli/apps/rollback", `{"id":"acme.demo","channel":"stable"}`); code != http.StatusConflict {
		t.Fatalf("nothing before: %d", code)
	}
	if code, _ := post("/api/cli/apps/publish", `{"dir":"`+v1+`","channel":"nightly"}`); code != http.StatusBadRequest {
		t.Fatalf("bad channel: %d", code)
	}
	code, body := r.call("GET", "/api/cli/apps/channels?id=acme.demo", "", shareCLI)
	if code != 200 || !strings.Contains(body, `"stable"`) {
		t.Fatalf("channels: %d %s", code, body)
	}
}

// The owner installs for the team on one machine; the platform carries the snapshot to a member's machine, where the
// app is there with no install of the member's own.
func TestOwnerInstallsForTheTeamAndTheMemberSeesIt(t *testing.T) {
	owner, member := newShareRig(t), newShareRig(t)
	post := func(r *shareRig, path, body string, h map[string]string) (int, map[string]any) {
		code, out := r.call("POST", path, body, h)
		return code, asJSON(t, out)
	}
	if code, _ := post(owner, "/api/cli/apps/team/set", `{"space":"sp1","role":"owner"}`, shareCLI); code != 200 {
		t.Fatal(code)
	}
	pkg := panePkg(t, "1.0.0")
	code, out := post(owner, "/api/cli/apps/publish", `{"dir":"`+pkg+`","channel":"stable","team":true}`, shareCLI)
	if code != 200 || out["catalog"] == nil {
		t.Fatalf("publish to the team: %d %v", code, out)
	}
	// The owner's page sees the entry, not installed yet.
	code, body := owner.call("GET", "/api/platform/apps/team", "", sharePage)
	info := asJSON(t, body)
	if code != 200 || info["role"] != "owner" || len(info["catalog"].([]any)) != 1 || info["catalog"].([]any)[0].(map[string]any)["installed"] != false {
		t.Fatalf("team page: %d %s", code, body)
	}
	if code, out := post(owner, "/api/platform/apps/team/install", `{"id":"acme.demo","channel":"stable"}`, sharePage); code != 200 || out["version"] != "1.0.0" {
		t.Fatalf("install for the team: %d %v", code, out)
	}

	// The platform asks the owner's machine for its snapshot and gives it to the member's.
	code, snap := owner.call("POST", "/api/platform/apps/team/snapshot", `{}`, shareBot)
	if code != 200 {
		t.Fatalf("snapshot: %d %s", code, snap)
	}
	var s map[string]any
	_ = json.Unmarshal([]byte(snap), &s)
	s["role"] = "member"
	raw, _ := json.Marshal(s)
	if code, out := post(member, "/api/platform/apps/team/sync", string(raw), shareBot); code != 200 || len(out["problems"].([]any)) != 0 {
		t.Fatalf("sync: %d %v", code, out)
	}
	list, _ := member.reg.List()
	if len(list) != 1 || list[0].ID != "acme.demo" || !list[0].Team {
		t.Fatalf("the member's apps: %+v", list)
	}
	view := viewOfApp(list[0], false)
	if !view.Team || view.Name != "Демо" {
		t.Fatalf("the page's view of it: %+v", view)
	}

	// The member cannot install or uninstall for the team; they mute it for themselves.
	if code, out := post(member, "/api/platform/apps/team/install", `{"id":"acme.demo","channel":"stable"}`, sharePage); code != http.StatusForbidden || out["error"] != "not-owner" {
		t.Fatalf("member install: %d %v", code, out)
	}
	if code, out := post(member, "/api/platform/apps/team/uninstall", `{"id":"acme.demo"}`, sharePage); code != http.StatusForbidden {
		t.Fatalf("member uninstall: %d %v", code, out)
	}
	if code, out := post(member, "/api/cli/apps/team/uninstall", `{"id":"acme.demo"}`, shareCLI); code != http.StatusForbidden {
		t.Fatalf("member uninstall by cli: %d %v", code, out)
	}
	if code, _ := post(member, "/api/platform/apps/team/mute", `{"id":"acme.demo","mute":true}`, sharePage); code != 200 {
		t.Fatalf("mute: %d", code)
	}
	if list, _ := member.reg.List(); len(list) != 0 {
		t.Fatalf("muted: %+v", list)
	}
	_, body = member.call("GET", "/api/platform/apps/team", "", sharePage)
	if got := asJSON(t, body)["muted"].([]any); len(got) != 1 {
		t.Fatalf("the page knows it is muted: %s", body)
	}
	// An old snapshot is refused.
	s["at"] = 1
	raw, _ = json.Marshal(s)
	if code, _ := post(member, "/api/platform/apps/team/sync", string(raw), shareBot); code != http.StatusConflict {
		t.Fatalf("stale: %d", code)
	}
	// A machine with no team says so.
	lone := newShareRig(t)
	if code, out := post(lone, "/api/platform/apps/team/install", `{"id":"acme.demo","channel":"stable"}`, sharePage); code != http.StatusConflict || out["error"] != "no-team" {
		t.Fatalf("no team: %d %v", code, out)
	}
}

func TestGuestLinkOverTheRoutes(t *testing.T) {
	r := newShareRig(t)
	if _, _, err := r.reg.Install(panePkg(t, "1.0.0"), apps.InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	code, body := r.call("POST", "/api/cli/apps/share", `{"id":"acme.demo","ttlSeconds":3600}`, shareCLI)
	out := asJSON(t, body)
	path, _ := out["path"].(string)
	if code != 200 || !strings.HasPrefix(path, apps.GuestPrefix) {
		t.Fatalf("share: %d %s", code, body)
	}
	// The guest reaches the page with no cookie and no token header.
	if code, page := r.call("GET", path, "", nil); code != 200 || !strings.Contains(page, "<iframe") {
		t.Fatalf("guest page: %d %s", code, page)
	}
	if code, _ := r.call("GET", "/api/cli/apps/shares?id=acme.demo", "", shareCLI); code != 200 {
		t.Fatalf("shares: %d", code)
	}
	link := out["link"].(map[string]any)
	if code, _ := r.call("POST", "/api/cli/apps/unshare", `{"id":"`+link["id"].(string)+`"}`, shareCLI); code != 200 {
		t.Fatalf("unshare: %d", code)
	}
	if code, _ := r.call("GET", path, "", nil); code != http.StatusNotFound {
		t.Fatalf("revoked link: %d", code)
	}
	// A chat-only app is not shared.
	chat := filepath.Join(t.TempDir(), "chatty")
	_ = apps.Scaffold(chat, "acme.chatty", "", "chat")
	_, _, _ = r.reg.Install(chat, apps.InstallOptions{HostVersion: "0.17.0"})
	if code, body := r.call("POST", "/api/cli/apps/share", `{"id":"acme.chatty"}`, shareCLI); code != http.StatusUnprocessableEntity {
		t.Fatalf("chat-only share: %d %s", code, body)
	}
}

// The page, the CLI and the app's removal tell the cluster through the machine agent: the team changed, a guest link
// was made (its hash, never its token) or ended.
func TestAppsEventsReachTheMachineAgent(t *testing.T) {
	r := newShareRig(t)
	events := make(chan map[string]any, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var ev map[string]any
		_ = json.NewDecoder(req.Body).Decode(&ev)
		if req.URL.Path != "/apps/event" || req.Header.Get("x-agent-local-token") != "agent-secret" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		events <- ev
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(r.d.DataDir, "agent.json"), []byte(fmt.Sprintf(`{"port":%d,"token":"agent-secret"}`, port)), 0o600); err != nil {
		t.Fatal(err)
	}
	wireAppsEvents(r.d, r.reg, r.g)
	next := func(kind string) map[string]any {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-events:
				if ev["event"] == kind {
					return ev
				}
			case <-deadline:
				t.Fatalf("no %s", kind)
			}
		}
	}
	next("apps.guest.sync") // the start: the live links, none yet

	if code, _ := r.call("POST", "/api/cli/apps/team/set", `{"space":"sp1","role":"owner"}`, shareCLI); code != 200 {
		t.Fatal(code)
	}
	pkg := panePkg(t, "1.0.0")
	if code, out := r.call("POST", "/api/cli/apps/publish", `{"dir":"`+pkg+`","channel":"stable","team":true}`, shareCLI); code != 200 {
		t.Fatalf("publish: %d %s", code, out)
	}
	next("apps.team.changed")
	if code, out := r.call("POST", "/api/platform/apps/team/install", `{"id":"acme.demo","channel":"stable"}`, sharePage); code != 200 {
		t.Fatalf("install: %d %s", code, out)
	}
	next("apps.team.changed")
	if code, out := r.call("POST", "/api/platform/apps/team/uninstall", `{"id":"acme.demo"}`, sharePage); code != 200 {
		t.Fatalf("uninstall: %d %s", code, out)
	}
	next("apps.team.changed")

	if _, _, err := r.reg.Install(panePkg(t, "1.0.0"), apps.InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	code, body := r.call("POST", "/api/cli/apps/share", `{"id":"acme.demo","ttlSeconds":3600}`, shareCLI)
	if code != 200 {
		t.Fatalf("share: %d %s", code, body)
	}
	made := next("apps.guest.created")
	link := asJSON(t, body)["link"].(map[string]any)
	hash, _ := made["hash"].(string)
	if made["id"] != link["id"] || len(hash) != 64 || made["expiresAt"] == nil {
		t.Fatalf("created: %v", made)
	}
	path := asJSON(t, body)["path"].(string)
	if token := strings.Split(strings.TrimPrefix(path, apps.GuestPrefix), "/")[0]; strings.Contains(fmt.Sprint(made), token) {
		t.Fatal("the token went to the agent")
	}
	if code, _ := r.call("POST", "/api/cli/apps/unshare", `{"id":"`+link["id"].(string)+`"}`, shareCLI); code != 200 {
		t.Fatal(code)
	}
	if ev := next("apps.guest.revoked"); ev["id"] != link["id"] {
		t.Fatalf("revoked: %v", ev)
	}
	// A second link ends with the app.
	_, body = r.call("POST", "/api/cli/apps/share", `{"id":"acme.demo"}`, shareCLI)
	second := asJSON(t, body)["link"].(map[string]any)["id"]
	next("apps.guest.created")
	if n, err := r.g.RevokeApp("acme.demo"); err != nil || n != 1 {
		t.Fatalf("revoke app: %d %v", n, err)
	}
	if ev := next("apps.guest.revoked"); ev["id"] != second {
		t.Fatalf("revoked with the app: %v", ev)
	}
}
