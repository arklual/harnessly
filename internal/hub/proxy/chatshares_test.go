package proxy_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

const (
	shareDraft = "DRAFTaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shareLink  = "LINKaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sharePage  = "<!doctype html><title>Chat</title><p>the redacted chat</p>"
)

// shareRuntime is a machine's snapshot routes: it gives the draft's page, publishes it as a link at the address it was
// handed, and ends the link.
type shareRuntime struct {
	mu    sync.Mutex
	bases []string
	calls []string
}

func (s *shareRuntime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	base := r.Header.Get("X-Harnessly-Share-Base")
	s.bases = append(s.bases, base)
	s.mu.Unlock()
	switch {
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/snapshots/"+shareDraft+"/download") && r.URL.Query().Get("format") == "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="chat.html"`)
		_, _ = io.WriteString(w, sharePage)
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/shares"):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"share":{"id":"`+shareLink+`","url":"`+base+shareLink+`","title":"Chat","expiresAt":4102444800000}}`)
	case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/shares/"+shareLink):
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

// a chat's snapshot published as a link is kept by the hub and opens to the people of the space with the
// author's machine off; to anybody else it is not there, and its author ends it without the machine.
func TestChatShareOpensFromTheHubWithTheMachineOff(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &shareRuntime{}
	agent := owner.Machine(space, rt)
	q := "?machine=" + agent.MachineID
	r := owner.Do("POST", "/agents/api/acme/shares"+q, map[string]any{"draftId": shareDraft, "expiresInDays": 7},
		"X-Harnessly-Share-Base", "https://evil.example.org/chat-shares/")
	if r.Code != 201 || !strings.Contains(string(r.Body), shareLink) {
		t.Fatalf("publish: %s", r)
	}
	if strings.Contains(string(r.Body), "guestLinksAvailable") {
		t.Fatal("guest publication was offered by default")
	}
	if r := owner.Do("POST", "/agents/api/acme/shares/"+shareLink+"/guest"+q, map[string]any{}); r.Code != 404 {
		t.Fatalf("guest publication with feature off: %s", r)
	}
	want := s.URL + "/agents/api/acme/chat-shares/"
	for _, b := range rt.bases {
		if b != "" && b != want {
			t.Fatalf("the address of the links is the hub's: %q, want %q", b, want)
		}
	}
	if !strings.Contains(string(r.Body), want+shareLink) {
		t.Fatalf("the link's address: %s", r)
	}
	page := "/agents/api/acme/chat-shares/" + shareLink
	open := func(p *hubtest.Person) hubtest.Resp { return p.Do("GET", page, nil) }

	member := s.SignIn("member@example.com")
	stranger := s.SignIn("stranger@example.com")
	owner.Join(space, member)
	agent.Close()
	for name, p := range map[string]*hubtest.Person{"the author": owner, "a member": member} {
		r := open(p)
		if r.Code != 200 || string(r.Body) != sharePage || !strings.HasPrefix(r.Header.Get("Content-Security-Policy"), "sandbox") || r.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s reads the page with the machine off: %s %v", name, r, r.Header)
		}
	}
	if r := open(stranger); r.Code != 404 {
		t.Fatalf("a stranger: %s", r)
	}
	if r := s.Browser().Do("GET", page, nil); r.Code == 200 {
		t.Fatalf("nobody signed in: %s", r)
	}
	if r := owner.Do("GET", "/agents/api/acme/chat-shares/"+shareDraft, nil); r.Code != 404 {
		t.Fatalf("an unknown link: %s", r)
	}
	// A member cannot end the author's link while the machine is away; the author can.
	if r := member.Do("DELETE", "/agents/api/acme/shares/"+shareLink+q, nil); r.Code == 204 {
		t.Fatalf("a member ends the author's link: %s", r)
	}
	if r := owner.Do("DELETE", "/agents/api/acme/shares/"+shareLink+q, nil); r.Code != 204 {
		t.Fatalf("the author ends the link with the machine off: %s", r)
	}
	if r := open(member); r.Code != 410 {
		t.Fatalf("an ended link: %s", r)
	}
}

