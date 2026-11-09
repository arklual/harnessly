package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func memoryPost(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, path string, body any) hubtest.Resp {
	t.Helper()
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	auth, _ := hubproto.SignMemory(m.Priv, m.MachineID, s.Clock.Now(), host)
	return s.Browser().Do("POST", path, body, "Authorization", auth)
}

func memoryPull(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, projects ...string) []hubproto.MemoryNote {
	t.Helper()
	r := memoryPost(t, s, m, hubproto.MemoryPullPath, hubproto.MemoryPull{Projects: projects})
	if r.Code != http.StatusOK {
		t.Fatalf("pull: %s", r)
	}
	var out hubproto.MemoryNotes
	r.JSON(&out)
	return out.Notes
}

func memoryPut(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, put hubproto.MemoryPut) hubproto.MemoryPutResult {
	t.Helper()
	r := memoryPost(t, s, m, hubproto.MemoryPutPath, put)
	if r.Code != http.StatusOK {
		t.Fatalf("put: %s", r)
	}
	var out hubproto.MemoryPutResult
	r.JSON(&out)
	return out
}

// A note one machine of a person writes, another machine of the same person reads; a machine of another person, in
// the same space, sees none of it and cannot write over it.
func TestMemorySharedByThePersonOnly(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_MEMORY_SYNC", "on"))
	owner := s.SignIn("owner@example.com")
	other := s.SignIn("other@example.com")
	space, _ := owner.NewSpace("Acme")
	owner.Join(space, other)
	// Only an owner of the space adds machines: the other person is one too, and still reads none of the owner's notes.
	if err := s.DB.S().SetRole(context.Background(), space, other.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	a := owner.Machine(space, nil)
	b := owner.Machine(space, nil)
	c := other.Machine(space, nil)
	const project = "git:example.com/acme/app"

	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	peers, _ := hubproto.SignPeers(a.Priv, a.MachineID, s.Clock.Now(), host)
	if r := s.Browser().Do("POST", hubproto.MemoryPullPath, hubproto.MemoryPull{Projects: []string{project}}, "Authorization", peers); r.Code != http.StatusUnauthorized {
		t.Fatalf("a signature for messages read the memory: %s", r)
	}

	res := memoryPut(t, s, a, hubproto.MemoryPut{Project: project, Name: "deploy.md", Body: "---\nname: deploy\n---\nsynthetic note\n"})
	if res.Note == nil || res.Note.Version != hubproto.MemoryVersion("---\nname: deploy\n---\nsynthetic note\n") {
		t.Fatalf("put: %+v", res)
	}
	got := memoryPull(t, s, b, project)
	if len(got) != 1 || got[0].Name != "deploy.md" || got[0].Body != "---\nname: deploy\n---\nsynthetic note\n" || got[0].Machine == "" {
		t.Fatalf("the other machine of the person: %+v", got)
	}
	if got := memoryPull(t, s, c, project); len(got) != 0 {
		t.Fatalf("another person read the note: %+v", got)
	}
	// The other person writes the same name: it lands in their own memory, the owner's note is untouched.
	if res := memoryPut(t, s, c, hubproto.MemoryPut{Project: project, Name: "deploy.md", Body: "theirs"}); res.Note == nil {
		t.Fatalf("other's put: %+v", res)
	}
	if got := memoryPull(t, s, a, project); len(got) != 1 || got[0].Body != "---\nname: deploy\n---\nsynthetic note\n" {
		t.Fatalf("another person wrote over the owner's note: %+v", got)
	}
	// Nothing in the body can name a person: an unknown field is ignored.
	if r := memoryPost(t, s, c, hubproto.MemoryPullPath, map[string]any{"projects": []string{project}, "person": "owner@example.com"}); r.Code != http.StatusOK {
		t.Fatalf("pull: %s", r)
	} else {
		var out hubproto.MemoryNotes
		r.JSON(&out)
		if len(out.Notes) != 1 || out.Notes[0].Body != "theirs" {
			t.Fatalf("a claimed person was believed: %+v", out.Notes)
		}
	}
	// Another project of the same person is apart.
	if got := memoryPull(t, s, b, "git:example.com/acme/other"); len(got) != 0 {
		t.Fatalf("another project: %+v", got)
	}
}

// Two machines write the same note from the same version: the second does not overwrite the first, it gets the
// newer note back; a delete leaves a tombstone the others see.
func TestMemoryConflictAndDelete(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_MEMORY_SYNC", "on"))
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	a := owner.Machine(space, nil)
	b := owner.Machine(space, nil)
	const p = "git:example.com/acme/app"
	v1 := memoryPut(t, s, a, hubproto.MemoryPut{Project: p, Name: "n.md", Body: "one"}).Note.Version
	if res := memoryPut(t, s, a, hubproto.MemoryPut{Project: p, Name: "n.md", Body: "from a", Base: v1}); res.Note == nil {
		t.Fatalf("a: %+v", res)
	}
	res := memoryPut(t, s, b, hubproto.MemoryPut{Project: p, Name: "n.md", Body: "from b", Base: v1})
	if res.Note != nil || res.Conflict == nil || res.Conflict.Body != "from a" {
		t.Fatalf("b overwrote a newer note: %+v", res)
	}
	// Writing what is already kept is no conflict.
	if res := memoryPut(t, s, b, hubproto.MemoryPut{Project: p, Name: "n.md", Body: "from a", Base: v1}); res.Note == nil {
		t.Fatalf("same text: %+v", res)
	}
	// A brand-new note over an existing one is a conflict too.
	if res := memoryPut(t, s, b, hubproto.MemoryPut{Project: p, Name: "n.md", Body: "fresh"}); res.Conflict == nil {
		t.Fatalf("a new note overwrote: %+v", res)
	}
	cur := hubproto.MemoryVersion("from a")
	if res := memoryPut(t, s, b, hubproto.MemoryPut{Project: p, Name: "n.md", Delete: true, Base: cur}); res.Note == nil || !res.Note.Deleted {
		t.Fatalf("delete: %+v", res)
	}
	got := memoryPull(t, s, a, p)
	if len(got) != 1 || !got[0].Deleted || got[0].Body != "" || got[0].Version != hubproto.MemoryTombstone {
		t.Fatalf("tombstone: %+v", got)
	}
	if r := memoryPost(t, s, a, hubproto.MemoryPutPath, hubproto.MemoryPut{Project: p, Name: "../x.md", Body: "x"}); r.Code != http.StatusBadRequest {
		t.Fatalf("a path as a name: %s", r)
	}
}

// A revoked machine, and a machine whose person left its space, reach no memory.
func TestMemoryRevokedAndReassigned(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_MEMORY_SYNC", "on"))
	owner := s.SignIn("owner@example.com")
	guest := s.SignIn("guest@example.com")
	space, _ := owner.NewSpace("Acme")
	owner.Join(space, guest)
	if err := s.DB.S().SetRole(context.Background(), space, guest.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	a := owner.Machine(space, nil)
	g := guest.Machine(space, nil)
	const p = "git:example.com/acme/app"
	memoryPut(t, s, g, hubproto.MemoryPut{Project: p, Name: "n.md", Body: "guest's"})

	ctx := context.Background()
	if _, err := s.DB.S().RevokeMachine(ctx, a.MachineID, s.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if r := memoryPost(t, s, a, hubproto.MemoryPullPath, hubproto.MemoryPull{Projects: []string{p}}); r.Code == http.StatusOK {
		t.Fatalf("a revoked machine pulled: %s", r)
	}
	if err := s.DB.S().RemoveMember(ctx, space, guest.ID); err != nil {
		t.Fatal(err)
	}
	if r := memoryPost(t, s, g, hubproto.MemoryPullPath, hubproto.MemoryPull{Projects: []string{p}}); r.Code != http.StatusForbidden {
		t.Fatalf("a machine whose person left the space pulled: %s", r)
	}
}

func TestMemoryOffByDefault(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Acme")
	a := owner.Machine(space, nil)
	if r := memoryPost(t, s, a, hubproto.MemoryPullPath, hubproto.MemoryPull{}); r.Code != http.StatusNotFound {
		t.Fatalf("memory without HUB_MEMORY_SYNC: %s", r)
	}
}
