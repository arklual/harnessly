package designaccess

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A board from the store is opened at an address with its key, and its files inherit it: the key opens only
// the person's own read of that project, and only for a while.
func TestFileKeyOpensOnlyItsPersonAndProject(t *testing.T) {
	k := newFileKeyer([]byte("master"))
	now := time.Unix(1_800_000_000, 0)
	key := k.mint("u1", "shop", now)
	if !k.check(key, "u1", "shop", now.Add(time.Hour)) {
		t.Fatalf("own key refused: %q", key)
	}
	for name, ok := range map[string]bool{
		"another person":  k.check(key, "u2", "shop", now),
		"another project": k.check(key, "u1", "shop-2", now),
		"past its end":    k.check(key, "u1", "shop", now.Add(fileKeyTTL+fileKeyStep+time.Minute)),
		"another hub":     newFileKeyer([]byte("other")).check(key, "u1", "shop", now),
		"no key":          k.check("", "u1", "shop", now),
		"cut key":         k.check(key[:len(key)-2], "u1", "shop", now),
	} {
		if ok {
			t.Errorf("%s: the key opened", name)
		}
	}
	// One address for hours, so the board's files stay in the browser's cache between openings.
	if again := k.mint("u1", "shop", now.Add(time.Minute)); again != key && k.mint("u1", "shop", now.Add(-time.Minute)) != key {
		t.Errorf("the key changed within its step: %q, %q", key, again)
	}
	if newFileKeyer(nil).mint("u1", "shop", now) != "" {
		t.Error("a hub without its secret mints no key")
	}
	if !storeKeyShape(key) {
		t.Errorf("the proxy's pattern must match the key: %q", key)
	}
}

// The plain address of a board leads to the keyed one beside it, whatever prefix the browser used.
func TestKeyedLocationResolvesBesideTheBoard(t *testing.T) {
	base, _ := url.Parse("https://hub.example.com/agents/api/platform/design/store/shop/f/dir/%D0%93%D0%BB%D0%B0%D0%B2%D0%BD%D0%B0%D1%8F.dc.html?rev=3")
	loc := keyedLocation([]string{"dir", "Главная.dc.html"}, "mz0.AbC", "rev=3")
	to, err := base.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/agents/api/platform/design/store/shop/k/mz0.AbC/f/dir/Главная.dc.html"; to.Path != want || to.RawQuery != "rev=3" {
		t.Errorf("%s -> %s ?%s, want %s", loc, to.Path, to.RawQuery, want)
	}
}

// The board's files come from its sandbox without the hub's session: the key itself says whose read it is,
// and only a live key of that project names anyone.
func TestKeyedFileUserIsTheKeysPerson(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := &Service{files: newFileKeyer([]byte("master")), now: func() time.Time { return now }}
	key := s.files.mint("tyqhce6nuauhwtzi3eoi", "shop", now)
	path := "/agents/api/platform/design/store/shop/k/" + key + "/f/lib/support.js"
	if got := s.KeyedFileUser(httptest.NewRequest("GET", path, nil)); got != "tyqhce6nuauhwtzi3eoi" {
		t.Fatalf("own key: %q", got)
	}
	end, rest, _ := strings.Cut(key, ".")
	_, sum, _ := strings.Cut(rest, ".")
	for name, c := range map[string]struct{ method, path string }{
		"another person in the key": {"GET", "/agents/api/platform/design/store/shop/k/" + end + ".u2." + sum + "/f/lib/support.js"},
		"another project":           {"GET", "/agents/api/platform/design/store/shop-2/k/" + key + "/f/lib/support.js"},
		"a write":                   {"POST", path},
		"not a file":                {"GET", "/agents/api/platform/design/store/shop/k/" + key + "/head"},
		"the old shape":             {"GET", "/agents/api/platform/design/store/shop/k/" + end + "." + sum + "/f/lib/support.js"},
	} {
		if got := s.KeyedFileUser(httptest.NewRequest(c.method, c.path, nil)); got != "" {
			t.Errorf("%s: signed in as %q", name, got)
		}
	}
	if got := (&Service{now: s.now}).KeyedFileUser(httptest.NewRequest("GET", path, nil)); got != "" {
		t.Errorf("a hub without its secret: %q", got)
	}
}