func TestGuestChatSnapshotIsOptInReadOnly(t *testing.T) {
	for _, onHub := range []bool{false, true} {
		name := "dedicated"
		if onHub {
			name = "single-host"
		}
		t.Run(name, func(t *testing.T) { testGuestChat(t, onHub) })
	}
}
func testGuestChat(t *testing.T, onHub bool) {
	host := "guests.example.test"
	guestOrigin := "http://" + host
	if onHub {
		guestOrigin = ""
	}
	s := hubtest.New(t, withWorkspace, fleetOn, hubtest.Env("HUB_DESIGN_SHARE_URL", guestOrigin), hubtest.Env("HUB_CHAT_GUEST_LINKS", "on"))
	owner := s.SignIn("owner@example.com")
	mate := s.SignIn("mate@example.com")
	space, _ := owner.NewSpace("Team")
	owner.Join(space, mate)
	agent := owner.Machine(space, &shareRuntime{})
	q := "?machine=" + agent.MachineID
	r := owner.Do("POST", "/agents/api/acme/shares"+q, map[string]any{"draftId": shareDraft, "expiresInDays": 7})
	if r.Code != 201 || !strings.Contains(string(r.Body), "guestLinksAvailable") {
		t.Fatal(r)
	}
	agent.Close()
	manage := "/agents/api/acme/shares/" + shareLink + "/guest" + q
	browser := s.Browser()
	guest := func(method, path string) hubtest.Resp {
		if onHub {
			return browser.Do(method, path, nil)
		}
		return browser.DoAt(s.URL, host, method, path, nil)
	}
	pathPrefix := "/chat/"
	origin := guestOrigin
	if onHub {
		origin = s.URL
		pathPrefix = "/guest/chat/"
	}

	if r := guest("GET", pathPrefix+shareLink); r.Code != 404 {
		t.Fatal(r)
	}
	if r := mate.Do("POST", manage, map[string]any{}); r.Code != 404 {
		t.Fatal(r)
	}
	makeLink := func() string {
		r := owner.Do("POST", manage, map[string]any{})
		if r.Code != 201 {
			t.Fatal(r)
		}
		var out struct{ URL string }
		r.JSON(&out)
		if !strings.HasPrefix(out.URL, origin+pathPrefix) {
			t.Fatal("unexpected guest URL")
		}
		return strings.TrimPrefix(out.URL, origin)
	}
	path := makeLink()
	list := "/agents/api/acme/chat-guest-links"
	if r := owner.Do("GET", list, nil); r.Code != 200 || !strings.Contains(string(r.Body), shareLink) || strings.Contains(string(r.Body), path) {
		t.Fatal(r)
	}
	if r := mate.Do("GET", list, nil); r.Code != 200 || strings.Contains(string(r.Body), shareLink) {
		t.Fatal(r)
	}
	previous := path
	path = makeLink()
	if r := guest("GET", previous); r.Code != 404 {
		t.Fatal(r)
	}
	if r := guest("HEAD", path); r.Code != 200 || len(r.Body) != 0 {
		t.Fatal(r)
	}
	if r := guest("GET", path); r.Code != 200 || !strings.Contains(string(r.Body), "the redacted chat") || r.Header.Get("Cache-Control") != "private, no-store" || !strings.HasPrefix(r.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal(r)
	}
	for _, route := range []struct{ method, path string }{{"POST", path}, {"GET", path + "/files"}, {"GET", "/api/me/tokens?token=" + strings.TrimPrefix(path, pathPrefix)}, {"GET", pathPrefix + shareDraft}} {
		if r := guest(route.method, route.path); r.Code == 200 {
			t.Fatalf("%s %s unexpectedly succeeded", route.method, route.path)
		}
	}
	if r := mate.Do("DELETE", manage, nil); r.Code != 404 {
		t.Fatal(r)
	}
	if r := owner.Do("DELETE", manage, nil); r.Code != 204 {
		t.Fatal(r)
	}
	if r := guest("GET", path); r.Code != 404 {
		t.Fatal(r)
	}
	path = makeLink()
	s.Clock.Advance(8 * 24 * time.Hour)
	if r := guest("GET", path); r.Code != 404 {
		t.Fatal(r)
	}
}
