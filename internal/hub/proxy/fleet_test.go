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

var fleetOn = hubtest.Env("HUB_FEATURES_FORCE", "runtime.fleet")

type fleetItems = []struct {
	ID      string `json:"id"`
	Reach   string `json:"reach"`
	OrgName string `json:"orgName"`
}

type fleetList struct {
	Machines []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Reach   string `json:"reach"`
		Status  string `json:"status"`
		OrgName string `json:"orgName"`
		Tenant  string `json:"tenant"`
	} `json:"machines"`
}

func (l fleetList) has(id string) bool {
	for _, m := range l.Machines {
		if m.ID == id {
			return true
		}
	}
	return false
}

// second adds the owner's second machine, in a space of its own.
func (f *fixture) second(t *testing.T) string {
	t.Helper()
	space, _ := f.owner.NewSpace("Вторая")
	return f.owner.Machine(space, f.runtime()).MachineID
}

func TestFleetIsDarkWithoutTheFlag(t *testing.T) {
	f := setup(t)
	if r := f.owner.Do("GET", "/api/v2/machines", nil); r.Code != 404 {
		t.Errorf("list: %s", r)
	}
	if r := f.owner.Do("GET", "/api/v2/ping?machine="+f.id, nil); r.Code != 404 {
		t.Errorf("proxy: %s", r)
	}
	if r := f.owner.Do("GET", "/api/me/machines", nil); r.Code != 404 {
		t.Errorf("me/machines: %s", r)
	}
}

func TestFleetListsEveryMachineOfThePersonAndOnlyThose(t *testing.T) {
	f := setup(t, fleetOn)
	other := f.second(t)
	stranger := f.s.SignIn("stranger@example.com")
	stranger.NewSpace("Чужая") // a space of their own, with the flag on and no machine in it

	var me fleetList
	if r := f.owner.Do("GET", "/api/me/machines", nil); r.Code != 200 {
		t.Fatalf("me/machines: %s", r)
	} else {
		r.JSON(&me)
	}
	if !me.has(f.id) || !me.has(other) || len(me.Machines) != 2 {
		t.Errorf("the owner sees %+v", me.Machines)
	}
	var v2 fleetItems
	if r := f.owner.Do("GET", "/api/v2/machines", nil); r.Code != 200 {
		t.Fatalf("v2 machines: %s", r)
	} else {
		r.JSON(&v2)
	}
	if len(v2) != 2 {
		t.Errorf("the runtime list is %+v", v2)
	}
	for _, m := range v2 {
		if m.Reach != "online" {
			t.Errorf("%s reach %q", m.ID, m.Reach)
		}
	}

	// A person with no access: no machine in either list, and the proxy refuses both.
	var none fleetList
	if r := stranger.Do("GET", "/api/me/machines", nil); r.Code != 200 {
		t.Fatalf("me/machines: %s", r)
	} else if r.JSON(&none); len(none.Machines) != 0 {
		t.Errorf("me/machines shows a stranger %+v", none.Machines)
	}
	var noneV2 fleetItems
	if r := stranger.Do("GET", "/api/v2/machines", nil); r.Code != 200 {
		t.Fatalf("v2 machines: %s", r)
	} else if r.JSON(&noneV2); len(noneV2) != 0 {
		t.Errorf("v2 machines shows a stranger %+v", noneV2)
	}
	before := f.sessionCalls()
	for _, id := range []string{f.id, other} {
		if r := stranger.Do("GET", "/api/v2/ping?machine="+id, nil); r.Code != 403 {
			t.Errorf("proxy for a stranger, %s: %s", id, r)
		}
	}
	if n := f.sessionCalls() - before; n != 0 {
		t.Errorf("%d runtime sessions were asked for a stranger", n)
	}
}

func TestFleetProxyReachesTheChosenMachine(t *testing.T) {
	f := setup(t, fleetOn)
	other := f.second(t)
	for _, id := range []string{f.id, other} {
		r := f.owner.Do("GET", "/api/v2/ping?machine="+id+"&x=1", nil)
		if r.Code != 200 || !strings.Contains(r.String(), "runtime /api/v2/ping") {
			t.Errorf("%s: %s", id, r)
		}
	}
	if r := f.owner.Do("GET", "/api/v2/ping", nil); r.Code != 404 {
		t.Errorf("no machine named: %s", r)
	}
	if r := f.owner.Do("GET", "/api/v2/me", nil); r.Code != 200 || strings.Contains(r.String(), "runtime /api/v2/me") || !strings.Contains(r.String(), "owner@example.com") {
		t.Errorf("the person's settings are the hub's, not a machine's (HRN-890): %s", r)
	}
	if r := f.owner.Do("GET", "/api/v2/ping?machine=m_nothing", nil); r.Code != 404 && r.Code != 403 {
		t.Errorf("a machine that is not there: %s", r)
	}
}

func TestFleetKeepsTheAccessListOfAMachine(t *testing.T) {
	f := setup(t, orgsOn, fleetOn, hubtest.Env("HUB_ACCESS_ENFORCE", "on"))
	listed, other := f.s.SignIn("listed@example.com"), f.s.SignIn("other@example.com")
	f.owner.Join(f.spaceID, listed)
	f.owner.Join(f.spaceID, other)
	f.share(t, listed.ID)

	var l fleetItems
	r := other.Do("GET", "/api/v2/machines", nil)
	r.JSON(&l)
	if r.Code != 200 || len(l) != 0 {
		t.Errorf("a member the list leaves out sees the machine: %s", r)
	}
	if r := other.Do("GET", "/api/v2/ping?machine="+f.id, nil); r.Code != 403 || r.Err() != "machine_access" {
		t.Errorf("proxy: %s", r)
	}
	l = nil
	r = listed.Do("GET", "/api/v2/machines", nil)
	r.JSON(&l)
	if len(l) != 1 || l[0].ID != f.id {
		t.Errorf("the listed member does not see it: %s", r)
	}
	if r := listed.Do("GET", "/api/v2/ping?machine="+f.id, nil); r.Code != 200 {
		t.Errorf("listed proxy: %s", r)
	}
}

func (f *fixture) v2Socket(t *testing.T, p *hubtest.Person, machine string) (string, *http.Response, error) {
	t.Helper()
	u, _ := url.Parse(f.s.URL)
	var cookie []string
	for _, c := range p.Client.Jar.Cookies(u) {
		cookie = append(cookie, c.Name+"="+c.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, "ws://"+u.Host+"/api/v2/socket?machine="+machine+"&x=1", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookie, "; ")}}})
	if err != nil {
		return "", res, err
	}
	defer c.CloseNow()
	_, msg, err := c.Read(ctx)
	return string(msg), res, err
}

func TestFleetSocketGoesToTheMachineOnlyForThoseWhoMayOpenIt(t *testing.T) {
	f := setup(t, fleetOn)
	stranger := f.s.SignIn("stranger@example.com")
	stranger.NewSpace("Чужая")
	if msg, _, err := f.v2Socket(t, f.owner, f.id); err != nil || msg != "v2:x=1" {
		t.Errorf("owner: %q %v (the machine parameter must not reach the runtime)", msg, err)
	}
	if _, res, err := f.v2Socket(t, stranger, f.id); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("stranger: %v %v", res, err)
	}
}
