package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func profileFixture(t *testing.T) (*ProfileAccess, map[string]ConversationIdentity) {
	t.Helper()
	records := map[string]ConversationIdentity{
		"a":     {Project: "project-a", ProjectName: "Account A", Title: "Chat A"},
		"b":     {Project: "project-b", ProjectName: "Account B", Title: "Chat B"},
		"b2":    {Project: "project-b", ProjectName: "Account B", Title: "Chat B2"},
		"c":     {Project: "project-c", ProjectName: "Account C", Title: "Chat C"},
		"loose": {Title: "Loose"},
	}
	a := NewProfileAccess(OpenProfiles(t.TempDir(), ""), func(conv string) (ConversationIdentity, bool) { v, ok := records[conv]; return v, ok })
	a.Wait = time.Millisecond
	return a, records
}

func TestProfileStoreIsolationPersistenceAndPermissions(t *testing.T) {
	a, _ := profileFixture(t)
	_, first, err := a.List("a")
	must(t, err)
	_, second, err := a.List("b")
	must(t, err)
	_, common, err := a.List("loose")
	must(t, err)
	if first.ID == second.ID || first.ID == "Account A" || !strings.HasPrefix(first.ID, "bp_") {
		t.Fatal(first, second)
	}
	if common.ID != DefaultProfile {
		t.Fatal("legacy common directory changed")
	}
	list, _, err := a.List("b")
	must(t, err)
	if len(list) != 1 || list[0] != second {
		t.Fatal(list)
	}
	for _, name := range []string{first.Name, "missing", common.Name} {
		_, err := a.UseName(t.Context(), "b", name)
		if !errors.Is(err, ErrProfileUnavailable) || strings.Contains(err.Error(), name) {
			t.Fatal(err)
		}
	}
	p, ok := a.Store.Lookup(first.ID)
	if !ok || len(p.UsedBy) != 1 || p.UsedBy[0].Conv != "a" || p.LastUsedAt == 0 {
		t.Fatal(p)
	}
	st, err := os.Stat(a.Store.path)
	must(t, err)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	reopened := OpenProfiles(filepath.Dir(filepath.Dir(a.Store.path)), "")
	must(t, reopened.Err())
	got, ok := reopened.Lookup(first.ID)
	if !ok || got.Name != first.Name {
		t.Fatal(got)
	}
	must(t, a.Store.SetProject("project-b", ProjectAccess{Default: second.ID, Granted: []string{first.ID}}))
	_, err = a.UseName(t.Context(), "b", first.Name)
	must(t, err)
	p, _, err = a.resolve("b")
	must(t, err)
	if p.ID != first.ID {
		t.Fatal(p)
	}
	must(t, a.Store.SetProject("project-b", ProjectAccess{Default: second.ID}))
	p, _, err = a.resolve("b")
	must(t, err)
	if p.ID != second.ID {
		t.Fatal("revoked selection survived")
	}
}

