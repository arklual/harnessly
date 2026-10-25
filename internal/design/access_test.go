package design

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var accessNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func timeP(t time.Time) *time.Time { return &t }

// accessLibrary is a library of one runtime ("se") whose clock stands at accessNow.
func accessLibrary(t *testing.T, root string) *Library {
	t.Helper()
	l := ownerLibrary(t, root, "se", "p.smirnov@example.com")
	l.now = func() time.Time { return accessNow }
	return l
}

func makeProject(t *testing.T, root, id, title, ownerFile string) {
	t.Helper()
	writeFile(t, filepath.Join(root, id, ArtifactFile), `{"title":"`+title+`"}`)
	writeFile(t, filepath.Join(root, id, ProjectDirname, "Main.dc.html"), "<html><body>"+title+"</body></html>")
	if ownerFile != "" {
		writeFile(t, filepath.Join(root, id, OwnerFile), ownerFile)
	}
}

func snapshot(grants map[string]Grant, team map[string]string) AccessSnapshot {
	return AccessSnapshot{Enforce: true, At: accessNow.Add(-time.Minute), Projects: grants, Team: team}
}

const (
	mineV1    = `{"v":1,"user":"se","name":"Пётр"}`
	mineV2    = `{"v":2,"user":"se","name":"Пётр","private":true}`
	otherV1   = `{"v":1,"user":"ki","name":"Олег"}`
	otherV2   = `{"v":2,"user":"ki","name":"Олег","private":true}`
	otherNone = ""
)

// Every step of the order RoleOf follows, one row each.
func TestRoleOfFollowsTheOrder(t *testing.T) {
	root := t.TempDir()
	for id, owner := range map[string]string{"g": otherV2, "expired": otherV2, "mine": mineV1, "teamed": otherV2, "norow": otherNone, "norow-v1": otherV1,
		"norow-private": otherV2, "norow-mine": mineV2, "closed": otherV1, "both": otherV2, "grantmine": mineV2} {
		makeProject(t, root, id, id, owner)
	}
	l := accessLibrary(t, root)
	who, _ := l.Identity()
	future, past := accessNow.Add(time.Hour), accessNow.Add(-time.Hour)
	snap := snapshot(map[string]Grant{
		"g":         {Role: "comment", Owner: "k@x", OwnerName: "Олег", Until: timeP(future)},
		"expired":   {Role: "edit", Owner: "k@x", OwnerName: "Олег", Until: timeP(past)},
		"both":      {Role: "view", Owner: "k@x", OwnerName: "Олег"},
		"grantmine": {Role: "view", Owner: "k@x", OwnerName: "Олег"},
	}, map[string]string{"g": "none", "expired": "none", "teamed": "view", "closed": "none", "both": "edit", "mine": "none", "grantmine": "none"})

	check := func(label, id string, want Role) {
		t.Helper()
		if got := l.RoleOf(id, who, accessNow); got != want {
			t.Errorf("%s: role of %s = %q, want %q", label, id, got, want)
		}
	}
	// 1. no snapshot: edit, even for a project someone else's and private.
	check("no snapshot", "teamed", RoleEdit)
	if err := l.SetAccess(AccessSnapshot{Enforce: false, At: accessNow, Projects: snap.Projects, Team: snap.Team}); err != nil {
		t.Fatal(err)
	}
	check("not enforcing", "teamed", RoleEdit)
	if err := l.SetAccess(snap); err != nil {
		t.Fatal(err)
	}
	check("a grant", "g", RoleComment)
	check("a grant that ran out", "expired", RoleNone)
	check("the owner file of this runtime, a team row and no grant: the file no longer speaks", "mine", RoleNone)
	check("the owner file of this runtime, no row at all", "norow-mine", RoleOwner)
	check("the team level", "teamed", RoleView)
	check("a row with team none", "closed", RoleNone)
	check("no row, no owner file", "norow", RoleEdit)
	check("no row, a v1 owner file of someone else", "norow-v1", RoleEdit)
	check("no row, a private v2 owner file", "norow-private", RoleNone)
	check("the stronger of the grant and the team level", "both", RoleEdit)
	check("a grant beats the owner file", "grantmine", RoleView)
	if got := l.RoleOf("none-such", who, accessNow); got != RoleEdit {
		t.Errorf("a project that does not exist: %q", got)
	}
}

