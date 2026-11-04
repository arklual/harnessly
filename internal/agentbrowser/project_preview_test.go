package agentbrowser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectAffectedChatsCountsOnlyChangedSelectionsWithOwnedTabs(t *testing.T) {
	a, records := profileFixture(t)
	_, first, err := a.List("a")
	must(t, err)
	_, second, err := a.List("b")
	must(t, err)
	third, err := a.Store.Create("Third", "#123456")
	must(t, err)
	for _, conv := range []string{"inherited", "selected", "direct", "empty", "unowned"} {
		records[conv] = records["a"]
		_, _, err := a.List(conv)
		must(t, err)
	}
	baseline := ProjectAccess{Default: first.ID, Granted: []string{second.ID}}
	must(t, a.Store.SetProject("project-a", baseline))
	must(t, a.Store.Select("selected", "project-a", second.ID))
	must(t, a.Store.Grant("direct", "project-a", first.ID, "chat"))
	data := filepath.Dir(filepath.Dir(a.Store.path))
	a.Manager = New(Config{DataDir: data, Chrome: "unavailable", LookupProfile: a.Store.Lookup})
	defer a.Manager.Close()
	dormant := newInstance(a.Manager, first.ID)
	defer dormant.cancel()
	for n, conv := range []string{"a", "a", "inherited", "selected", "direct", "b", "unrelated-owner"} {
		id := string(rune('a' + n))
		dormant.pages[id] = newPage(Tab{ID: id, Conversation: conv, URL: "https://example.org"})
	}
	dormant.changedLocked()
	dormant.save()
	metadata, err := os.ReadFile(a.Store.path)
	must(t, err)
	tabsPath := filepath.Join(dormant.dir, "tabs.json")
	tabs, err := os.ReadFile(tabsPath)
	must(t, err)
	before, err := json.Marshal(a.Store.data)
	must(t, err)
	for _, tt := range []struct {
		name   string
		access ProjectAccess
		want   int
	}{
		{"unchanged", baseline, 0},
		{"additional grants", ProjectAccess{Default: first.ID, Granted: []string{second.ID, third.ID}}, 0},
		{"duplicate grants", ProjectAccess{Default: first.ID, Granted: []string{second.ID, second.ID}}, 0},
		{"revoke selected grant", ProjectAccess{Default: first.ID}, 1},
		{"new default with old default granted", ProjectAccess{Default: second.ID, Granted: []string{first.ID}}, 2},
		{"new default keeps explicit selection and direct grant", ProjectAccess{Default: second.ID}, 2},
		{"new default revokes explicit selection", ProjectAccess{Default: third.ID}, 3},
		{"automatic own default", ProjectAccess{Granted: []string{second.ID}}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			count, err := a.ProjectAffectedChats("project-a", tt.access)
			must(t, err)
			if count != tt.want {
				t.Fatalf("affected chats = %d, want %d", count, tt.want)
			}
			after, err := json.Marshal(a.Store.data)
			must(t, err)
			if string(after) != string(before) {
				t.Fatal("preview changed in-memory access")
			}
			saved, err := os.ReadFile(a.Store.path)
			must(t, err)
			if string(saved) != string(metadata) {
				t.Fatal("preview wrote access metadata")
			}
			saved, err = os.ReadFile(tabsPath)
			must(t, err)
			if string(saved) != string(tabs) {
				t.Fatal("preview changed existing tabs")
			}
			if len(a.notices) != 0 || a.Manager.preparation != nil || len(a.Manager.instances) != 0 {
				t.Fatal("preview produced side effects")
			}
		})
	}
	for _, tt := range []struct {
		project string
		access  ProjectAccess
	}{
		{"", baseline},
		{"project-a", ProjectAccess{Default: "missing"}},
		{"project-a", ProjectAccess{Default: first.ID, Granted: []string{"missing"}}},
	} {
		_, previewErr := a.ProjectAffectedChats(tt.project, tt.access)
		mutationErr := a.SetProject(t.Context(), tt.project, tt.access)
		if previewErr == nil || mutationErr == nil || previewErr.Error() != mutationErr.Error() {
			t.Fatalf("preview = %v, mutation = %v", previewErr, mutationErr)
		}
	}
	// Applying the same access agrees with the preview and closes no unrelated owner's tabs.
	count, err := a.ProjectAffectedChats("project-a", ProjectAccess{Default: third.ID})
	must(t, err)
	must(t, a.SetProject(t.Context(), "project-a", ProjectAccess{Default: third.ID}))
	if count != 3 {
		t.Fatal(count)
	}
	for _, conv := range []string{"a", "inherited", "selected"} {
		if a.Manager.ConversationHasTabs(conv) {
			t.Fatalf("affected tabs survived for %s", conv)
		}
	}
	for _, conv := range []string{"direct", "b", "unrelated-owner"} {
		if !a.Manager.ConversationHasTabs(conv) {
			t.Fatalf("unaffected tabs closed for %s", conv)
		}
	}
	if a.Manager.preparation != nil {
		t.Fatal("mutation started Chrome")
	}
}

func TestProjectAffectedChatsWithoutManager(t *testing.T) {
	a, _ := profileFixture(t)
	_, first, err := a.List("a")
	must(t, err)
	count, err := a.ProjectAffectedChats("project-a", ProjectAccess{Default: first.ID})
	must(t, err)
	if count != 0 {
		t.Fatal(count)
	}
}
