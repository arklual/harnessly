package sites

import "testing"

func TestValidSiteName(t *testing.T) {
	good := []string{"shop", "a", "my-site-2", "0day", "x" + "yz"}
	for _, n := range good {
		if err := ValidSiteName(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	bad := []string{"", "-a", "a-", "a--b", "A", "a_b", "a.b", "www", "api", "admin", "login", "harnessly", "sinopia",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "привет"}
	for _, n := range bad {
		if err := ValidSiteName(n); err == nil {
			t.Errorf("%q should be refused", n)
		}
	}
}

func TestValidSpaceLabel(t *testing.T) {
	for _, n := range []string{"acme", "my-team", "abc"} {
		if err := ValidSpaceLabel(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", "a--b", "-a", "aaaaaaaaaaaaaaaaaaaaaaaaa", "Acme"} {
		if err := ValidSpaceLabel(n); err == nil {
			t.Errorf("%q should be refused", n)
		}
	}
}

func TestParseHost(t *testing.T) {
	cases := []struct {
		host string
		want Addr
		ok   bool
	}{
		{"shop--acme.sites.example.com", Addr{Site: "shop", Space: "acme", Kind: KindCurrent}, true},
		{"Shop--Acme.sites.example.com:8443", Addr{Site: "shop", Space: "acme", Kind: KindCurrent}, true},
		{"shop--v7--acme.sites.example.com", Addr{Site: "shop", Space: "acme", Kind: KindVersion, N: 7}, true},
		{"shop--pr-12--acme.sites.example.com", Addr{Site: "shop", Space: "acme", Kind: KindPreview, N: 12}, true},
		{"shop--acme.sites.example.com.", Addr{Site: "shop", Space: "acme", Kind: KindCurrent}, true},
		{"sites.example.com", Addr{}, false},
		{"shop.sites.example.com", Addr{}, false},
		{"shop--acme.other.example.com", Addr{}, false},
		{"a.shop--acme.sites.example.com", Addr{}, false},
		{"shop--v0--acme.sites.example.com", Addr{}, false},
		{"shop--vx--acme.sites.example.com", Addr{}, false},
		{"shop--pr-0--acme.sites.example.com", Addr{}, false},
		{"shop--a--b--acme.sites.example.com", Addr{}, false},
		{"--acme.sites.example.com", Addr{}, false},
		{"shop--.sites.example.com", Addr{}, false},
		{"shop--acme.sites.example.com.evil.com", Addr{}, false},
	}
	for _, c := range cases {
		got, ok := ParseHost(c.host, "sites.example.com")
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: got %+v %v, want %+v %v", c.host, got, ok, c.want, c.ok)
		}
	}
}

func TestAddrHostRoundTrip(t *testing.T) {
	for _, a := range []Addr{
		{Site: "shop", Space: "acme", Kind: KindCurrent},
		{Site: "shop", Space: "acme", Kind: KindVersion, N: 3},
		{Site: "shop", Space: "acme", Kind: KindPreview, N: 12},
	} {
		h := a.Host("s.example.com")
		got, ok := ParseHost(h, "s.example.com")
		if !ok || got != a {
			t.Errorf("%s: %+v %v", h, got, ok)
		}
	}
}
