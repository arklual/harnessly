package proxy_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

const slotDomain = "slots.example.net"

var slotKey = "1-" + strings.Repeat("0a", 16)

// slotFixture is a hub with the slot hosts configured and the switch on, a machine whose runtime answers any path, and a
// browser that keeps cookies per host the way a real one does (the test client's jar keys on the address, not the Host).
type slotFixture struct {
	*fixture
	other *hubtest.Person
	http  *http.Client
	jar   map[string]string // host -> "name=value"
}

func slotSetup(t *testing.T) *slotFixture {
	t.Helper()
	f := &slotFixture{fixture: setup(t, hubtest.Env("HUB_PREVIEW_SLOT_DOMAIN", slotDomain)), jar: map[string]string{}}
	f.other = f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, f.other)
	f.http = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f
}

func (f *slotFixture) switchOn(t *testing.T, person string) {
	t.Helper()
	body := map[string]any{"feature": "preview-slot-host", "enabled": true}
	if person != "" {
		body["person"] = person
	}
	if r := f.owner.Do("PUT", "/api/admin/switches", body); r.Code != 204 {
		t.Fatalf("switch: %s", r)
	}
}

func (f *slotFixture) path(rest string) string { return "/m/" + f.id + "/__pv/" + slotKey + rest }

// open asks the hub for the person's entry address of a slot.
func (f *slotFixture) open(t *testing.T, p *hubtest.Person, path string) (hubtest.Resp, string, string) {
	t.Helper()
	r := p.Do("POST", "/api/preview/slot-open", map[string]string{"machine": f.id, "path": path})
	var out struct{ URL, Host string }
	if r.Code == 200 {
		r.JSON(&out)
	}
	return r, out.URL, out.Host
}

