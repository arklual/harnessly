package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// memKV is a KV engine in memory.
type memKV struct {
	mu   sync.Mutex
	data map[string]map[string]string
	down bool
}

func newMemKV() *memKV { return &memKV{data: map[string]map[string]string{}} }

func (m *memKV) Read(_ context.Context, p string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down {
		return nil, refuse(ReasonUnavailable, errors.New("down"))
	}
	d, ok := m.data[p]
	if !ok {
		return nil, refuse(ReasonNotFound, nil)
	}
	out := map[string]string{}
	for k, v := range d {
		out[k] = v
	}
	return out, nil
}

func (m *memKV) Write(_ context.Context, p string, d map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down {
		return refuse(ReasonUnavailable, errors.New("down"))
	}
	m.data[p] = d
	return nil
}

func (m *memKV) Delete(_ context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, p)
	return nil
}

func (m *memKV) List(_ context.Context, p string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down {
		return nil, refuse(ReasonUnavailable, errors.New("down"))
	}
	seen := map[string]bool{}
	for k := range m.data {
		if rest, ok := strings.CutPrefix(k, p+"/"); ok {
			if i := strings.Index(rest, "/"); i >= 0 {
				seen[rest[:i+1]] = true
			} else {
				seen[rest] = true
			}
		}
	}
	out := []string{}
	for k := range seen {
		out = append(out, k)
	}
	return out, nil
}

func newSplitSafe(t *testing.T) (*Safe, *memKV) {
	t.Helper()
	dir := t.TempDir()
	kv := newMemKV()
	remote := NewRemote(kv, "harnessly/users/alice", dir)
	remote.RefPrefixes = []string{"shared/ci"}
	return &Safe{
		Local:   Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "state", "safe.key")),
		Primary: remote,
		Refs:    remote,
		Where:   WhereVault,
	}, kv
}

