package api_test

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// HRN-979: with HUB_APPS_GUEST_URL the guest links of apps run on an origin of their own and nothing of the hub does.
func TestAppsGuestHostServesOnlyGuestLinks(t *testing.T) {
	const guest = "guest.example.com"
	s := hubtest.New(t, hubtest.Env("HUB_APPS_GUEST_URL", "http://"+guest))
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}
	b := s.Browser()
	ended := func(r hubtest.Resp) bool {
		return r.Code == 404 && strings.Contains(string(r.Body), "This link has ended.")
	}

	if r := b.DoAt(s.URL, guest, "GET", "/apps/guest/unknown/", nil); !ended(r) {
		t.Errorf("a link on the guest host goes to the guest view: %s", r)
	}
	for _, path := range []string{"/api/me", "/", "/agent/v1"} {
		if r := b.DoAt(s.URL, guest, "GET", path, nil); r.Code != 404 || ended(r) {
			t.Errorf("%s on the guest host must be nothing of the hub: %s", path, r)
		}
	}
	if r := b.Do("GET", "/apps/guest/unknown/", nil); ended(r) {
		t.Errorf("the hub's own origin must not serve guest links when they have a host: %s", r)
	}
}

func TestAppsGuestHostIsNotTheHubsOwn(t *testing.T) {
	// Without the variable the links stay on the hub's origin.
	s := hubtest.New(t)
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}
	if r := s.Browser().Do("GET", "/apps/guest/unknown/", nil); !strings.Contains(string(r.Body), "This link has ended.") {
		t.Errorf("default: %s", r)
	}
}
