package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestSafeAccessOriginCanonical(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://EXAMPLE.com", "https://example.com"},
		{"HTTPS://Example.COM:443/", "https://example.com"},
		{"https://example.com:00443", "https://example.com"},
		{"https://example.com:08443/", "https://example.com:8443"},
		{"https://sub.example.com", "https://sub.example.com"},
		{"https://[::1]:9443", "https://[::1]:9443"},
		{"http://localhost", "http://localhost"},
		{"HTTP://LOCALHOST:80/", "http://localhost"},
		{"http://localhost:00080", "http://localhost"},
		{"http://127.0.0.1:443", "http://127.0.0.1:443"},
		{"http://localhost:00443", "http://localhost:443"},
		{"http://127.0.0.1:03000/", "http://127.0.0.1:3000"},
		{"http://[::1]:80/", "http://[::1]"},
		{"http://[::1]:3000", "http://[::1]:3000"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got := Origin(tc.raw)
			if got != tc.want {
				t.Errorf("Origin(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if got != "" && Origin(got) != got {
				t.Errorf("canonical origin is not idempotent: %q", got)
			}
		})
	}
	for _, raw := range []string{
		"", "example.com", "//example.com", " https://example.com", "https://example.com ",
		"http://example.com", "http://127.0.0.2", "http://0.0.0.0", "http://127.1", "http://2130706433",
		"http://localhost.example.com", "http://sub.localhost", "http://localhost.", "http://[::ffff:127.0.0.1]",
		"ftp://localhost", "ws://localhost", "wss://example.com", "file:///localhost", "javascript:example.com",
		"https://", "https://user:pass@example.com", "http://user@localhost", "https://example.com@evil.example",
		"https://example.com/path", "http://localhost/path", "https://example.com//", "https://example.com/?q=1",
		"https://example.com?", "https://example.com#", "http://localhost#fragment", "http://localhost?query=1",
		"https://example.com.", "https://*.example.com", "https://exa_mple.com", "https://example.com\\evil",
		"https://example.com:0", "https://example.com:65536", "https://example.com:-1", "https://example.com:abc",
		"http://localhost:0", "http://localhost:65536", "http://localhost:abc", "https://example.com\n",
		"https://%65xample.com", "https://example.com%2fevil", "https://" + strings.Repeat("a", 256),
	} {
		t.Run("reject/"+raw, func(t *testing.T) {
			if got := Origin(raw); got != "" {
				t.Errorf("Origin(%q) accepted as %q", raw, got)
			}
		})
	}
}

func accessBytes(t *testing.T, dir string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "safe.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func accessPut(t *testing.T, s *Store, name string, origins, projects []string) {
	t.Helper()
	if _, _, err := s.Put(name, fakeValue); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Metadata(name, origins, projects); err != nil {
		t.Fatal(err)
	}
}

