package orgs_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// The four answers the Manage page reads (/api/me/limits, usage, sessions, kpis), captured from the real handlers into
// web/app/next/features/manage/fixtures. The web test parses exactly these bodies, so the client cannot drift from the
// server: UPDATE_FIXTURES=1 go test ./internal/hub/orgs -run TestMeManageFixturesAreCurrent writes them.

// Generated ids are 20 lowercase letters or digits and differ on every run.
var randomID = regexp.MustCompile(`"[a-z0-9]{20}"`)

func capture(t *testing.T, p *hubtest.Person, dir string) {
	t.Helper()
	for _, c := range []struct{ path, file string }{
		{"/api/me/limits", "limits.json"},
		{"/api/me/usage", "usage.json"},
		{"/api/me/sessions", "sessions.json"},
		{"/api/me/kpis", "kpis.json"},
	} {
		r := p.Do("GET", c.path, nil)
		if r.Code != 200 {
			t.Fatalf("%s: %s", c.path, r)
		}
		var v any
		r.JSON(&v)
		sortRules(v)
		got, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		path := filepath.Join("..", "..", "..", "web", "app", "next", "features", "manage", "fixtures", dir, c.file)
		if os.Getenv("UPDATE_FIXTURES") == "1" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (UPDATE_FIXTURES=1 go test ./internal/hub/orgs -run TestMeManageFixturesAreCurrent writes it)", path, err)
		}
		if !bytes.Equal(scrub(want), scrub(got)) {
			t.Errorf("%s differs from the hub's answer: run UPDATE_FIXTURES=1 go test ./internal/hub/orgs -run TestMeManageFixturesAreCurrent", path)
		}
	}
}

// The hub lists rules in id order, and ids are random: sort the lists by their content so a rerun gives the same file.
func sortRules(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"rules", "effective"} {
		list, ok := m[key].([]any)
		if !ok {
			continue
		}
		keys := make(map[int]string, len(list))
		for i, e := range list {
			b, _ := json.Marshal(e)
			keys[i] = string(scrub(b))
		}
		idx := make([]int, len(list))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
		sorted := make([]any, len(list))
		for i, j := range idx {
			sorted[i] = list[j]
		}
		m[key] = sorted
	}
}

func scrub(b []byte) []byte { return randomID.ReplaceAll(b, []byte(`"-"`)) }

func TestMeManageFixturesAreCurrent(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
		s.SignIn("root@example.com")
		capture(t, s.SignIn("solo@example.com"), "empty")
	})
	t.Run("rules", func(t *testing.T) {
		v := newVera(t)
		capture(t, v.vera, "rules")
	})
}
