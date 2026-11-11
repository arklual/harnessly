package proxy_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// orgsOn: the access list is edited through the organizations side.
var orgsOn = hubtest.Env("HUB_ORGS", "on")

// share sets who the machine is shared with: the people by id.
func (f *fixture) share(t *testing.T, ids ...string) {
	t.Helper()
	access := []map[string]string{}
	for _, id := range ids {
		access = append(access, map[string]string{"kind": "user", "id": id})
	}
	if r := f.owner.Do("PATCH", "/api/machines/"+f.id, map[string]any{"access": access}); r.Code != 200 {
		t.Fatalf("share: %s", r)
	}
}

func (f *fixture) browserSocket(t *testing.T, p *hubtest.Person) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u, _ := url.Parse(f.s.URL)
	var cookie []string
	for _, c := range p.Client.Jar.Cookies(u) {
		cookie = append(cookie, c.Name+"="+c.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws://"+u.Host+"/api/machines/"+f.id+"/browser/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookie, "; ")}}})
}

// With the setting off (the default) the list is stored and shown, and nobody is turned away by it.
func TestAccessListIsNotKeptWhileEnforcementIsOff(t *testing.T) {
	f := setup(t, orgsOn)
	listed, other := f.s.SignIn("listed@example.com"), f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, listed)
	f.owner.Join(f.spaceID, other)
	f.share(t, listed.ID)
	if r := other.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
		t.Errorf("path: %s", r)
	}
	if r := other.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 200 {
		t.Errorf("open: %s", r)
	}
}

func TestAccessListIsKeptOnEveryPathInPathMode(t *testing.T) {
	f := setup(t, orgsOn, hubtest.Env("HUB_ACCESS_ENFORCE", "on"))
	listed, other := f.s.SignIn("listed@example.com"), f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, listed)
	f.owner.Join(f.spaceID, other)

	// An empty list is the whole tenant.
	if r := other.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
		t.Fatalf("open to the tenant: %s", r)
	}
	before := f.sessionCalls()
	f.share(t, listed.ID)
	// The cache of the proxy believes "may" for a few seconds; the next person was never asked.
	third := f.s.SignIn("third@example.com")
	f.owner.Join(f.spaceID, third)

	for name, p := range map[string]*hubtest.Person{"not listed": third} {
		if r := p.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 403 || r.Err() != "machine_access" {
			t.Errorf("%s, proxied: %s", name, r)
		}
		if r := p.Do("GET", "/m/"+f.id+"/", nil, "Accept", "text/html"); r.Code != 403 {
			t.Errorf("%s, page: %s", name, r)
		}
		if r := p.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 403 || r.Err() != "machine_access" {
			t.Errorf("%s, open: %s", name, r)
		}
		if c, res, err := f.browserSocket(t, p); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			if c != nil {
				c.CloseNow()
			}
			t.Errorf("%s, browser socket: %v %v", name, res, err)
		}
	}
	if n := f.sessionCalls() - before; n != 0 {
		t.Errorf("%d runtime sessions were asked for people the list refuses", n)
	}
	for name, p := range map[string]*hubtest.Person{"listed": listed, "creator": f.owner} {
		if r := p.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
			t.Errorf("%s, proxied: %s", name, r)
		}
		if r := p.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 200 {
			t.Errorf("%s, open: %s", name, r)
		}
		c, _, err := f.browserSocket(t, p)
		if err != nil {
			t.Errorf("%s, browser socket: %v", name, err)
			continue
		}
		c.CloseNow()
	}
}

// A person the list drops out of is not let into new requests, but a chat already running is not cut.
func TestChangingTheListLeavesRunningStreamsAlone(t *testing.T) {
	f := setup(t, orgsOn, hubtest.Env("HUB_ACCESS_ENFORCE", "on"))
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	other := f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, other)

	req, _ := http.NewRequest("GET", f.s.URL+"/m/"+f.id+"/stream", nil)
	res, err := member.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 5)
	if _, err := res.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	f.share(t, other.ID)
	select {
	case <-f.streamEnded:
		t.Fatal("the stream of a person the list now leaves out was ended")
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := res.Body.Read(buf); err != nil {
		t.Errorf("the stream stopped: %v", err)
	}
}

func TestAccessListIsKeptInSubdomainMode(t *testing.T) {
	f := setup(t, orgsOn, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain), hubtest.Env("HUB_ACCESS_ENFORCE", "on"))
	listed, other := f.s.SignIn("listed@example.com"), f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, listed)
	f.owner.Join(f.spaceID, other)
	f.share(t, listed.ID)

	if r := other.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 403 || r.Err() != "machine_access" {
		t.Errorf("open: %s", r)
	}
	// A ticket cannot be had by asking, and the cookie of a person later left out of the list buys no more requests.
	host, uri := f.open(t, listed, f.id)
	o := f.machineOrigin()
	o.enter(host, uri)
	if r := o.do(host, "GET", "/m/"+f.id+"/headers"); r.Code != 200 {
		t.Fatalf("listed: %s", r)
	}
	f.share(t, other.ID)
	f.s.Clock.Advance(6 * time.Second) // the proxy believes a yes for 5 s
	if r := o.do(host, "GET", "/m/"+f.id+"/headers"); r.Code != 403 {
		t.Errorf("after the list dropped them: %s", r)
	}
}

func TestAccessWarnLetsEveryoneInAndSaysSo(t *testing.T) {
	f := setup(t, orgsOn, hubtest.Env("HUB_ACCESS_ENFORCE", "warn"))
	listed, other := f.s.SignIn("listed@example.com"), f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, listed)
	f.owner.Join(f.spaceID, other)
	f.share(t, listed.ID)
	if r := other.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
		t.Errorf("proxied: %s", r)
	}
	if r := other.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 200 {
		t.Errorf("open: %s", r)
	}
}

// The owner's rule (machaccess.FeatureOwnWork): with the switch on for a person, an administrator's role still shows the
// machine but no longer opens it, and an empty list is not an opening; the creator's listing is.
func TestOwnWorkRoleManagesButDoesNotOpen(t *testing.T) {
	f := setup(t, orgsOn, hubtest.Env("HUB_ACCESS_ENFORCE", "on"), hubtest.Env("HUB_ADMIN_EMAILS", "owner@example.com, boss@example.com"))
	boss, member := f.s.SignIn("boss@example.com"), f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, boss)
	f.owner.Join(f.spaceID, member)
	if r := f.owner.Do("PUT", "/api/admin/switches", map[string]any{"feature": "own-work", "person": "Boss@example.com", "enabled": true}); r.Code != 204 {
		t.Fatalf("switch: %s", r)
	}
	before := f.sessionCalls()
	if r := boss.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 403 || r.Err() != "own_work" {
		t.Errorf("an administrator under the owner's rule, proxied: %s", r)
	}
	if r := boss.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 403 || r.Err() != "own_work" {
		t.Errorf("an administrator under the owner's rule, open: %s", r)
	}
	if n := f.sessionCalls() - before; n != 0 {
		t.Errorf("%d runtime sessions were asked for a person the owner's rule refuses", n)
	}
	if r := boss.Do("GET", "/api/machines/"+f.id, nil); r.Code != 200 {
		t.Errorf("the machine is hidden from the administrator who manages it: %s", r)
	}
	// Without the switch the empty list is still the tenant.
	if r := member.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
		t.Errorf("a member without the switch: %s", r)
	}
	if r := f.owner.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
		t.Errorf("the creator: %s", r)
	}
	f.share(t, f.owner.ID, boss.ID)
	if r := boss.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 200 {
		t.Errorf("listed by the creator: %s", r)
	}
}