// get is a request to a slot host as a browser would send it.
func (f *slotFixture) get(host, path string, headers ...string) (*http.Response, string) {
	req, _ := http.NewRequest("GET", f.s.URL+path, nil)
	req.Host = host
	if c := f.jar[host]; c != "" {
		req.Header.Set("Cookie", c)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := f.http.Do(req)
	if err != nil {
		f.s.T.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	for _, c := range res.Cookies() {
		if c.MaxAge > 0 {
			f.jar[host] = c.Name + "=" + c.Value
		}
	}
	return res, string(b)
}

// enter walks an entry address and returns the answer; the cookie lands in the jar of its host.
func (f *slotFixture) enter(t *testing.T, entry string, headers ...string) *http.Response {
	t.Helper()
	u, err := url.Parse(entry)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := f.get(u.Host, u.RequestURI(), headers...)
	return res
}

func (f *slotFixture) opened(t *testing.T, p *hubtest.Person, rest string) (host string) {
	t.Helper()
	r, entry, host := f.open(t, p, f.path(rest))
	if r.Code != 200 {
		t.Fatalf("slot-open: %s", r)
	}
	if res := f.enter(t, entry); res.StatusCode != http.StatusFound || res.Header.Get("Location") != f.path(rest) {
		t.Fatalf("enter: %d %v", res.StatusCode, res.Header)
	}
	return host
}

func TestSlotHostOffUnlessSwitched(t *testing.T) {
	f := slotSetup(t)
	if r, _, _ := f.open(t, f.owner, f.path("/x")); r.Code != 404 {
		t.Fatalf("an off switch is not there: %s", r)
	}
	f.switchOn(t, "")
	if r, entry, host := f.open(t, f.owner, f.path("/x")); r.Code != 200 || !strings.HasSuffix(strings.Split(host, ":")[0], "-1."+slotDomain) || !strings.Contains(entry, "/__tk/enter") {
		t.Fatalf("on: %s %q", r, entry)
	}
}

func TestSlotHostOpensAndServesOnlyItsSlot(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	host := f.opened(t, f.owner, "/page")
	res, body := f.get(host, f.path("/page"))
	if res.StatusCode != 200 || !strings.Contains(body, "runtime") {
		t.Fatalf("the slot: %d %s", res.StatusCode, body)
	}
	if csp := strings.Join(res.Header.Values("Content-Security-Policy"), ";"); strings.Contains(csp, "sandbox") || !strings.Contains(csp, "frame-ancestors") {
		t.Errorf("csp on the slot's own origin: %q", csp)
	}
	if res.Header.Get("X-Robots-Tag") == "" || strings.Contains(res.Header.Get("Cache-Control"), "public") {
		t.Errorf("headers: %v", res.Header)
	}
	if res, _ := f.get(host, "/"); res.StatusCode != http.StatusFound || res.Header.Get("Location") != f.path("/") {
		t.Errorf("the root goes into the slot: %d %v", res.StatusCode, res.Header)
	}
	for _, p := range []string{
		"/m/" + f.id + "/api/v2/agents",                             // the machine's own API
		"/m/" + f.id + "/__pv/2-" + strings.Repeat("0a", 16) + "/x", // another slot
		"/m/" + f.id + "/__pv/" + slotKey + "x",                     // a longer name
		"/api/me",                                                   // the hub
		"/__tk/other",                                               // the reserved prefix
	} {
		if res, body := f.get(host, p); res.StatusCode == 200 || strings.Contains(body, "runtime") {
			t.Errorf("%s reached more than the slot: %d %s", p, res.StatusCode, body)
		}
	}
	for _, p := range []string{f.path("/../x"), f.path("//x"), f.path("/a/./b"), f.path("/a%2f..%2fb"), f.path(`/a\b`)} {
		if res, _ := f.get(host, p); res.StatusCode == 200 {
			t.Errorf("%s was served", p)
		}
	}
}

func TestSlotTicketIsSingleUseAndBoundToItsHost(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	_, entry, _ := f.open(t, f.owner, f.path("/x"))
	if res := f.enter(t, entry); res.StatusCode != http.StatusFound {
		t.Fatalf("first use: %d", res.StatusCode)
	}
	f.jar = map[string]string{}
	if res := f.enter(t, entry); res.StatusCode != http.StatusForbidden || len(res.Cookies()) != 0 {
		t.Errorf("replay: %d %v", res.StatusCode, res.Cookies())
	}
	// A ticket of person A on the host of person B.
	_, entryA, _ := f.open(t, f.owner, f.path("/x"))
	_, _, hostB := f.open(t, f.other, f.path("/x"))
	u, _ := url.Parse(entryA)
	res, _ := f.get(hostB, u.RequestURI())
	if res.StatusCode != http.StatusForbidden || f.jar[hostB] != "" {
		t.Errorf("a ticket on a foreign host: %d jar=%q", res.StatusCode, f.jar[hostB])
	}
	// The same ticket was spent by that attempt.
	if res := f.enter(t, entryA); res.StatusCode != http.StatusForbidden {
		t.Errorf("a ticket tried on a foreign host is still spent: %d", res.StatusCode)
	}
}

func TestSlotForeignSlotAndForgedCookieAreRefused(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	hostA := f.opened(t, f.owner, "/x")
	hostB := f.opened(t, f.other, "/x")
	if hostA == hostB {
		t.Fatal("two people, one host")
	}
	cookieA, cookieB := f.jar[hostA], f.jar[hostB]
	// B's cookie sent to A's host, and A's to B's.
	for host, c := range map[string]string{hostA: cookieB, hostB: cookieA} {
		f.jar[host] = c
		if res, body := f.get(host, f.path("/x")); res.StatusCode == 200 || strings.Contains(body, "runtime") {
			t.Errorf("a cookie of another host on %s: %d", host, res.StatusCode)
		}
	}
	f.jar[hostA], f.jar[hostB] = cookieA, cookieB
	if res, _ := f.get(hostA, f.path("/x")); res.StatusCode != 200 {
		t.Fatalf("the right cookie: %d", res.StatusCode)
	}
	// A cookie of slot 1 on the host of slot 2 of the same person.
	_, entry2, host2 := f.open(t, f.owner, "/m/"+f.id+"/__pv/2-"+strings.Repeat("0a", 16)+"/x")
	if host2 == hostA {
		t.Fatal("two slots, one host")
	}
	_ = entry2
	f.jar[host2] = cookieA
	if res, _ := f.get(host2, "/m/"+f.id+"/__pv/2-"+strings.Repeat("0a", 16)+"/x"); res.StatusCode == 200 {
		t.Errorf("slot 1's cookie opened slot 2: %d", res.StatusCode)
	}
	// Forged: a payload with the right shape and a MAC of nothing, and a real MAC on another payload.
	name, val, _ := strings.Cut(cookieA, "=")
	body, mac, _ := strings.Cut(val, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	var claims map[string]any
	_ = json.Unmarshal(raw, &claims)
	claims["u"] = f.other.ID
	forged, _ := json.Marshal(claims)
	for label, c := range map[string]string{
		"payload swapped": name + "=" + base64.RawURLEncoding.EncodeToString(forged) + "." + mac,
		"no mac":          name + "=" + body + ".",
		"garbage mac":     name + "=" + body + ".AAAA",
		"no dot":          name + "=" + body,
	} {
		f.jar[hostA] = c
		if res, _ := f.get(hostA, f.path("/x")); res.StatusCode == 200 {
			t.Errorf("forged cookie (%s) was accepted", label)
		}
	}
}

func TestSlotCookieExpiresAndMembershipIsRechecked(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	host := f.opened(t, f.other, "/x")
	if res, _ := f.get(host, f.path("/x")); res.StatusCode != 200 {
		t.Fatalf("before: %d", res.StatusCode)
	}
	// The switch is turned off for that person: the cookie they hold stops working at once.
	if r := f.owner.Do("PUT", "/api/admin/switches", map[string]any{"feature": "preview-slot-host", "person": "other@example.com", "enabled": false}); r.Code != 204 {
		t.Fatalf("switch off: %s", r)
	}
	if res, _ := f.get(host, f.path("/x")); res.StatusCode == 200 {
		t.Errorf("a cookie outlived its switch: %d", res.StatusCode)
	}
	f.switchOn(t, "other@example.com")
	if res, _ := f.get(host, f.path("/x")); res.StatusCode != 200 {
		t.Fatalf("on again: %d", res.StatusCode)
	}
	f.s.Clock.Advance(13 * time.Hour)
	if res, _ := f.get(host, f.path("/x")); res.StatusCode == 200 {
		t.Errorf("an expired cookie served: %d", res.StatusCode)
	}
}

func TestSlotRequestsMustBeOurs(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	host := f.opened(t, f.owner, "/x")
	otherHost := f.opened(t, f.other, "/x")
	for label, h := range map[string][]string{
		"cross-site fetch":      {"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "cors"},
		"cross-site image":      {"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "no-cors", "Sec-Fetch-Dest", "image"},
		"origin of another man": {"Origin", "http://" + otherHost, "Sec-Fetch-Site", "same-site"},
		"opaque origin":         {"Origin", "null"},
		"service worker":        {"Service-Worker", "script"},
	} {
		if res, body := f.get(host, f.path("/x"), h...); res.StatusCode == 200 || strings.Contains(body, "runtime") {
			t.Errorf("%s was served: %d", label, res.StatusCode)
		}
	}
	for label, h := range map[string][]string{
		"the slot itself":      {"Origin", "http://" + host, "Sec-Fetch-Site", "same-origin"},
		"a link from anywhere": {"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "document"},
	} {
		if res, _ := f.get(host, f.path("/x"), h...); res.StatusCode != 200 {
			t.Errorf("%s was refused: %d", label, res.StatusCode)
		}
	}
}

func TestSlotRemovedMemberIsRefused(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	host := f.opened(t, f.other, "/x")
	if r := f.owner.Do("DELETE", "/api/spaces/"+f.spaceID+"/members/"+f.other.ID, nil); r.Code/100 != 2 {
		t.Skipf("no way to remove a member here: %s", r)
	}
	if res, _ := f.get(host, f.path("/x")); res.StatusCode == 200 {
		t.Errorf("a removed member still reaches the machine: %d", res.StatusCode)
	}
	if r, _, _ := f.open(t, f.other, f.path("/x")); r.Code == 200 {
		t.Errorf("a removed member opened a slot: %s", r)
	}
}

func TestSlotOpenRefusesWhatIsNotASlotAndOtherMachines(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	for _, p := range []string{"/m/" + f.id + "/api/v2/agents", "/x", "http://evil.example/m/" + f.id + "/__pv/" + slotKey + "/", f.path("/../x"), "/m/" + f.id + "/__pv/7-" + strings.Repeat("0a", 16) + "/"} {
		if r, _, _ := f.open(t, f.owner, p); r.Code == 200 {
			t.Errorf("%q was opened", p)
		}
	}
	if r := f.owner.Do("POST", "/api/preview/slot-open", map[string]string{"machine": "nomachine", "path": f.path("/x")}); r.Code == 200 {
		t.Errorf("no such machine: %s", r)
	}
	// A person outside the machine's space.
	stranger := f.s.SignIn("stranger@example.com")
	if r, _, _ := f.open(t, stranger, f.path("/x")); r.Code == 200 {
		t.Errorf("a stranger opened a slot: %s", r)
	}
}

func TestSlotHostsAreNotTheHubsOrigin(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	host := f.opened(t, f.owner, "/x")
	// The hub's own cookie of the owner never decides anything on the slot host: no cookie, no way in.
	delete(f.jar, host)
	if res, body := f.get(host, f.path("/x")); res.StatusCode == 200 || strings.Contains(body, "runtime") {
		t.Errorf("no cookie: %d", res.StatusCode)
	}
	if res, _ := f.get(host, "/api/me"); res.StatusCode == 200 {
		t.Errorf("the hub's api on a slot host: %d", res.StatusCode)
	}
	// A well-formed handle nobody holds has no cookie, so no way in; a malformed one is not a slot host at all.
	if res, body := f.get("pabcdefghijkl-1."+slotDomain, f.path("/x")); res.StatusCode == 200 || strings.Contains(body, "runtime") {
		t.Errorf("a made-up handle: %d", res.StatusCode)
	}
}

func TestSlotHostsListIsForAdministratorsAndMatchesTheRealHosts(t *testing.T) {
	f := slotSetup(t)
	f.switchOn(t, "")
	list := func(p *hubtest.Person, email string) (hubtest.Resp, []string) {
		r := p.Do("GET", "/api/admin/preview-slot-hosts?email="+url.QueryEscape(email), nil)
		var out struct{ Hosts []string }
		if r.Code == 200 {
			r.JSON(&out)
		}
		return r, out.Hosts
	}
	if r, _ := list(f.other, "other@example.com"); r.Code != 403 {
		t.Fatalf("a member is not an administrator: %s", r)
	}
	if r, _ := list(f.owner, "nobody@example.com"); r.Code != 404 {
		t.Fatalf("unknown account: %s", r)
	}
	r, hosts := list(f.owner, "Other@Example.com")
	if r.Code != 200 || len(hosts) != 6 {
		t.Fatalf("list: %s %v", r, hosts)
	}
	bare := func(h string) string { return strings.Split(h, ":")[0] }
	if !strings.HasSuffix(bare(hosts[0]), "-1."+slotDomain) || !strings.HasSuffix(bare(hosts[5]), "-6."+slotDomain) {
		t.Errorf("hosts: %v", hosts)
	}
	_, _, host := f.open(t, f.other, f.path("/x"))
	if host != hosts[0] {
		t.Errorf("the listed first host %q is not the one the person gets %q", hosts[0], host)
	}
}