// A grant ends at its until by the runtime's clock, without waiting for the gateway's next snapshot.
func TestExpiredGrantIsRefusedBeforeTheNextSnapshot(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Secret plan", otherV2)
	l := accessLibrary(t, root)
	who, _ := l.Identity()
	until := accessNow.Add(30 * time.Minute)
	if err := l.SetAccess(snapshot(map[string]Grant{"p1": {Role: "edit", Owner: "k@x", OwnerName: "Олег", Until: &until}}, map[string]string{"p1": "none"})); err != nil {
		t.Fatal(err)
	}
	if got := l.RoleOf("p1", who, accessNow); got != RoleEdit {
		t.Fatalf("before: %q", got)
	}
	if got := l.RoleOf("p1", who, until); got != RoleNone {
		t.Fatalf("at until: %q", got)
	}
	l.now = func() time.Time { return until.Add(time.Second) }
	if l.Can("p1", RoleView) {
		t.Fatal("the runtime clock still lets the person in")
	}
}

func TestListingHidesProjectsWithoutARole(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "open", "Open", otherV1)
	makeProject(t, root, "hidden", "Hidden", otherV2)
	makeProject(t, root, "fresh", "Fresh", mineV2)
	l := accessLibrary(t, root)
	if got := len(l.VisibleTo(l.ListArtifacts())); got != 3 {
		t.Fatalf("without a snapshot: %d", got)
	}
	// "fresh" is not in the snapshot: the maker still sees it.
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"hidden": "none"})); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range l.VisibleTo(l.ListArtifacts()) {
		ids = append(ids, a.ID)
	}
	if len(ids) != 2 || !(strings.Contains(strings.Join(ids, ","), "open") && strings.Contains(strings.Join(ids, ","), "fresh")) {
		t.Fatalf("visible: %v", ids)
	}
}

func boardGet(l *Library, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = "127.0.0.1:18470"
	w := httptest.NewRecorder()
	l.serveLibrary(w, r, 18470)
	return w
}

// A revoke is felt by the next request to the board: 403 and a short page that says nothing of the project.
func TestBoardRoute403AfterRevoke(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Secret plan", otherV2)
	l := accessLibrary(t, root)
	grant := snapshot(map[string]Grant{"p1": {Role: "view", Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"p1": "none"})
	if err := l.SetAccess(grant); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/a/p1/p/Main.dc.html", "/a/p1/", "/__lib/boards?a=p1", "/__lib/version?a=p1", "/__lib/changes?a=p1&since=0"} {
		if w := boardGet(l, p); w.Code == 403 {
			t.Fatalf("%s refused with a grant: %s", p, w.Body)
		}
	}
	if w := boardGet(l, "/a/p1/p/Main.dc.html"); w.Code != 200 || !strings.Contains(w.Body.String(), "Secret plan") {
		t.Fatalf("board with a grant: %d %s", w.Code, w.Body)
	}
	revoked := grant
	revoked.At = accessNow
	revoked.Projects = map[string]Grant{}
	if err := l.SetAccessOrdered(revoked); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/a/p1/p/Main.dc.html", "/a/p1/", "/a/p1/p/anything.png"} {
		w := boardGet(l, p)
		if w.Code != 404 {
			t.Errorf("%s after revoke: %d %s", p, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "Secret plan") || strings.Contains(w.Body.String(), "Олег") {
			t.Errorf("%s: the refusal names the project: %s", p, w.Body)
		}
		// The same body as an id that does not exist.
		if gone := boardGet(l, strings.Replace(p, "p1", "nothing-here", 1)); gone.Code != 404 || gone.Body.String() != w.Body.String() {
			t.Errorf("%s: hidden and missing differ: %d %q / %d %q", p, w.Code, w.Body, gone.Code, gone.Body)
		}
	}
	for _, p := range []string{"/__lib/boards?a=p1", "/__lib/version?a=p1", "/__lib/changes?a=p1&since=0", "/__lib/events?a=p1"} {
		if w := boardGet(l, p); w.Code != 404 {
			t.Errorf("%s after revoke: %d", p, w.Code)
		}
	}
	if w := boardGet(l, "/"); strings.Contains(w.Body.String(), "p1") || strings.Contains(w.Body.String(), "Secret plan") {
		t.Errorf("the index lists it: %s", w.Body)
	}
}