func TestSafeAccessMetadataIsPublicAndAtomic(t *testing.T) {
	s, dir, key := newStore(t)
	accessPut(t, s, "service-key", nil, nil)
	before, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Metadata("service-key", []string{"HTTPS://EXAMPLE.COM:443/", "https://example.com", "http://LOCALHOST:80"}, []string{"project-a", "project-a", "project-b"})
	if err != nil || !slices.Equal(v.Origins, []string{"https://example.com", "http://localhost"}) || !slices.Equal(v.Projects, []string{"project-a", "project-b"}) {
		t.Fatalf("canonical metadata: %+v, %v", v, err)
	}
	after, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Secrets[0].Nonce, after.Secrets[0].Nonce) || !bytes.Equal(before.Secrets[0].Box, after.Secrets[0].Box) {
		t.Fatal("changing public metadata resealed the value")
	}
	unchanged := accessBytes(t, dir)
	for _, tc := range []struct {
		origins, projects []string
		want              string
	}{
		{[]string{"https://valid.example.com", "http://example.com"}, []string{"project-c"}, "bad-origin"},
		{nil, []string{"project-c", ""}, "bad-project"},
		{nil, []string{" project-c"}, "bad-project"},
		{nil, []string{"project-c "}, "bad-project"},
		{nil, []string{"project\nforged"}, "bad-project"},
		{nil, []string{"project\rforged"}, "bad-project"},
		{nil, []string{"project\x00forged"}, "bad-project"},
		{nil, []string{strings.Repeat("p", 201)}, "bad-project"},
	} {
		if _, err := s.Metadata("service-key", tc.origins, tc.projects); ReasonOf(err) != tc.want {
			t.Errorf("invalid metadata: %v, want %s", err, tc.want)
		}
		if !bytes.Equal(unchanged, accessBytes(t, dir)) {
			t.Fatal("invalid metadata partially persisted")
		}
	}
	if _, err := s.Metadata("absent", nil, nil); ReasonOf(err) != ReasonNotFound {
		t.Errorf("missing metadata: %v", err)
	}
	// Neither listing nor metadata lookup needs the encryption key.
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	views, err := s.List()
	if err != nil || len(views) != 1 {
		t.Fatalf("list without key: %v", err)
	}
	lookup, err := s.Lookup("service-key", "project-a")
	if err != nil || !reflect.DeepEqual(views[0], lookup) {
		t.Fatalf("public lookup: %+v, %v", lookup, err)
	}
	raw, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{fakeValue, `"value"`, `"box"`, `"nonce"`} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Error("public view exposes value or encryption material")
		}
	}
}

func TestSafeAccessLegacyFileAndExactProjects(t *testing.T) {
	s, dir, key := newStore(t)
	if _, _, err := s.Put("legacy-key", fakeValue); err != nil {
		t.Fatal(err)
	}
	// Explicitly remove new fields to exercise the persisted v1 format rather than a new struct's defaults.
	var old map[string]any
	if err := json.Unmarshal(accessBytes(t, dir), &old); err != nil {
		t.Fatal(err)
	}
	for _, item := range old["secrets"].([]any) {
		e := item.(map[string]any)
		delete(e, "origins")
		delete(e, "projects")
	}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "safe.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s = Open(dir, key)
	for _, project := range []string{"", "project-a", "project-b"} {
		got, err := s.Reveal([]string{"legacy-key"}, Use{Project: project})
		if err != nil || got["legacy-key"] != fakeValue {
			t.Fatalf("v1 unscoped secret refused project %q: %v", project, err)
		}
	}
	accessPut(t, s, "scoped-key", nil, []string{"project-a"})
	for _, project := range []string{"", "project-b", "PROJECT-A", "project-a/child", "project-a ", " project-a"} {
		_, hidden := s.Lookup("scoped-key", project)
		_, missing := s.Lookup("absent-key", project)
		if hidden == nil || missing == nil || hidden.Error() != missing.Error() || ReasonOf(hidden) != ReasonNotFound {
			t.Errorf("foreign lookup differs from absent for project %q", project)
		}
		got, denied := s.Reveal([]string{"scoped-key"}, Use{Project: project})
		_, absent := s.Reveal([]string{"absent-key"}, Use{Project: project})
		if got != nil || denied == nil || ReasonOf(denied) != ReasonOf(absent) || ReasonOf(denied) != ReasonNotFound {
			t.Errorf("foreign reveal differs from absent for project %q", project)
		}
	}
	if got, err := s.Reveal([]string{"scoped-key"}, Use{Project: "project-a"}); err != nil || got["scoped-key"] != fakeValue {
		t.Fatalf("exact project refused: %v", err)
	}
}

