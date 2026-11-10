package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// The organization's rule for apps «Не спрашивать разрешений» is its admin's, by a person's session: a
// machine's signature or a session of its store (what an agent on the machine can reach) does not change it.
func TestAppsConsentRuleIsTheAdminsBySession(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com") // the first account administers the instance
	anna := s.SignIn("anna@example.com")
	vera := s.SignIn("vera@example.com")
	space, _ := anna.NewSpace("Example")
	anna.Join(space, vera)
	var made struct{ Org struct{ ID string } }
	if r := anna.Do("POST", "/api/orgs", map[string]any{"name": "Example", "spaceIds": []string{space}}); r.Code != 201 {
		t.Fatalf("org: %s", r)
	} else {
		r.JSON(&made)
	}
	path := "/api/orgs/" + made.Org.ID
	rule := func() string {
		t.Helper()
		var v struct{ Org struct{ AppsConsent string } }
		if r := anna.Do("GET", path, nil); r.Code != 200 {
			t.Fatalf("get: %s", r)
		} else {
			r.JSON(&v)
		}
		return v.Org.AppsConsent
	}
	if got := rule(); got != "ask" {
		t.Fatalf("the default rule = %q, want ask", got)
	}

	m := anna.Machine(space, nil)
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	dial, _ := hubproto.SignDial(m.Priv, m.MachineID, s.Clock.Now(), host)
	sess, err := storelink.RequestSession(context.Background(), http.DefaultClient,
		storelink.Machine{ID: m.MachineID, DialURL: dialURL(s), Key: m.Priv}, s.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"appsConsent": "auto"}
	for name, auth := range map[string]string{"the machine's signature": dial, "the machine's store session": "Bearer " + sess.Token} {
		if r := s.Browser().Do("PATCH", path, body, "Authorization", auth); r.Code != http.StatusUnauthorized {
			t.Errorf("%s changed the rule: %s", name, r)
		}
	}
	if r := vera.Do("PATCH", path, body); r.Code != http.StatusForbidden {
		t.Errorf("a member changed the rule: %s", r)
	}
	if got := rule(); got != "ask" {
		t.Fatalf("the rule changed without its admin: %q", got)
	}
	if r := anna.Do("PATCH", path, map[string]any{"appsConsent": "always"}); r.Code != http.StatusBadRequest {
		t.Errorf("an unknown rule: %s", r)
	}
	var patched struct{ Org struct{ AppsConsent string } }
	if r := anna.Do("PATCH", path, body); r.Code != 200 {
		t.Fatalf("the admin's patch: %s", r)
	} else if r.JSON(&patched); patched.Org.AppsConsent != "auto" {
		t.Fatalf("patched = %+v", patched.Org)
	}
	if got := rule(); got != "auto" {
		t.Fatalf("the rule after the admin's patch = %q", got)
	}
}
