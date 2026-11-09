package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// a machine reports the move of its data with its store session; the person who may open it sees how far it
// is and asks to retry; nobody else sees it.
func TestTheJournalOfAMachinesMove(t *testing.T) {
	s := hubtest.New(t)
	var doc struct {
		Capabilities []struct{ ID, Status string } `json:"capabilities"`
	}
	s.Browser().Do("GET", "/api/capabilities", nil).JSON(&doc)
	on := false
	for _, c := range doc.Capabilities {
		on = on || c.ID == hubproto.MigrationCapability && c.Status == "on"
	}
	if !on {
		t.Fatalf("the journal is not among the capabilities: %+v", doc)
	}

	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	a := owner.Machine(space, nil)
	if r := s.Browser().Do("POST", hubproto.MigrationPath+"/report", map[string]any{}); r.Code != http.StatusUnauthorized {
		t.Fatalf("a report without a session: %s", r)
	}
	sess, err := storelink.RequestSession(context.Background(), http.DefaultClient,
		storelink.Machine{ID: a.MachineID, DialURL: dialURL(s), Key: a.Priv}, s.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	bearer := "Bearer " + sess.Token
	// The machine put the picture into the space's uploads first, as its move does.
	png := []byte("png")
	sum := sha256.Sum256(png)
	sha := hex.EncodeToString(sum[:])
	l := linkOf(t, s, a, t.TempDir())
	if err := l.Client("uploads").PutBlob(context.Background(), sha, png); err != nil {
		t.Fatal(err)
	}
	rep := hubproto.MigrationReport{Migration: "0001-data-to-hub", Version: "0.17.0", Total: 3, Objects: []hubproto.MigrationObject{
		{Kind: "uploads", Object: "up_1", Sha256: sha, Size: 3, Mime: "image/png", Name: "a.png", State: hubproto.MigrationVerified},
		{Kind: "uploads", Object: "up_2", Sha256: strings.Repeat("ef", 32), Size: 3, State: hubproto.MigrationVerified},
		{Kind: "files", Object: strings.Repeat("cd", 32), State: hubproto.MigrationFailed, Code: "MIG-READ", Error: "permission denied\x1b[2J"},
	}}
	if r := s.Browser().Do("POST", hubproto.MigrationPath+"/report", rep, "Authorization", bearer); r.Code != 200 {
		t.Fatalf("report: %s", r)
	}

	// up_2 says it is verified, and the hub has no such blob: the hub writes it down as a failure.
	var st hubproto.MigrationStatus
	must(t, owner.Do("GET", "/api/machines/"+a.MachineID+"/migration", nil)).JSON(&st)
	if st.Total != 3 || st.Verified != 1 || st.Failed != 2 || len(st.Failures) != 2 {
		t.Fatalf("status = %+v", st)
	}
	codes := map[string]string{}
	for _, f := range st.Failures {
		codes[f.Object] = f.Code
		if strings.ContainsRune(f.Error, 0x1b) {
			t.Fatalf("an escape reached the journal: %q", f.Error)
		}
	}
	if codes["up_2"] != "MIG-VERIFY" || codes[strings.Repeat("cd", 32)] != "MIG-READ" {
		t.Fatalf("failures = %+v", st.Failures)
	}
	eve := s.SignIn("eve@example.com")
	if r := eve.Do("GET", "/api/machines/"+a.MachineID+"/migration", nil); r.Code == 200 {
		t.Fatalf("a stranger read the journal: %s", r)
	}
	if r := eve.Do("POST", "/api/machines/"+a.MachineID+"/migration/skip", map[string]any{}); r.Code == 200 {
		t.Fatalf("a stranger skipped objects: %s", r)
	}

	// The upload the machine moved is its person's in the hub, as one the page put there.
	var refs int
	if err := s.DB.S().Q().QueryRow(context.Background(), `SELECT COUNT(*) FROM upload_refs WHERE machine_id = ? AND upload_id = 'up_1' AND sha256 = ?`,
		a.MachineID, sha).Scan(&refs); err != nil || refs != 1 {
		t.Fatalf("upload ref = %d, %v", refs, err)
	}

	must(t, owner.Do("POST", "/api/machines/"+a.MachineID+"/migration/retry", map[string]any{}))
	var ans hubproto.MigrationAnswer
	rep.Objects = nil
	must(t, s.Browser().Do("POST", hubproto.MigrationPath+"/report", rep, "Authorization", bearer)).JSON(&ans)
	if len(ans.Retry) != 2 {
		t.Fatalf("answer = %+v, want both failed objects to retry", ans)
	}
	var own hubproto.MigrationStatus
	must(t, s.Browser().Do("GET", hubproto.MigrationPath+"/status", nil, "Authorization", bearer)).JSON(&own)
	if own.Retrying != 2 || own.Doc != "docs/migrations/0001-data-to-hub.md" {
		t.Fatalf("own status = %+v", own)
	}
}

// A hub with the store off keeps no journal, and says so in its capabilities.
func TestNoJournalWithTheStoreOff(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_STORE", "off"))
	if s.Hub.Migration != nil {
		t.Fatal("a journal with HUB_STORE=off")
	}
	var doc struct {
		Capabilities []struct{ ID, Status string } `json:"capabilities"`
	}
	s.Browser().Do("GET", "/api/capabilities", nil).JSON(&doc)
	for _, c := range doc.Capabilities {
		if c.ID == hubproto.MigrationCapability && c.Status == "on" {
			t.Fatal("the journal is on with the store off")
		}
	}
}

func must(t *testing.T, r hubtest.Resp) hubtest.Resp {
	t.Helper()
	if r.Code != http.StatusOK {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	return r
}

// a machine merges its projects and favorites into its person's sidebar with its store session; the journal
// counts the object verified only once the sidebar lists the machine; a hub without history answers 404.
func TestAMachineMergesItsSidebarIntoItsPersons(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	a := owner.Machine(space, nil)
	side := hubproto.MigrationSidebar{
		Projects:  []hubproto.MigrationProject{{ID: "proj-1", Name: "Работа", CreatedAt: 1}},
		Favorites: &hubproto.MigrationFavorites{Groups: []hubproto.MigrationFavGroup{{ID: "fg-abcd", Name: "Важное", Chats: []string{"tab-a"}}}, Loose: []string{"tab-b"}},
	}
	if r := s.Browser().Do("POST", hubproto.MigrationPath+"/sidebar", side); r.Code != http.StatusUnauthorized {
		t.Fatalf("a sidebar without a session: %s", r)
	}
	sess, err := storelink.RequestSession(context.Background(), http.DefaultClient,
		storelink.Machine{ID: a.MachineID, DialURL: dialURL(s), Key: a.Priv}, s.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	bearer := "Bearer " + sess.Token
	obj := hubproto.MigrationObject{Kind: hubproto.MigrationSidebarKind, Object: hubproto.MigrationSidebarObject,
		Sha256: strings.Repeat("ab", 32), State: hubproto.MigrationVerified}
	rep := hubproto.MigrationReport{Migration: "0001-data-to-hub", Version: "0.17.0", Total: 1, Objects: []hubproto.MigrationObject{obj}}
	// Called verified before the merge: the hub does not believe it.
	must(t, s.Browser().Do("POST", hubproto.MigrationPath+"/report", rep, "Authorization", bearer))
	var st hubproto.MigrationStatus
	must(t, owner.Do("GET", "/api/machines/"+a.MachineID+"/migration", nil)).JSON(&st)
	if st.Failed != 1 || st.Failures[0].Code != "MIG-VERIFY" {
		t.Fatalf("status before the merge = %+v", st)
	}
	for range 2 {
		var ans hubproto.MigrationSidebarAnswer
		must(t, s.Browser().Do("POST", hubproto.MigrationPath+"/sidebar", side, "Authorization", bearer)).JSON(&ans)
		if !ans.Merged {
			t.Fatalf("answer = %+v", ans)
		}
	}
	var got struct {
		Rev int
		Doc struct {
			Projects []struct {
				Name    string
				Members []struct{ MachineID, ProjectID string }
			}
			Favorites struct {
				Groups []struct{ Name string }
				Loose  []string
			}
			Merged []string
		}
	}
	var raw string
	if err := s.DB.S().Q().QueryRow(context.Background(), `SELECT rev, doc FROM history_sidebars`).Scan(&got.Rev, &raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &got.Doc); err != nil {
		t.Fatal(err)
	}
	d := got.Doc
	if got.Rev != 1 || len(d.Projects) != 1 || d.Projects[0].Members[0].MachineID != a.MachineID || len(d.Favorites.Groups) != 1 ||
		len(d.Favorites.Loose) != 1 || len(d.Merged) != 1 || d.Merged[0] != a.MachineID {
		t.Fatalf("sidebar = %+v", got)
	}
	must(t, s.Browser().Do("POST", hubproto.MigrationPath+"/report", rep, "Authorization", bearer))
	must(t, owner.Do("GET", "/api/machines/"+a.MachineID+"/migration", nil)).JSON(&st)
	if st.Verified != 1 || st.Failed != 0 {
		t.Fatalf("status after the merge = %+v", st)
	}

	off := hubtest.New(t, hubtest.Env("HUB_HISTORY", "off"))
	o := off.SignIn("owner@example.com")
	sp, _ := o.NewSpace("Acme")
	b := o.Machine(sp, nil)
	sb, err := storelink.RequestSession(context.Background(), http.DefaultClient,
		storelink.Machine{ID: b.MachineID, DialURL: dialURL(off), Key: b.Priv}, off.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r := off.Browser().Do("POST", hubproto.MigrationPath+"/sidebar", side, "Authorization", "Bearer "+sb.Token); r.Code != http.StatusNotFound {
		t.Fatalf("a hub without history: %s", r)
	}
}