func TestSafeAccessRevealOriginDistinctions(t *testing.T) {
	s, dir, _ := newStore(t)
	accessPut(t, s, "web-key", []string{"https://example.com", "http://localhost:3000"}, []string{"project-a"})
	for _, origin := range []string{
		"https://sub.example.com", "https://example.com:8443", "https://example.com.evil.example", "http://example.com",
		"https://other.example.com", "http://localhost", "http://localhost:3001", "https://localhost:3000",
		"http://127.0.0.1:3000", "http://[::1]:3000",
	} {
		before := accessBytes(t, dir)
		got, err := s.Reveal([]string{"web-key"}, Use{Project: "project-a", Origin: origin})
		if got != nil || ReasonOf(err) != "origin-denied" {
			t.Errorf("origin %q was not denied: %v", origin, err)
		}
		if !bytes.Equal(before, accessBytes(t, dir)) {
			t.Error("denied origin changed usage metadata")
		}
	}
	for _, origin := range []string{"https://example.com", "http://localhost:3000", ""} {
		got, err := s.Reveal([]string{"web-key"}, Use{Project: "project-a", Origin: origin, Conv: "chat-a"})
		if err != nil || got["web-key"] != fakeValue {
			t.Errorf("allowed origin %q refused: %v", origin, err)
		}
	}
}

func TestSafeAccessRevealBatchIsAtomic(t *testing.T) {
	for _, failure := range []string{"foreign-project", "missing", "wrong-origin", "corrupt-value"} {
		t.Run(failure, func(t *testing.T) {
			s, dir, _ := newStore(t)
			accessPut(t, s, "first-key", []string{"https://example.com"}, []string{"project-a"})
			accessPut(t, s, "second-key", []string{"https://example.com"}, []string{"project-a"})
			name, want := "second-key", ReasonNotFound
			switch failure {
			case "foreign-project":
				_, err := s.Metadata(name, []string{"https://example.com"}, []string{"project-b"})
				if err != nil {
					t.Fatal(err)
				}
			case "missing":
				name = "absent-key"
			case "wrong-origin":
				_, err := s.Metadata(name, []string{"https://other.example.com"}, []string{"project-a"})
				if err != nil {
					t.Fatal(err)
				}
				want = "origin-denied"
			case "corrupt-value":
				f, err := s.read()
				if err != nil {
					t.Fatal(err)
				}
				f.Secrets[1].Box[0] ^= 1
				if err := s.write(f); err != nil {
					t.Fatal(err)
				}
				want = ReasonKeyLost
			}
			before := accessBytes(t, dir)
			for _, names := range [][]string{{"first-key", name}, {name, "first-key"}} {
				got, err := s.Reveal(names, Use{Project: "project-a", Origin: "https://example.com", Conv: "chat-a"})
				if got != nil || ReasonOf(err) != want {
					t.Errorf("batch must return no partial values: reason=%s", ReasonOf(err))
				}
				if !bytes.Equal(before, accessBytes(t, dir)) {
					t.Error("failed batch changed the safe, including usage history")
				}
			}
		})
	}
}

func TestSafeAccessGrantPreservesValueAndOtherScopes(t *testing.T) {
	s, dir, _ := newStore(t)
	accessPut(t, s, "service-key", []string{"https://example.com"}, []string{"project-a"})
	before, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Grant("service-key", "project-b", "https://other.example.com"); err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	x, y := before.Secrets[0], after.Secrets[0]
	if !slices.Equal(y.Projects, []string{"project-a", "project-b"}) || !slices.Equal(y.Origins, []string{"https://example.com", "https://other.example.com"}) {
		t.Fatalf("grant replaced or duplicated scope: %+v", y.View)
	}
	if !bytes.Equal(x.Box, y.Box) || !bytes.Equal(x.Nonce, y.Nonce) || x.CreatedAt != y.CreatedAt || !reflect.DeepEqual(x.UsedBy, y.UsedBy) {
		t.Error("grant changed the sealed value or its history")
	}
	unchanged := accessBytes(t, dir)
	for _, tc := range []struct{ name, project, origin, reason string }{
		{"service-key", "", "", ReasonNotFound},
		{"absent", "project-c", "", ReasonNotFound},
		{"service-key", "project-c", "http://example.com", "bad-origin"},
	} {
		if err := s.Grant(tc.name, tc.project, tc.origin); ReasonOf(err) != tc.reason {
			t.Errorf("grant refusal: %v", err)
		}
		if !bytes.Equal(unchanged, accessBytes(t, dir)) {
			t.Error("rejected grant changed the safe")
		}
	}
	for _, project := range []string{"project-a", "project-b"} {
		if got, err := s.Reveal([]string{"service-key"}, Use{Project: project}); err != nil || got["service-key"] != fakeValue {
			t.Errorf("grant changed value or denied %q: %v", project, err)
		}
	}
}