func pagesAPI(l *Library) *API {
	return &API{L: l, Session: func(*http.Request) bool { return true }, CLIToken: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
}

func pagePost(a *API, body string) Answer {
	r := httptest.NewRequest(http.MethodPost, "/api/cli/design/pages", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return a.CLIPages(r)
}

func TestPageEditNeedsEdit(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	set := func(role string) {
		t.Helper()
		s := snapshot(map[string]Grant{"p1": {Role: role, Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"p1": "none"})
		s.At = s.At.Add(time.Duration(len(role)) * time.Second)
		if err := l.SetAccess(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"view", "comment"} {
		set(role)
		ans := pagePost(a, `{"action":"create","project":"p1","title":"New"}`)
		raw, _ := json.Marshal(ans.Body)
		if ans.Status != 403 || !strings.Contains(string(raw), `"code":"no_access"`) || !strings.Contains(string(raw), `"ownerName":"Олег"`) || !strings.Contains(string(raw), `"owner":"k@x"`) {
			t.Errorf("%s may create a page: %d %s", role, ans.Status, raw)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/cli/design/pages?project=p1", nil)
		if ans := a.CLIPages(r); ans.Status == 403 {
			t.Errorf("%s may not list pages", role)
		}
	}
	set("edit")
	if ans := pagePost(a, `{"action":"create","project":"p1","title":"New"}`); ans.Status == 403 {
		t.Errorf("edit refused: %d", ans.Status)
	}
	// Revoked altogether: not even the list.
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"p1": "none"})); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/cli/design/pages?project=p1", nil)
	if ans := a.CLIPages(r); ans.Status != 404 {
		t.Errorf("list after revoke: %d", ans.Status)
	}
}

// Review Focus 3: no snapshot, or one that does not enforce, changes nothing: every route works as it did.
func TestNoSnapshotMeansEverythingAsBefore(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	if ans := pagePost(a, `{"action":"create","project":"p1","title":"New"}`); ans.Status == 403 {
		t.Fatalf("no snapshot: %d", ans.Status)
	}
	if w := boardGet(l, "/a/p1/p/Main.dc.html"); w.Code != 200 {
		t.Fatalf("board without a snapshot: %d", w.Code)
	}
	if err := l.SetAccess(AccessSnapshot{Enforce: false, At: accessNow, Projects: map[string]Grant{}, Team: map[string]string{"p1": "none"}}); err != nil {
		t.Fatal(err)
	}
	if w := boardGet(l, "/a/p1/p/Main.dc.html"); w.Code != 200 {
		t.Fatalf("board with a snapshot that does not enforce: %d", w.Code)
	}
	if len(l.VisibleTo(l.ListArtifacts())) != 1 {
		t.Fatal("listing hides without enforcement")
	}
}

// Review Focus 2: a restart keeps the snapshot, so a personal project stays closed.
func TestRestartKeepsPersonalProjectsClosed(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	mk := func() *Library {
		l := New(Config{LibraryDir: root, DataDir: data})
		l.osUser = func() string { return "se" }
		l.now = func() time.Time { return accessNow }
		return l
	}
	if err := mk().SetAccess(snapshot(map[string]Grant{}, map[string]string{"p1": "none"})); err != nil {
		t.Fatal(err)
	}
	if w := boardGet(mk(), "/a/p1/p/Main.dc.html"); w.Code != 404 {
		t.Fatalf("after a restart: %d", w.Code)
	}
}

func TestCreateMarksPrivateAndEmitsCreated(t *testing.T) {
	root := t.TempDir()
	l := accessLibrary(t, root)
	var mu sync.Mutex
	var events []string
	l.emitFn = func(kind, project string) { mu.Lock(); events = append(events, kind+" "+project); mu.Unlock() }
	p, err := l.CreateProject("Новый")
	if err != nil {
		t.Fatal(err)
	}
	l.AnnounceNew(p.ID)
	raw, err := os.ReadFile(filepath.Join(root, p.ID, OwnerFile))
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	if json.Unmarshal(raw, &f) != nil || f["v"] != float64(2) || f["private"] != true || f["user"] != "se" || f["name"] != "Пётр" {
		t.Fatalf("owner file: %s", raw)
	}
	if len(events) != 1 || events[0] != "design.created "+p.ID {
		t.Fatalf("events: %v", events)
	}
	// Before the gateway has heard of it, the maker sees and edits it.
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	if got := l.RoleNow(p.ID); got != RoleOwner {
		t.Fatalf("role of the maker: %q", got)
	}
}

func TestV1OwnFileEmitsClaimOnce(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "old", "Old", mineV1)
	makeProject(t, root, "theirs", "Theirs", otherV1)
	makeProject(t, root, "known", "Known", mineV1)
	makeProject(t, root, "v2", "V2", mineV2)
	l := accessLibrary(t, root)
	var mu sync.Mutex
	var events []string
	l.emitFn = func(kind, project string) { mu.Lock(); events = append(events, kind+" "+project); mu.Unlock() }
	who, _ := l.Identity()
	sweep := func() {
		for _, id := range []string{"old", "theirs", "known", "v2"} {
			o, _ := l.ReadOwner(id)
			l.ClaimOnce(id, o, who)
		}
	}
	sweep() // no snapshot: that gateway does not know the event
	if len(events) != 0 {
		t.Fatalf("without a snapshot: %v", events)
	}
	if err := l.SetAccess(snapshot(map[string]Grant{"known": {Role: "owner", Owner: "s@x", OwnerName: "Пётр"}}, map[string]string{"known": "edit"})); err != nil {
		t.Fatal(err)
	}
	sweep()
	sweep()
	if len(events) != 1 || events[0] != "design.claim old" {
		t.Fatalf("events: %v", events)
	}
}

func hookCall(t *testing.T, env GuardEnv, tool, key, value, cwd string) (deny bool, reason string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]any{key: value}, "cwd": cwd})
	return hookRaw(t, env, raw)
}