func TestProfileRequestScopesRetryDeclineAndMove(t *testing.T) {
	for _, scope := range []string{"chat", "project", "decline"} {
		t.Run(scope, func(t *testing.T) {
			a, records := profileFixture(t)
			_, private, err := a.List("a")
			must(t, err)
			answer, err := a.Request(t.Context(), "b", "Need the other account")
			must(t, err)
			if answer.Status != "waiting" {
				t.Fatal(answer)
			}
			pending := a.Asks.Pending()
			if len(pending) != 1 || pending[0].Title != "Chat B" || pending[0].Why != "Need the other account" {
				t.Fatal(pending)
			}
			_, err = a.Request(t.Context(), "b", "Need the other account")
			must(t, err)
			if next := a.Asks.Pending(); len(next) != 1 || next[0].ID != pending[0].ID {
				t.Fatal(next)
			}
			_, err = a.UseName(t.Context(), "b", private.Name)
			if !errors.Is(err, ErrProfileUnavailable) {
				t.Fatal(err)
			}
			must(t, a.Answer(t.Context(), pending[0].ID, private.ID, scope, "", ""))
			answer, err = a.Request(t.Context(), "b", "Need the other account")
			must(t, err)
			expected := "ready"
			if scope == "decline" {
				expected = "declined"
			}
			if answer.Status != expected || scope == "decline" && answer.Name != "" {
				t.Fatal(answer)
			}
			_, err = a.UseName(t.Context(), "b", private.Name)
			if (err == nil) != (scope != "decline") {
				t.Fatal(err)
			}
			_, err = a.UseName(t.Context(), "b2", private.Name)
			if (err == nil) != (scope == "project") {
				t.Fatal(err)
			}
			records["b"] = records["c"]
			_, current, err := a.List("b")
			must(t, err)
			if current.ID == private.ID {
				t.Fatal("selection crossed project move")
			}
			_, err = a.UseName(t.Context(), "b", private.Name)
			if !errors.Is(err, ErrProfileUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

func TestProfileRemovalForkAndMachineLocality(t *testing.T) {
	a, records := profileFixture(t)
	_, p, err := a.List("a")
	must(t, err)
	records["fork"] = records["a"]
	_, fork, err := a.List("fork")
	must(t, err)
	if fork.ID != p.ID {
		t.Fatal("fork did not inherit project default")
	}
	must(t, a.Store.Grant("b", "project-b", p.ID, "chat"))
	must(t, a.ConversationChanged("b", "", true))
	_, err = a.UseName(t.Context(), "b", p.Name)
	if err == nil {
		t.Fatal("deleted chat retained browser access")
	}
	must(t, a.ConversationChanged("b", "project-b", false))
	must(t, a.Store.RemoveProject("project-a"))
	if _, ok := a.Store.Lookup(p.ID); !ok {
		t.Fatal("project removal deleted profile")
	}
	must(t, a.Store.Grant("b", "project-b", p.ID, "project"))
	must(t, a.Delete(t.Context(), p.ID))
	_, bindings := a.Store.Snapshot()
	for _, access := range bindings {
		if access.Default == p.ID || strings.Contains(strings.Join(access.Granted, ","), p.ID) {
			t.Fatal(access)
		}
	}
	other := NewProfileAccess(OpenProfiles(t.TempDir(), ""), a.Identity)
	_, remote, err := other.List("fork")
	must(t, err)
	if remote.ID == fork.ID {
		t.Fatal("machine-local profile was transferred")
	}
}

func TestProfileRegistryFailsClosedAndConcurrentResolution(t *testing.T) {
	a, _ := profileFixture(t)
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Go(func() {
			_, _, err := a.List("a")
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	profiles, _ := a.Store.Snapshot()
	if len(profiles) != 1 {
		t.Fatal(profiles)
	}
	must(t, os.WriteFile(a.Store.path, []byte(`{"profiles":`), 0600))
	broken := OpenProfiles(filepath.Dir(filepath.Dir(a.Store.path)), "")
	a.Store = broken
	if a.ProfileOf("a") == DefaultProfile || a.ProfileOf("unknown") == DefaultProfile {
		t.Fatal("failed open")
	}
	if _, _, err := a.List("a"); err == nil {
		t.Fatal("accepted corrupt registry")
	}
}

func TestProfileRequestWaitAndExpiry(t *testing.T) {
	asks := NewProfileAsks()
	clock := time.Now()
	asks.now = func() time.Time { return clock }
	ask, err := asks.Open("chat", "project", "Title", "Why")
	must(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := asks.Wait(ctx, ask.ID, time.Hour); got.Status != "waiting" {
		t.Fatal(got)
	}
	clock = clock.Add(31 * time.Minute)
	if got := asks.Wait(t.Context(), ask.ID, 0); got.Status != "declined" {
		t.Fatal(got)
	}
	if len(asks.Pending()) != 0 {
		t.Fatal("expired request still visible")
	}
}

func TestProfileSummariesDoNotDiscloseUsageOrCDP(t *testing.T) {
	a, _ := profileFixture(t)
	_, p, err := a.List("a")
	must(t, err)
	raw, err := json.Marshal(p)
	must(t, err)
	for _, key := range []string{"usedBy", "cdp", "lastUsedAt", "Chat A"} {
		if strings.Contains(string(raw), key) {
			t.Fatal(string(raw))
		}
	}
}

func TestProfileCommonLocalizationAndLongProjectNames(t *testing.T) {
	a, records := profileFixture(t)
	t.Setenv("RUNTIME_LOCALE", "ru")
	_, common, err := a.List("loose")
	must(t, err)
	if common.Name != "Общий" {
		t.Fatal(common)
	}
	_, err = a.UseName(t.Context(), "loose", "Общий")
	must(t, err)
	records["a"] = ConversationIdentity{Project: "project-a", ProjectName: strings.Repeat("界", 200)}
	_, first, err := a.List("a")
	must(t, err)
	records["b"] = ConversationIdentity{Project: "project-b", ProjectName: strings.Repeat("界", 200)}
	_, second, err := a.List("b")
	must(t, err)
	if first.ID == second.ID || first.Name == second.Name {
		t.Fatal(first, second)
	}
}

func TestProfileRequestCreationAndActiveRetryPriority(t *testing.T) {
	a, _ := profileFixture(t)
	_, err := a.Request(t.Context(), "b", "First reason")
	must(t, err)
	ask := a.Asks.Pending()[0]
	must(t, a.Answer(t.Context(), ask.ID, "", "decline", "", ""))
	_, err = a.Request(t.Context(), "b", "Second reason")
	must(t, err)
	waiting := a.Asks.Pending()[0]
	// A pending card wins over an older settled card even when the agent repeats an old reason.
	got, err := a.Asks.Open("b", "project-b", "Chat B", "First reason")
	must(t, err)
	if got.ID != waiting.ID {
		t.Fatal("retry returned an old answer")
	}
	must(t, a.Answer(t.Context(), waiting.ID, "", "chat", "New account", "#123456"))
	answer, err := a.Request(t.Context(), "b", "Second reason")
	must(t, err)
	if answer.Status != "ready" || answer.Name != "New account" {
		t.Fatal(answer)
	}
	_, current, err := a.List("b")
	must(t, err)
	if current.Name != "New account" {
		t.Fatal(current)
	}
}

func TestProfileEventRefusesStaleActorRecord(t *testing.T) {
	a, records := profileFixture(t)
	_, _, err := a.List("a")
	must(t, err)
	must(t, a.ConversationChanged("a", "project-b", false))
	if a.ProfileOf("a") != "!unavailable" {
		t.Fatal("move accepted the actor's previous cached project")
	}
	records["a"] = records["b"]
	if a.ProfileOf("a") == "!unavailable" {
		t.Fatal("move did not accept the updated record")
	}
	must(t, a.ConversationChanged("a", "", true))
	if a.ProfileOf("a") != "!unavailable" {
		t.Fatal("deletion accepted the actor's previous cached record")
	}
	must(t, a.ConversationChanged("a", "project-b", false))
	if a.ProfileOf("a") == "!unavailable" {
		t.Fatal("restore remained blocked")
	}
}

func TestProfileImportPublishesOnlySuccessfulCopies(t *testing.T) {
	store := OpenProfiles(t.TempDir(), "")
	source := t.TempDir()
	must(t, os.WriteFile(filepath.Join(source, "Local State"), []byte(`{}`), 0600))
	warning, err := store.Import(t.Context(), source, "Imported account", "#123456")
	must(t, err)
	if !strings.Contains(warning, "system keyring") {
		t.Fatal("import hid its cookie-key limitation")
	}
	profiles, _ := store.Snapshot()
	if len(profiles) != 1 {
		t.Fatal(profiles)
	}
	path := filepath.Join(filepath.Dir(store.path), "profiles", profiles[0].ID, "chrome", "Local State")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, "Local State")); err != nil {
		t.Fatal("source was moved", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Import(ctx, source, "Cancelled account", "#123456"); err == nil {
		t.Fatal("cancelled import succeeded")
	}
	profiles, _ = store.Snapshot()
	if len(profiles) != 1 {
		t.Fatal("failed import published a profile")
	}
}