func TestSafeAccessPutAskedCannotOverwriteForeignEntry(t *testing.T) {
	s, dir, _ := newStore(t)
	a := Ask{Name: "service-key", Project: "project-a", Origin: "https://example.com", Replace: true}
	// Another project creates the name after the normal card was opened, before it is answered.
	accessPut(t, s, a.Name, []string{"https://other.example.com"}, []string{"project-b"})
	before := accessBytes(t, dir)
	if _, _, err := s.PutAsked(a, fakeValue+"replacement"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("foreign normal card overwrite: %v", err)
	}
	a.Grant = true
	if _, _, err := s.PutAsked(a, fakeValue+"replacement"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("grant card accepted a value: %v", err)
	}
	if !bytes.Equal(before, accessBytes(t, dir)) {
		t.Error("rejected card changed the safe")
	}
	if got, err := s.Reveal([]string{a.Name}, Use{Project: "project-b"}); err != nil || got[a.Name] != fakeValue {
		t.Errorf("foreign value changed: %v", err)
	}
	if v, created, err := s.PutAsked(Ask{Name: "new-key", Project: "project-a", Origin: "https://example.com"}, fakeValue); err != nil || !created || !slices.Equal(v.Projects, []string{"project-a"}) || !slices.Equal(v.Origins, []string{"https://example.com"}) {
		t.Errorf("new card did not scope its value: %+v, %v", v, err)
	}
}

func TestSafeAccessAsksKeepApprovalsSeparate(t *testing.T) {
	a := NewAsks()
	base := Ask{Name: "service-key", Conv: "chat-a", Project: "project-a", Origin: "https://example.com", Why: "sign in", Grant: true, Replace: true}
	first, state, err := a.OpenScoped(base)
	if err != nil || state != AskWaiting {
		t.Fatalf("open: %s, %v", state, err)
	}
	if first.Replace {
		t.Error("grant card retained a request to replace the value")
	}
	again, _, err := a.OpenScoped(base)
	if err != nil || again.ID != first.ID {
		t.Fatal("identical request did not reuse its card")
	}
	var others []Ask
	for _, field := range []string{"conv", "project", "origin", "grant"} {
		x := base
		switch field {
		case "conv":
			x.Conv = "chat-b"
		case "project":
			x.Project = "project-b"
		case "origin":
			x.Origin = "https://other.example.com"
		case "grant":
			x.Grant = false
		}
		card, state, err := a.OpenScoped(x)
		if err != nil || state != AskWaiting || card.ID == first.ID {
			t.Fatalf("%s did not get independent approval: %v", field, err)
		}
		others = append(others, card)
	}
	a.Ready(first.ID)
	if a.Wait(context.Background(), first.ID, 0) != AskReady {
		t.Fatal("answered card did not settle")
	}
	if _, ok := a.Get(first.ID); ok {
		t.Error("settled card can be answered twice")
	}
	for _, card := range others {
		if a.Wait(context.Background(), card.ID, 0) != AskWaiting {
			t.Error("answer settled another scope's card")
		}
	}
	a.Fulfil(base.Name)
	for _, card := range others {
		want := AskWaiting
		if !card.Grant {
			want = AskReady
		}
		if a.Wait(context.Background(), card.ID, 0) != want {
			t.Error("put settled a grant card or left a value card waiting")
		}
	}
}
