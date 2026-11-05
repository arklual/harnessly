package preview

import (
	"net/http/httptest"
	"testing"
)

func TestResolveListedOpensOnlyPrivateNetworks(t *testing.T) {
	lookup := func(answers ...string) Lookup {
		return func(string) ([]string, error) { return answers, nil }
	}
	self := map[string]bool{"100.64.0.9": true}
	for name, c := range map[string]struct {
		answers []string
		want    string
	}{
		"overlay":    {[]string{"100.64.0.7"}, ""},
		"office":     {[]string{"10.1.2.3", "192.168.1.5"}, ""},
		"public":     {[]string{"203.0.113.10"}, "forbidden"}, // a documentation range stays special
		"loopback":   {[]string{"100.64.0.7", "127.0.0.1"}, "forbidden"},
		"link-local": {[]string{"169.254.169.254"}, "forbidden"},
		"own":        {[]string{"100.64.0.9"}, "forbidden"},
		"ula":        {[]string{"fd00::1"}, ""},
		"v6 local":   {[]string{"fe80::1"}, "forbidden"},
	} {
		got := ResolveListed("gitlab.example.ru", lookup(c.answers...), self)
		if got.Error != c.want {
			t.Errorf("%s: %+v, want %q", name, got, c.want)
		}
	}
	if got := ResolveListed("gitlab.example.ru", lookup("8.8.8.8"), self); got.Error != "" || got.Address != "8.8.8.8" {
		t.Errorf("a public answer: %+v", got)
	}
	if got := ResolvePublic("gitlab.example.ru", lookup("100.64.0.7"), self); got.Error != "forbidden" {
		t.Errorf("an unlisted host into the overlay: %+v", got)
	}
}

func TestPrivateHostsTakesNamesOnly(t *testing.T) {
	got := PrivateHosts([]string{" GitLab.Example.RU. ", "10.0.0.1", "localhost", "a.localhost", "x.ru:443", "", "u@x.ru"})
	if len(got) != 1 || !got["gitlab.example.ru"] {
		t.Fatalf("got %v", got)
	}
}

func TestListedPrivateHostOpensOnlyOnItsOwnPort(t *testing.T) {
	secret := []byte("s3cret-s3cret-s3cret-s3cret-s3cret")
	p := New(Config{
		Path: "/proxy", ClaimHeader: "x-acme-preview", Audience: "acme-preview-proxy", Secret: secret,
		PrivateHeaderPrefix: "x-acme-", PrivateHosts: []string{"gitlab.example.ru"},
		Resolve: func(string) Resolved { return Resolved{Address: "100.64.0.7", Family: 4} },
	})
	admitted := func(origin string) int {
		claim := `{"sub":"alice","target":{"kind":"url","origin":"` + origin + `"},"slotOrigin":"https://ai-alice-7.example.org","portSlots":[],"parentOrigin":"https://app.example.org"}`
		r := httptest.NewRequest("GET", "/proxy/", nil)
		r.Header.Set("x-acme-preview", signClaim(secret, []byte(claim)))
		return p.admit(r).status
	}
	if got := admitted("https://gitlab.example.ru"); got != 0 {
		t.Errorf("own port: status %d", got)
	}
	if got := admitted("https://gitlab.example.ru:8443"); got == 401 {
		t.Fatal("the claim of the test is not valid")
	}
	if got := admitted("https://gitlab.example.ru:8443"); got != 403 {
		t.Errorf("another port: status %d, want 403", got)
	}
}
