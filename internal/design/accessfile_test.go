package design

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAccessSnapshotSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	l := New(Config{DataDir: dir})
	if _, ok := l.Access(); ok {
		t.Fatal("a snapshot out of nothing")
	}
	until := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	want := AccessSnapshot{Enforce: true, At: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		Projects: map[string]Grant{"p1": {Role: "edit", Owner: "a@b.c", OwnerName: "A", Until: &until}, "p2": {Role: "owner", Owner: "me@b.c", OwnerName: "Me"}},
		Team:     map[string]string{"p1": "none", "p2": "view"}}
	if err := l.SetAccess(want); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dir, AccessFile)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", st, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*.tmp")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	got, ok := New(Config{DataDir: dir}).Access()
	if !ok || !got.Enforce || !got.At.Equal(want.At) || got.Team["p2"] != "view" || got.Projects["p2"].Until != nil {
		t.Fatalf("after restart: %+v %v", got, ok)
	}
	if g := got.Projects["p1"]; g.Role != "edit" || g.OwnerName != "A" || g.Until == nil || !g.Until.Equal(until) {
		t.Errorf("grant after restart: %+v", g)
	}
}

func TestAccessFileToleratesExtrasAndRefusesGarbage(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, AccessFile)
	if err := os.WriteFile(file, []byte(`{"enforce":true,"at":"2026-10-20T09:00:00Z","later":{"x":1},"projects":{"p1":{"role":"view","owner":"a@b.c","ownerName":"A","until":null,"new":1}},"team":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, ok := New(Config{DataDir: dir}).Access(); !ok || s.Projects["p1"].Role != "view" {
		t.Errorf("extra fields: %+v %v", s, ok)
	}
	for _, bad := range []string{`{`, ``, `[]`, `{"enforce":true,"at":"2026-10-20T09:00:00Z","projects":{"p1":{"role":"god"}}}`, `{"at":"2026-10-20T09:00:00Z","team":{"p1":"admin"}}`, `{}`, `null`, `{"enforce":false}`} {
		if err := os.WriteFile(file, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := New(Config{DataDir: dir}).Access(); ok {
			t.Errorf("%q gave a snapshot", bad)
		}
	}
}

func TestOnlyTheMachineAgentMayPostASnapshot(t *testing.T) {
	l := New(Config{DataDir: t.TempDir()})
	agentCall := func(r *http.Request) bool { return r.Header.Get("X-Agent") == "1" }
	api := &API{L: l, MachineAgent: agentCall}
	do := func(method, agent, body string) Answer {
		r := httptest.NewRequest(method, "/design/access", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if agent != "" {
			r.Header.Set("X-Agent", agent)
		}
		return api.PostAccess(r)
	}
	snap := `{"enforce":true,"at":"2026-10-20T09:00:00Z","projects":{"p1":{"role":"manage","owner":"a@b.c","ownerName":"A","until":null}},"team":{"p1":"comment"}}`
	if a := do("POST", "", snap); a.Status != 403 {
		t.Errorf("a page: %d", a.Status)
	}
	if _, ok := l.Access(); ok {
		t.Fatal("a snapshot from a page was taken")
	}
	if a := do("GET", "1", ""); a.Status != 405 {
		t.Errorf("GET: %d", a.Status)
	}
	if a := do("POST", "1", `{"projects":{"p1":{"role":"root"}}}`); a.Status != 400 {
		t.Errorf("unknown role: %d", a.Status)
	}
	if a := do("POST", "1", snap); a.Status != 200 {
		t.Fatalf("the agent: %d %v", a.Status, a.Body)
	}
	if s, ok := l.Access(); !ok || s.Projects["p1"].Role != "manage" || s.Team["p1"] != "comment" {
		t.Errorf("kept: %+v %v", s, ok)
	}
}

func TestAccessSnapshotIsValidatedAndOrdered(t *testing.T) {
	l := New(Config{DataDir: t.TempDir()})
	api := &API{L: l, MachineAgent: func(*http.Request) bool { return true }}
	post := func(body string) Answer {
		r := httptest.NewRequest("POST", "/design/access", strings.NewReader(body))
		return api.PostAccess(r)
	}
	for _, bad := range []string{`{}`, `null`, ``, `[]`, `{"enforce":false}`, `{"at":"0001-01-01T00:00:00Z"}`, `{"at":"nope"}`,
		`{"at":"2026-10-20T09:00:00Z","projects":{"Bad Id":{"role":"view"}}}`,
		`{"at":"2026-10-20T09:00:00Z","team":{"../x":"view"}}`,
		`{"at":"2026-10-20T09:00:00Z","projects":{"p1":{"role":"view","owner":"` + strings.Repeat("a", 255) + `"}}}`,
		`{"at":"2026-10-20T09:00:00Z","projects":{"p1":{"role":"view","ownerName":"` + strings.Repeat("a", 65) + `"}}}`,
		`{"at":"2026-10-20T09:00:00Z","projects":{"p1":{"role":"view","ownerName":"a\nb"}}}`,
		`{"at":"2026-10-20T09:00:00Z","projects":{"p1":{"role":"view","until":"yesterday"}}}`,
		`{"at":"2026-10-20T09:00:00Z","padding":"` + strings.Repeat("x", 5<<20) + `"}`} {
		if a := post(bad); a.Status != 400 {
			t.Errorf("%.60q: %d", bad, a.Status)
		}
	}
	if _, ok := l.Access(); ok {
		t.Fatal("a refused snapshot was taken")
	}
	many := map[string]string{}
	for i := 0; i < 10001; i++ {
		many["p"+strconv.Itoa(i)] = "view"
	}
	if err := (AccessSnapshot{At: time.Now(), Team: many}).Validate(); err == nil {
		t.Error("10001 entries accepted")
	}
	if a := post(`{"enforce":true,"at":"2026-10-20T09:00:00Z","projects":{},"team":{}}`); a.Status != 200 {
		t.Fatalf("empty but timed snapshot: %d", a.Status)
	}
	if a := post(`{"enforce":false,"at":"2026-10-20T08:59:59Z"}`); a.Status != 400 {
		t.Errorf("an older snapshot: %d", a.Status)
	}
	if s, _ := l.Access(); !s.Enforce {
		t.Error("an older snapshot switched enforcement off")
	}
	if a := post(`{"enforce":true,"at":"2026-10-20T09:00:00Z"}`); a.Status != 200 {
		t.Errorf("the same instant: %d", a.Status)
	}
}

func TestConcurrentSnapshotsLeaveDiskEqualToMemory(t *testing.T) {
	dir := t.TempDir()
	l := New(Config{DataDir: dir})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.SetAccess(AccessSnapshot{At: time.Unix(int64(1000+i), 0).UTC(), Team: map[string]string{"p" + strconv.Itoa(i): "view"}})
		}()
	}
	wg.Wait()
	mem, _ := l.Access()
	disk, ok := New(Config{DataDir: dir}).Access()
	if !ok || !disk.At.Equal(mem.At) || len(disk.Team) != 1 {
		t.Fatalf("disk %+v, memory %+v", disk, mem)
	}
}

func TestAccessSnapshotFromTheFutureIsClamped(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	l := New(Config{DataDir: dir})
	l.now = func() time.Time { return now }
	api := &API{L: l, MachineAgent: func(*http.Request) bool { return true }}
	post := func(at time.Time) int {
		body := `{"enforce":true,"at":"` + at.Format(time.RFC3339) + `","projects":{},"team":{}}`
		r := httptest.NewRequest(http.MethodPost, "/design/access", strings.NewReader(body))
		return api.PostAccess(r).Status
	}
	if code := post(now.Add(4 * time.Minute)); code != 200 {
		t.Errorf("a little ahead: %d", code)
	}
	// Far ahead is accepted, with its time clamped: no lockout, and no old roles left in force.
	if code := post(now.Add(6 * time.Hour)); code != 200 {
		t.Errorf("far ahead: %d", code)
	}
	if s, ok := l.Access(); !ok || !s.At.Equal(now.Add(accessMaxAhead)) {
		t.Errorf("clamped at: %+v %v", s, ok)
	}
	if code := post(now.Add(7 * time.Minute)); code != 200 {
		t.Errorf("an honest snapshot after a far one: %d", code)
	}
	// A file dated ahead is kept on load (dropping it would open everything on a restart with a slow clock).
	dir2 := t.TempDir()
	body := `{"enforce":true,"at":"2026-10-25T10:00:00Z","projects":{},"team":{}}`
	if err := os.WriteFile(filepath.Join(dir2, AccessFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l2 := &Library{cfg: Config{DataDir: dir2}, now: func() time.Time { return now }}
	l2.loadAccess()
	if _, ok := l2.Access(); !ok {
		t.Error("a snapshot from the future was dropped on load")
	}
	// ... with its time clamped, so that a clock that stepped back does not make every fresh snapshot stale.
	if s, _ := l2.Access(); !s.At.Equal(now.Add(accessMaxAhead)) {
		t.Errorf("kept at: %v", s.At)
	}
}