func TestSafeKeepsNewSecretsOnTheServer(t *testing.T) {
	s, kv := newSplitSafe(t)
	if _, _, err := s.Local.Put("old-token", "old-value-1"); err != nil {
		t.Fatal(err)
	}
	v, created, err := s.Put("gh-token", "  new-value-1\n")
	if err != nil || !created || v.Where != WhereVault {
		t.Fatalf("put: %+v %v %v", v, created, err)
	}
	if got := kv.data["harnessly/users/alice/gh-token"]["value"]; got != "new-value-1" {
		t.Fatalf("server holds %q", got)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Local.dir, "safe.json"))
	if strings.Contains(string(raw), "gh-token") {
		t.Fatal("the new secret landed in the file")
	}
	views, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].Name != "gh-token" || views[0].Where != WhereVault || views[1].Where != WhereLocal {
		t.Fatalf("list: %+v", views)
	}
	values, err := s.Reveal([]string{"gh-token", "old-token", "local:old-token"}, Use{Conv: "c1", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if values["gh-token"] != "new-value-1" || values["old-token"] != "old-value-1" || values["local:old-token"] != "old-value-1" {
		t.Fatalf("values: %v", values)
	}
	views, _ = s.List()
	if views[0].LastUsedAt == 0 || len(views[0].UsedBy) != 1 || views[0].UsedBy[0].Conv != "c1" {
		t.Fatalf("use not recorded: %+v", views[0])
	}
	ledger, _ := os.ReadFile(filepath.Join(filepath.Dir(s.Local.dir), "remote-ledger.json"))
	if strings.Contains(string(ledger), "new-value-1") {
		t.Fatal("the ledger holds a value")
	}
}

func TestSafeShadowsAndRemoves(t *testing.T) {
	s, kv := newSplitSafe(t)
	if _, _, err := s.Local.Put("tok", "local-value"); err != nil {
		t.Fatal(err)
	}
	// Put on the server by another machine of the person: the server's one wins.
	kv.data["harnessly/users/alice/tok"] = map[string]string{"value": "remote-value"}
	values, err := s.Reveal([]string{"tok", "local:tok"}, Use{})
	if err != nil || values["tok"] != "remote-value" || values["local:tok"] != "local-value" {
		t.Fatalf("server first: %v %v", values, err)
	}
	if err := s.Remove("tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := kv.data["harnessly/users/alice/tok"]; ok {
		t.Fatal("not removed on the server")
	}
	// The file's one shows again, and goes on the next remove.
	if values, err := s.Reveal([]string{"tok"}, Use{}); err != nil || values["tok"] != "local-value" {
		t.Fatalf("file after: %v %v", values, err)
	}
	if err := s.Remove("tok"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("tok"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("third remove: %v", err)
	}
}

func TestReplacingAFileSecretMovesIt(t *testing.T) {
	s, kv := newSplitSafe(t)
	if _, _, err := s.Local.Put("tok", "local-value"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put("tok", "new-value"); err != nil {
		t.Fatal(err)
	}
	if kv.data["harnessly/users/alice/tok"]["value"] != "new-value" || s.Local.Has("tok") {
		t.Fatal("the replaced secret did not move to the server")
	}
	views, err := s.List()
	if err != nil || len(views) != 1 || views[0].Where != WhereVault {
		t.Fatalf("list: %+v %v", views, err)
	}
}

func TestRefsAreReadOnlyAndFenced(t *testing.T) {
	s, kv := newSplitSafe(t)
	kv.data["shared/ci/deploy"] = map[string]string{"token": "ci-token-1", "user": "bot"}
	kv.data["shared/prod/db"] = map[string]string{"value": "prod-pass-1"}
	values, err := s.Reveal([]string{"vault:shared/ci/deploy#token"}, Use{})
	if err != nil || values["vault:shared/ci/deploy#token"] != "ci-token-1" {
		t.Fatalf("ref: %v %v", values, err)
	}
	if _, err := s.Reveal([]string{"vault:shared/prod/db"}, Use{}); ReasonOf(err) != ReasonDenied {
		t.Fatalf("outside the prefixes: %v", err)
	}
	if _, err := s.Reveal([]string{"vault:shared/ci/deploy#nope"}, Use{}); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("missing key: %v", err)
	}
	if _, _, err := s.Put("vault:shared/ci/deploy#token", "x1234"); ReasonOf(err) != ReasonReadOnly {
		t.Fatalf("put a ref: %v", err)
	}
	if err := s.Remove("vault:shared/ci/deploy#token"); ReasonOf(err) != ReasonReadOnly {
		t.Fatalf("remove a ref: %v", err)
	}
	for in, want := range map[string]string{
		"gh-token": "gh-token", "local:gh-token": "gh-token", "vault:shared/ci/deploy#token": "deploy-token",
		"vault:shared/ci/deploy": "deploy", "vault:a/.hidden": "secret.hidden",
	} {
		if got := FileName(in); got != want {
			t.Errorf("FileName(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"vault:../x", "vault:a//b", "vault:a/b#", "vault:/a", "local:", "local:a/b", "x:y"} {
		if ValidRef(bad) {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestSafeWithoutServerIsTheFile(t *testing.T) {
	dir := t.TempDir()
	s := &Safe{Local: Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "k"))}
	if _, _, err := s.Put("a", "value-a"); err != nil {
		t.Fatal(err)
	}
	views, err := s.List()
	if err != nil || len(views) != 1 || views[0].Where != "" {
		t.Fatalf("file alone: %+v %v", views, err)
	}
	if _, err := s.Reveal([]string{"vault:a/b"}, Use{}); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("ref without a server: %v", err)
	}
}

func TestServerDownIsUnavailable(t *testing.T) {
	s, kv := newSplitSafe(t)
	kv.down = true
	if _, _, err := s.Put("a", "value-a"); ReasonOf(err) != ReasonUnavailable {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.List(); ReasonOf(err) != ReasonUnavailable {
		t.Fatalf("list: %v", err)
	}
	if _, err := s.Reveal([]string{"a"}, Use{}); ReasonOf(err) != ReasonUnavailable {
		t.Fatalf("reveal: %v", err)
	}
}

func TestServerSecretsKeepProjectAndSiteLimits(t *testing.T) {
	s, kv := newSplitSafe(t)
	// An ask from project A keeps the new secret to A, as the file does.
	v, created, err := s.PutAsked(Ask{Name: "api", Project: "A", Origin: "https://example.com"}, "v1")
	if err != nil || !created || v.Where != WhereVault {
		t.Fatalf("put asked: %+v %v %v", v, created, err)
	}
	if _, ok := kv.data["harnessly/users/alice/api"]; !ok {
		t.Fatal("the value is not on the server")
	}
	if _, err := s.Lookup("api", "B"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("project B sees A's secret: %v", err)
	}
	if _, err := s.Reveal([]string{"api"}, Use{Conv: "c", Project: "B"}); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("project B reads A's secret: %v", err)
	}
	if _, err := s.Reveal([]string{"api"}, Use{Conv: "c", Project: "A", Origin: "https://other.example.com"}); ReasonOf(err) != "origin-denied" {
		t.Fatalf("typed into a site it was not given: %v", err)
	}
	if got, err := s.Reveal([]string{"api"}, Use{Conv: "c", Project: "A", Origin: "https://example.com"}); err != nil || got["api"] != "v1" {
		t.Fatalf("reveal for A: %v %v", got, err)
	}
	// B's ask cannot overwrite A's secret; a grant lets B use it.
	if _, _, err := s.PutAsked(Ask{Name: "api", Project: "B"}, "v2"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("B overwrote A's secret: %v", err)
	}
	if err := s.Grant("api", "B", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup("api", "B"); err != nil {
		t.Fatalf("granted B: %v", err)
	}
	if v, err := s.Metadata("api", nil, nil); err != nil || len(v.Projects) != 0 {
		t.Fatalf("metadata: %+v %v", v, err)
	}
}

func TestMovingAFileSecretKeepsItsLimits(t *testing.T) {
	s, _ := newSplitSafe(t)
	if _, _, err := s.Local.PutAsked(Ask{Name: "tok", Project: "A"}, "old"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutAsked(Ask{Name: "tok", Project: "B"}, "x"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("B replaced A's file secret: %v", err)
	}
	v, _, err := s.Put("tok", "new")
	if err != nil || v.Where != WhereVault || len(v.Projects) != 1 || v.Projects[0] != "A" {
		t.Fatalf("moved: %+v %v", v, err)
	}
	if s.Local.Has("tok") {
		t.Fatal("the old copy stayed in the file")
	}
	if _, err := s.Lookup("tok", "B"); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("the limit was lost in the move: %v", err)
	}
}