func hookRaw(t *testing.T, env GuardEnv, raw []byte) (bool, string) {
	t.Helper()
	out := GuardHook(raw, "acme", env)
	if out == nil {
		return false, ""
	}
	var a struct {
		H struct {
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &a); err != nil || a.H.Decision != "deny" {
		t.Fatalf("answer: %s", out)
	}
	return true, a.H.Reason
}

func TestGuardRefusesReadOfAHiddenProjectAndWriteWithView(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	root, data := t.TempDir(), t.TempDir()
	makeProject(t, root, "secret", "Тайный план", otherV2)
	makeProject(t, root, "shared", "Общий", otherV2)
	// The hook asks the operating system who runs it.
	u, err := user.Current()
	if err != nil {
		t.Skip("no current user")
	}
	makeProject(t, root, "mine", "Мой", `{"v":2,"user":"`+u.Username+`","name":"Я","private":true}`)
	l := New(Config{DataDir: data, LibraryDir: root})
	env := GuardEnv{DataDir: data, Now: accessNow}
	secret := filepath.Join(root, "secret", ProjectDirname, "Main.dc.html")
	shared := filepath.Join(root, "shared", ProjectDirname, "Main.dc.html")
	mine := filepath.Join(root, "mine", ProjectDirname, "Main.dc.html")

	// No snapshot: open (an old gateway).
	if deny, _ := hookCall(t, env, "Read", "file_path", secret, ""); deny {
		t.Fatal("refused without a snapshot")
	}
	if deny, _ := hookRaw(t, env, []byte("not json")); deny {
		t.Fatal("broken input refused without a snapshot")
	}
	if err := l.SetAccess(snapshot(map[string]Grant{"shared": {Role: "view", Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"secret": "none", "shared": "none"})); err != nil {
		t.Fatal(err)
	}
	// No role at all: the plain words, no title and no owner.
	want := "Нет доступа к проекту"
	deny, reason := hookCall(t, env, "Read", "file_path", secret, "")
	if !deny || reason != want {
		t.Fatalf("read of a hidden project: %v %q", deny, reason)
	}
	cases := []struct {
		tool, key, value, cwd string
		deny                  bool
	}{
		{"Write", "file_path", secret, "", true},
		{"Edit", "file_path", "project/Main.dc.html", filepath.Join(root, "secret"), true},
		{"Read", "file_path", "project/Main.dc.html", filepath.Join(root, "secret"), true},
		{"Glob", "pattern", filepath.Join(root, "secret", "**", "*.html"), "", true},
		{"Grep", "path", filepath.Join(root, "secret"), "", true},
		{"Bash", "command", "cat " + secret, "", true},
		{"Bash", "command", "ls", filepath.Join(root, "secret"), true},
		{"Read", "file_path", shared, "", false},
		{"Write", "file_path", shared, "", true},
		{"Edit", "file_path", shared, "", true},
		{"Bash", "command", "cat " + shared, "", false},
		{"Bash", "command", "echo x > " + shared, "", true},
		{"Bash", "command", "sed -i s/a/b/ " + shared, "", true},
		{"Write", "file_path", mine, "", false},
		{"Read", "file_path", "/etc/hostname", filepath.Join(root, "mine"), false},
		{"Bash", "command", "ls", filepath.Join(root, "mine"), false},
		{"Write", "file_path", filepath.Join(t.TempDir(), "x.txt"), "", false},
	}
	for _, c := range cases {
		if got, _ := hookCall(t, env, c.tool, c.key, c.value, c.cwd); got != c.deny {
			t.Errorf("%s %s (cwd %s): deny %v, want %v", c.tool, c.value, c.cwd, got, c.deny)
		}
	}
	// With a snapshot an input the hook cannot read is refused, never let through.
	if deny, _ := hookRaw(t, env, []byte("not json")); !deny {
		t.Error("broken input let through with a snapshot")
	}
	// The grant ending is felt by the hook with no new snapshot.
	until := accessNow.Add(time.Minute)
	if err := l.SetAccess(snapshot(map[string]Grant{"shared": {Role: "edit", Owner: "k@x", OwnerName: "Олег", Until: &until}}, map[string]string{"shared": "none"})); err != nil {
		t.Fatal(err)
	}
	if deny, _ := hookCall(t, env, "Write", "file_path", shared, ""); deny {
		t.Error("edit refused inside the grant")
	}
	env.Now = until.Add(time.Second)
	if deny, _ := hookCall(t, env, "Write", "file_path", shared, ""); !deny {
		t.Error("write allowed after the grant ran out")
	}
	// The canvas guard still works beside it.
	writeFile(t, filepath.Join(root, "mine", ProjectDirname, canvasJSONName), "{}")
	if deny, _ := hookCall(t, env, "Write", "file_path", filepath.Join(root, "mine", ProjectDirname, canvasJSONName), ""); !deny {
		t.Error("canvas.json writable")
	}
}

func TestViewOnlyAPIRoutesAndRevokeOfLiveRoutes(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"p1": "none"})); err != nil {
		t.Fatal(err)
	}
	get := func(path string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		return a.CLIDesign(r).Status
	}
	for _, p := range []string{"/api/cli/design?logs=p1", "/api/cli/design?state=p1"} {
		if code := get(p); code != 404 {
			t.Errorf("%s: %d", p, code)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/cli/design", nil)
	raw, _ := json.Marshal(a.CLIDesign(r).Body)
	if strings.Contains(string(raw), "p1") {
		t.Errorf("cli list shows a hidden project: %s", raw)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/cli/design/claims", strings.NewReader(`{"action":"claim","session":"s1","project":"p1","board":"Main"}`))
	r.Header.Set("Content-Type", "application/json")
	if ans := a.CLIClaims(r); ans.Status != 404 {
		t.Errorf("claim: %d", ans.Status)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/cli/design/history?project=p1", nil)
	if ans := a.CLIHistory(r); ans.Status != 404 {
		t.Errorf("history: %d", ans.Status)
	}
}

func TestStateAndLogNeedEdit(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	put := func(role string, at time.Duration) {
		s := snapshot(map[string]Grant{"p1": {Role: role, Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"p1": "none"})
		s.At = s.At.Add(at)
		if err := l.SetAccess(s); err != nil {
			t.Fatal(err)
		}
	}
	state := func() int {
		r := httptest.NewRequest(http.MethodGet, "/__lib/state?a=p1&s=%7B%7D", nil)
		r.Host = "127.0.0.1:18470"
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		l.serveLibrary(w, r, 18470)
		return w.Code
	}
	put("view", 0)
	if c := state(); c != 404 {
		t.Errorf("view writes state: %d", c)
	}
	put("edit", time.Second)
	if c := state(); c != 204 {
		t.Errorf("edit: %d", c)
	}
}

func TestOwnerFileOnlyBeforeRegistration(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", mineV1)
	l := accessLibrary(t, root)
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	if got := l.RoleNow("p1"); got != RoleOwner {
		t.Fatalf("before the gateway has a row: %q", got)
	}
	s := snapshot(map[string]Grant{"p1": {Role: "owner", Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"p1": "none"})
	s.At = accessNow
	if err := l.SetAccess(s); err != nil {
		t.Fatal(err)
	}
	s.Projects = map[string]Grant{} // ownership handed over, no grant left here
	s.At = accessNow.Add(time.Second)
	if err := l.SetAccess(s); err != nil {
		t.Fatal(err)
	}
	if got := l.RoleNow("p1"); got != RoleNone {
		t.Fatalf("a former owner keeps the role: %q", got)
	}
}

func TestGuardCoversSearchRootsAndRelativePaths(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	base, data := t.TempDir(), t.TempDir()
	root := filepath.Join(base, "design-artifacts")
	makeProject(t, root, "secret", "Тайный план", otherV2)
	makeProject(t, root, "other", "Другой", otherV2)
	u, err := user.Current()
	if err != nil {
		t.Skip("no current user")
	}
	makeProject(t, root, "mine", "Мой", `{"v":2,"user":"`+u.Username+`","name":"Я","private":true}`)
	l := New(Config{DataDir: data, LibraryDir: root})
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"secret": "none", "other": "none"})); err != nil {
		t.Fatal(err)
	}
	env := GuardEnv{DataDir: data, LibraryDir: root, Now: accessNow}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Symlink(root, filepath.Join(home, "lib")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, tool, key, value, cwd string
		deny                        bool
	}{
		{"Grep at the library root", "Grep", "path", root, base, true},
		{"Grep at a parent of the root", "Grep", "path", base, base, true},
		{"Grep regex only, cwd at a parent", "Grep", "pattern", "secret", base, true},
		{"Write the access file", "Write", "file_path", filepath.Join(data, AccessFile), "", true},
		{"Bash rewrites the access file", "Bash", "command", "echo '{}' > " + filepath.Join(data, AccessFile), "", true},
		{"Bash reads the access file", "Bash", "command", "cat " + filepath.Join(data, AccessFile), "", false},
		{"Write the library root hint", "Write", "file_path", filepath.Join(data, LibraryRootFile), "", true},
		{"Bash rewrites the library root hint", "Bash", "command", "echo /tmp > " + filepath.Join(data, LibraryRootFile), "", true},
		{"apply_patch into a hidden project", "apply_patch", "command", "*** Begin Patch\n*** Update File: " + root + "/secret/project/Main.dc.html\n@@\n-a\n+b\n*** End Patch", home, true},
		{"apply_patch moving into a hidden project", "apply_patch", "command", "*** Begin Patch\n*** Update File: " + root + "/mine/project/a.html\n*** Move to: " + root + "/secret/project/a.html\n*** End Patch", home, true},
		{"apply_patch with loose headers", "apply_patch", "command", "*** Begin Patch\n  ***update file:  " + root + "/secret/project/Main.dc.html\n*** End Patch", home, true},
		{"apply_patch in a visible project", "apply_patch", "command", "*** Begin Patch\n*** Add File: " + root + "/mine/project/new.html\n+hi\n*** End Patch", home, false},
		{"Grep with the regex only, cwd elsewhere", "Grep", "pattern", "secret|other", home, false},
		{"Grep regex that looks like a path", "Grep", "pattern", root + "/secret", home, false},
		{"Glob across projects", "Glob", "pattern", root + "/*/project/*", home, true},
		{"Glob under the root, via **", "Glob", "pattern", "**/*.html", root, true},
		{"Glob in a visible project", "Glob", "pattern", root + "/mine/project/*", home, false},
		{"LS of the root", "LS", "path", root, home, true},
		{"Read relative from the root", "Read", "file_path", "secret/project/Main.dc.html", root, true},
		{"Read through ..", "Read", "file_path", "../secret/project/Main.dc.html", filepath.Join(root, "mine"), true},
		{"Read of a visible project", "Read", "file_path", "project/Main.dc.html", filepath.Join(root, "mine"), false},
		{"Bash ..", "Bash", "command", "cat ../secret/project/Main.dc.html", filepath.Join(root, "mine"), true},
		{"Bash relative from the root", "Bash", "command", "cat secret/project/Main.dc.html", root, true},
		{"Bash glob", "Bash", "command", "cat " + root + "/*/project/*", home, true},
		{"Bash tilde", "Bash", "command", "cat ~/lib/secret/project/Main.dc.html", home, true}, // through a symlink, still found
		{"Bash tilde to the real path", "Bash", "command", "cat " + root + "/secret/project/Main.dc.html", home, true},
		{"Bash cd then relative", "Bash", "command", "cd " + root + "/secret && cat project/Main.dc.html", home, true},
		{"Bash find from above the root", "Bash", "command", "find " + base + " -name '*.html'", home, true},
		{"Bash grep -r at the root", "Bash", "command", "grep -r secret .", root, true},
		{"Bash unrelated", "Bash", "command", "ls -la", home, false},
		{"Bash in a visible project", "Bash", "command", "cat project/Main.dc.html", filepath.Join(root, "mine"), false},
		{"Write the owner file", "Write", "file_path", filepath.Join(root, "mine", OwnerFile), "", true},
		{"Bash writes the owner file", "Bash", "command", "echo '{}' > " + filepath.Join(root, "mine", OwnerFile), "", true},
		{"Bash reads the owner file", "Bash", "command", "cat " + filepath.Join(root, "mine", OwnerFile), "", false},
	}
	for _, c := range cases {
		if got, _ := hookCall(t, env, c.tool, c.key, c.value, c.cwd); got != c.deny {
			t.Errorf("%s: deny %v, want %v", c.name, got, c.deny)
		}
	}
}

// A search aimed above a project says nothing of the projects it would touch, and the hook finds the library from the
// data folder when its environment names none.
func TestGuardSearchRefusalNamesNoProjectAndFindsTheLibrary(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	base, data := t.TempDir(), t.TempDir()
	root := filepath.Join(base, "design-artifacts")
	makeProject(t, root, "secret", "Тайный план", otherV2)
	l := New(Config{DataDir: data, LibraryDir: root})
	if err := l.SetAccess(snapshot(map[string]Grant{"secret": {Role: "none"}}, map[string]string{"secret": "none"})); err == nil {
		t.Log("grant with role none is refused by Validate, as it should be")
	}
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"secret": "none"})); err != nil {
		t.Fatal(err)
	}
	// No LibraryDir in the hook's environment: the runtime left it in the data folder.
	env := GuardEnv{DataDir: data, Now: accessNow}
	for _, c := range []struct{ tool, key, value string }{
		{"Grep", "path", root}, {"Glob", "pattern", root + "/**/*.html"}, {"Bash", "command", "find " + base + " -name x"},
	} {
		deny, reason := hookCall(t, env, c.tool, c.key, c.value, base)
		if !deny {
			t.Errorf("%s let through", c.tool)
			continue
		}
		if reason != "Поиск задевает закрытые проекты Designly. Укажите папку проекта, к которому есть доступ." {
			t.Errorf("%s: %q", c.tool, reason)
		}
		for _, leak := range []string{"secret", "Тайный", "Олег"} {
			if strings.Contains(reason, leak) {
				t.Errorf("%s: the refusal names %q", c.tool, leak)
			}
		}
	}
	// Naming the project itself gets the plain words too: nothing of its title or owner.
	_, reason := hookCall(t, env, "Read", "file_path", filepath.Join(root, "secret", ProjectDirname, "Main.dc.html"), base)
	if reason != "Нет доступа к проекту" {
		t.Errorf("named project: %q", reason)
	}
}

