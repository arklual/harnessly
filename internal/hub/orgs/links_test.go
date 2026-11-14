package orgs_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The organization's links for the chat are checked and brought to one form by PATCH, come back in the
// organization's view, and a change of them is told to the machines.
func TestPatchLinksValidatesAndNormalizes(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	vera := s.SignIn("vera@example.com")
	space, _ := anna.NewSpace("Example")
	anna.Join(space, vera)
	o := create(t, anna, "Example", space)
	path := "/api/orgs/" + o.Org.ID
	get := func() hubproto.LinkSettings {
		t.Helper()
		var v struct {
			Org struct{ Links hubproto.LinkSettings }
		}
		r := anna.Do("GET", path, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("get: %s", r)
		}
		r.JSON(&v)
		return v.Org.Links
	}
	patch := func(links map[string]any) hubtest.Resp {
		t.Helper()
		return anna.Do("PATCH", path, map[string]any{"links": links})
	}

	if !(&hubproto.LinkSettings{}).Empty() || get().StandDomain != "" {
		t.Fatalf("a new organization names nothing: %+v", get())
	}
	if r := vera.Do("PATCH", path, map[string]any{"links": map[string]any{"standDomain": "a.example.com"}}); r.Code != http.StatusForbidden {
		t.Errorf("a member changed the links: %s", r)
	}
	for name, links := range map[string]map[string]any{
		"scheme in the domain":  {"standDomain": "https://preview.example.com"},
		"path in the domain":    {"standDomain": "preview.example.com/x"},
		"one label":             {"standDomain": "localhost"},
		"tracker with a path":   {"trackerUrl": "https://tracker.example.com/issue"},
		"tracker without https": {"trackerUrl": "tracker.example.com"},
		"ftp tracker":           {"trackerUrl": "ftp://tracker.example.com"},
		"documents with a user": {"artifactsUrl": "https://u:p@a.example.com"},
		"boards with a query":   {"boardsUrl": "https://b.example.com?x=1"},
		"gitlab with a scheme":  {"gitlabHosts": []string{"https://gitlab.example.com"}},
		"gitlab with a path":    {"gitlabHosts": []string{"gitlab.example.com/group"}},
	} {
		if r := patch(links); r.Code != http.StatusBadRequest {
			t.Errorf("%s: %s", name, r)
		}
	}
	if got := get(); got.StandDomain != "" || got.TrackerURL != "" {
		t.Fatalf("a refused patch changed the links: %+v", got)
	}

	r := patch(map[string]any{
		"standDomain": " *.Preview.Example.com ", "trackerUrl": "https://tracker.example.com/",
		"gitlabHosts":  []string{"GitLab.example.com, git.example.com", "gitlab.example.com", ""},
		"artifactsUrl": "http://a.example.com", "boardsUrl": "https://b.example.com//",
	})
	if r.Code != http.StatusOK {
		t.Fatalf("a good patch: %s", r)
	}
	got := get()
	want := hubproto.LinkSettings{StandDomain: "preview.example.com", TrackerURL: "https://tracker.example.com",
		GitLabHosts: []string{"gitlab.example.com", "git.example.com"}, ArtifactsURL: "http://a.example.com", BoardsURL: "https://b.example.com"}
	if got.StandDomain != want.StandDomain || got.TrackerURL != want.TrackerURL || got.ArtifactsURL != want.ArtifactsURL ||
		got.BoardsURL != want.BoardsURL || !slices.Equal(got.GitLabHosts, want.GitLabHosts) {
		t.Fatalf("links = %+v, want %+v", got, want)
	}

	// A patch without links leaves them; an empty object clears them.
	if r := anna.Do("PATCH", path, map[string]any{"name": "Example 2"}); r.Code != http.StatusOK {
		t.Fatalf("rename: %s", r)
	}
	if got := get(); got.StandDomain != want.StandDomain {
		t.Fatalf("a patch without links cleared them: %+v", got)
	}
	if r := patch(map[string]any{}); r.Code != http.StatusOK {
		t.Fatalf("clear: %s", r)
	}
	if got := get(); !got.Empty() {
		t.Fatalf("not cleared: %+v", got)
	}
}