// A project with no role answers as one that does not exist: 404, nothing of the owner or the title; a person who sees
// it but lacks the level hears whom to ask.
func TestGateWithoutARoleAnswersAsMissing(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "hidden", "Тайный", otherV2)
	makeProject(t, root, "seen", "Видимый", otherV2)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	if err := l.SetAccess(snapshot(map[string]Grant{"seen": {Role: "view", Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"hidden": "none", "seen": "none"})); err != nil {
		t.Fatal(err)
	}
	no := a.gate(httptest.NewRequest(http.MethodGet, "/x", nil), "hidden", RoleView)
	if no == nil || no.Status != 404 {
		t.Fatalf("hidden: %+v", no)
	}
	raw, _ := json.Marshal(no.Body)
	for _, leak := range []string{"owner", "Олег", "no_access", "Тайный"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the 404 says %q: %s", leak, raw)
		}
	}
	if no := a.gate(httptest.NewRequest(http.MethodGet, "/x", nil), "seen", RoleEdit); no == nil || no.Status != 403 {
		t.Fatalf("view asked for edit: %+v", no)
	}
	w := httptest.NewRecorder()
	if !a.denyImage(w, "hidden") || w.Code != 404 {
		t.Errorf("image: %d", w.Code)
	}
}

// A project made here, with no row in the snapshot, is told to the gateway again, once; others are left alone.
func TestReannounceOwnProjectsAbsentFromTheSnapshot(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "lost", "Lost", mineV2)
	makeProject(t, root, "known", "Known", mineV2)
	makeProject(t, root, "teamed", "Teamed", mineV2)
	makeProject(t, root, "theirs", "Theirs", otherV2)
	makeProject(t, root, "legacy", "Legacy", mineV1)
	l := accessLibrary(t, root)
	var mu sync.Mutex
	var events []string
	l.emitFn = func(kind, project string) { mu.Lock(); events = append(events, kind+" "+project); mu.Unlock() }
	l.ReannounceOwn() // no snapshot: that gateway knows nothing of the event
	if len(events) != 0 {
		t.Fatalf("without a snapshot: %v", events)
	}
	if err := l.SetAccess(snapshot(map[string]Grant{"known": {Role: "owner", Owner: "s@x", OwnerName: "Пётр"}}, map[string]string{"teamed": "none"})); err != nil {
		t.Fatal(err)
	}
	l.ReannounceOwn()
	l.ReannounceOwn()
	if len(events) != 1 || events[0] != "design.created lost" {
		t.Fatalf("events: %v", events)
	}
}

// Heavy files are served at their hash only while the person may open the project.
func TestHashedNotServedAfterRevoke(t *testing.T) {
	root := t.TempDir()
	l := accessLibrary(t, root)
	l.srv = &server{}
	makeProject(t, root, "one", "One", otherV2)
	p := filepath.Join(root, "one", "project")
	writeFile(t, filepath.Join(p, "Main.dc.html"), hashedHTML)
	writeFile(t, filepath.Join(p, "lib", "_ds_bundle.js"), heavyBody("ds"))
	writeFile(t, filepath.Join(p, "_ds", "x", "_ds_bundle.js"), heavyBody("ds"))
	if err := l.SetAccess(snapshot(map[string]Grant{"one": {Role: "view", Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"one": "none"})); err != nil {
		t.Fatal(err)
	}
	url := hashedURLRe.FindString(hashedBoard(t, l, "one"))
	if url == "" {
		t.Fatal("no hashed url")
	}
	if w := hashedGet(l, url); w.Code != 200 {
		t.Fatalf("before the revoke: %d", w.Code)
	}
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"one": "none"})); err != nil {
		t.Fatal(err)
	}
	if w := hashedGet(l, url); w.Code != 404 {
		t.Fatalf("after the revoke: %d", w.Code)
	}
}
