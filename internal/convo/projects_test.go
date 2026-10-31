package convo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A folder made a project goes out as project_upsert at the seq the answer names, is in the next convs snapshot with
// the default folder, and the same folder again is the same project.
func TestAFolderMadeAProjectIsPublishedAndListed(t *testing.T) {
	r := newRig(t)
	folder := filepath.Join(r.dir, "work")
	if err := os.MkdirAll(filepath.Join(folder, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := r.do(wire.ProjectCreateCmd{OpID: "p1", Path: folder})
	if res.R != wire.ResultAccepted || res.ID == nil || res.Ch != wire.ChannelConvs {
		t.Fatalf("project.create: %+v", res)
	}
	evs := r.events(wire.EvProjectUpsert)
	if len(evs) != 1 || evs[0].seq != res.Seq {
		t.Fatalf("events %+v, answer at %d", evs, res.Seq)
	}
	got := evs[0].e.(wire.ProjectUpsertEv).Project
	if got.ID != *res.ID || got.Path != folder || got.Name != "work" || !got.Git || got.MachineID != "m1" {
		t.Fatalf("project: %+v", got)
	}
	again := r.do(wire.ProjectCreateCmd{OpID: "p2", Path: folder + "/"})
	if again.ID == nil || *again.ID != *res.ID || len(r.events(wire.EvProjectUpsert)) != 2 {
		t.Fatalf("the same folder again: %+v", again)
	}
	snap, err := r.rt.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Projects) != 1 || snap.Projects[0] != got {
		t.Fatalf("snapshot projects: %+v", snap.Projects)
	}
	if err := snap.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAFileOrAnAbsentFolderIsNotAProject(t *testing.T) {
	r := newRig(t)
	file := filepath.Join(r.dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, filepath.Join(r.dir, "absent")} {
		if res := r.do(wire.ProjectCreateCmd{OpID: "p" + path, Path: path}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
			t.Fatalf("%s: %+v", path, res)
		}
	}
	if snap, _ := r.rt.Conversations(context.Background()); len(snap.Projects) != 0 {
		t.Fatalf("projects: %+v", snap.Projects)
	}
}

// A project outlives the runtime, and a conversation made in it starts in its folder.
func TestAProjectIsKeptAndAConversationStartsInIt(t *testing.T) {
	r := newRig(t)
	folder := filepath.Join(r.dir, "kept")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	res := r.do(wire.ProjectCreateCmd{OpID: "p1", Path: folder})
	if res.ID == nil {
		t.Fatalf("project.create: %+v", res)
	}
	r.rt = r.runtime()
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap, _ := r.rt.Conversations(context.Background())
	if len(snap.Projects) != 1 || snap.Projects[0].ID != *res.ID {
		t.Fatalf("after the restart: %+v", snap.Projects)
	}
	made := r.do(wire.ConversationCreateCmd{OpID: "c1", ProjectID: *res.ID, Provider: wire.ProviderClaude})
	if made.R != wire.ResultAccepted || made.ID == nil {
		t.Fatalf("create: %+v", made)
	}
	if len(r.launcher.launched) != 1 || r.launcher.launched[0].Cwd != folder {
		t.Fatalf("launched: %+v", r.launcher.launched)
	}
}

// A project is called what the person named it, else by its folder; the same folder again keeps its first name.
func TestAProjectIsCalledWhatThePersonNamedIt(t *testing.T) {
	r := newRig(t)
	named, plain := filepath.Join(r.dir, "app-1"), filepath.Join(r.dir, "app-2")
	for _, d := range []string{named, plain} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.do(wire.ProjectCreateCmd{OpID: "p1", Path: named, Name: "  Платформа  "})
	r.do(wire.ProjectCreateCmd{OpID: "p2", Path: plain})
	r.do(wire.ProjectCreateCmd{OpID: "p3", Path: named, Name: "Другое"})
	snap, _ := r.rt.Conversations(context.Background())
	if len(snap.Projects) != 2 || snap.Projects[0].Name != "Платформа" || snap.Projects[1].Name != "app-2" {
		t.Fatalf("projects: %+v", snap.Projects)
	}
}

// Legacy's projects are groups: two of them over one folder stay two, each under its id and name with its
// conversations, and a conversation the person moves afterwards is not put back.
func TestLegacyProjectsAreTakenOverOnce(t *testing.T) {
	r := newRig(t)
	r.rt.Import([]ImportTab{
		{ID: "tab-a00001", Session: "s1", Provider: wire.ProviderClaude, Cwd: r.dir},
		{ID: "tab-b00002", Session: "s2", Provider: wire.ProviderClaude, Cwd: r.dir},
	}, nil)
	legacy := []LegacyProject{{ID: "prj-admin1", Name: "admin"}, {ID: "prj-client", Name: "Клиентка"}}
	assign := map[string]string{"tab-a00001": "prj-admin1", "tab-b00002": "prj-client", "tab-gone01": "prj-admin1"}
	if n := r.rt.ImportProjects(legacy, assign); n != 2 {
		t.Fatalf("placed %d", n)
	}
	snap, _ := r.rt.Conversations(context.Background())
	if len(snap.Projects) != 2 || snap.Projects[0].Name != "admin" || snap.Projects[1].ID != "prj-client" || snap.Projects[1].Path != r.dir {
		t.Fatalf("projects: %+v", snap.Projects)
	}
	in := map[string]string{}
	for _, c := range snap.Conversations {
		if c.ProjectID != nil {
			in[c.ID] = *c.ProjectID
		}
	}
	if in["tab-a00001"] != "prj-admin1" || in["tab-b00002"] != "prj-client" {
		t.Fatalf("placed: %v", in)
	}
	r.do(wire.ConversationMoveCmd{OpID: "mv", ID: "tab-a00001", ProjectID: nil})
	r.restart()
	if n := r.rt.ImportProjects(legacy, assign); n != 0 {
		t.Fatalf("placed again: %d", n)
	}
	snap, _ = r.rt.Conversations(context.Background())
	for _, c := range snap.Conversations {
		if c.ID == "tab-a00001" && c.ProjectID != nil {
			t.Fatalf("moved out, put back: %v", *c.ProjectID)
		}
	}
	if len(snap.Projects) != 2 {
		t.Fatalf("projects after a restart: %+v", snap.Projects)
	}
}

func TestLegacyOrganizerNamesTabsAsConversations(t *testing.T) {
	file := filepath.Join(t.TempDir(), "organizer.json")
	raw := `{"version":1,"projects":[{"id":"prj-admin1","name":"admin","color":"blue"},{"id":"prj-x","name":""}],
		"assignments":{"agent:runner-2:tab-a00001":"prj-admin1","agent:tab-b00002":"prj-admin1","chat:0123456789":"prj-admin1"},"pins":[]}`
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, assign, err := LegacyOrganizer(file)
	if err != nil || len(projects) != 1 || projects[0] != (LegacyProject{ID: "prj-admin1", Name: "admin"}) {
		t.Fatalf("projects %+v, %v", projects, err)
	}
	if len(assign) != 2 || assign["tab-a00001"] != "prj-admin1" || assign["tab-b00002"] != "prj-admin1" {
		t.Fatalf("assignments %v", assign)
	}
	if p, a, err := LegacyOrganizer(filepath.Join(t.TempDir(), "absent")); p != nil || a != nil || err != nil {
		t.Fatalf("absent: %v %v %v", p, a, err)
	}
}

// A project of a name alone is a group over the default folder: two such names stay two projects, a chat made in one
// starts in that folder, and with no default folder there is nowhere to start it.
func TestAProjectOfANameAloneStartsInTheDefaultFolder(t *testing.T) {
	r := newRig(t)
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, DefaultCwd: r.dir})
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := r.do(wire.ProjectCreateCmd{OpID: "p1", Name: " Хакатон "})
	second := r.do(wire.ProjectCreateCmd{OpID: "p2", Name: "Клиентка"})
	if first.R != wire.ResultAccepted || first.ID == nil || second.ID == nil || *first.ID == *second.ID {
		t.Fatalf("project.create: %+v %+v", first, second)
	}
	snap, _ := r.rt.Conversations(context.Background())
	if len(snap.Projects) != 2 || snap.Projects[0].Name != "Хакатон" || snap.Projects[0].Path != r.dir {
		t.Fatalf("projects: %+v", snap.Projects)
	}
	made := r.do(wire.ConversationCreateCmd{OpID: "c1", ProjectID: *first.ID, Provider: wire.ProviderClaude})
	if made.R != wire.ResultAccepted || len(r.launcher.launched) != 1 || r.launcher.launched[0].Cwd != r.dir {
		t.Fatalf("create: %+v %+v", made, r.launcher.launched)
	}
	bare := newRig(t)
	if res := bare.do(wire.ProjectCreateCmd{OpID: "p1", Name: "x"}); res.R != wire.ResultRefused {
		t.Fatalf("without a default folder: %+v", res)
	}
}

// A project's folder may change: its next chat starts in the new one, the chat it had stays where it started, the
// project keeps its id and the change outlives the runtime. A folder that is not there changes nothing.
func TestAProjectMovesToAnotherFolder(t *testing.T) {
	r := newRig(t)
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, DefaultCwd: r.dir})
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(r.dir, "acme")
	if err := os.MkdirAll(filepath.Join(folder, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	made := r.do(wire.ProjectCreateCmd{OpID: "p1", Name: "Витрина"})
	if made.ID == nil {
		t.Fatalf("project.create: %+v", made)
	}
	id := *made.ID
	r.do(wire.ConversationCreateCmd{OpID: "c1", ProjectID: id, Provider: wire.ProviderClaude})

	for _, path := range []string{filepath.Join(r.dir, "absent"), filepath.Join(folder, ".git", "HEAD")} {
		if res := r.do(wire.ProjectUpdateCmd{OpID: "u" + path, ID: id, Path: path}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
			t.Fatalf("%s: %+v", path, res)
		}
	}
	if res := r.do(wire.ProjectUpdateCmd{OpID: "u-none", ID: "prj-absent", Path: folder}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
		t.Fatalf("no such project: %+v", res)
	}
	res := r.do(wire.ProjectUpdateCmd{OpID: "u1", ID: id, Path: folder + "/"})
	if res.R != wire.ResultAccepted {
		t.Fatalf("project.update: %+v", res)
	}
	evs := r.events(wire.EvProjectUpsert)
	got := evs[len(evs)-1].e.(wire.ProjectUpsertEv).Project
	if evs[len(evs)-1].seq != res.Seq || got.ID != id || got.Path != folder || !got.Git || got.Name != "Витрина" {
		t.Fatalf("published %+v at %d, answer %+v", got, evs[len(evs)-1].seq, res)
	}
	r.do(wire.ProjectUpdateCmd{OpID: "u2", ID: id, Name: " Витрина 2 "})

	r.restart()
	snap, _ := r.rt.Conversations(context.Background())
	if len(snap.Projects) != 1 || snap.Projects[0].ID != id || snap.Projects[0].Path != folder || snap.Projects[0].Name != "Витрина 2" {
		t.Fatalf("after the restart: %+v", snap.Projects)
	}
	r.do(wire.ConversationCreateCmd{OpID: "c2", ProjectID: id, Provider: wire.ProviderClaude})
	if len(r.launcher.launched) != 2 || r.launcher.launched[0].Cwd != r.dir || r.launcher.launched[1].Cwd != folder {
		t.Fatalf("launched: %+v", r.launcher.launched)
	}
}

// A deleted project goes as project_removed after its chats: kept, they are in the common list; deleted with it, they
// are removed, and a restore brings them back out of any project. After a restart the project is not there.
func TestAProjectIsDeletedWithOrWithoutItsChats(t *testing.T) {
	for _, chats := range wire.ProjectChatsAll {
		t.Run(string(chats), func(t *testing.T) {
			r := newRig(t)
			r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
				Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, DefaultCwd: r.dir})
			if err := r.rt.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			made := r.do(wire.ProjectCreateCmd{OpID: "p1", Name: "Витрина"})
			other := r.do(wire.ProjectCreateCmd{OpID: "p2", Name: "Другое"})
			id := *made.ID
			in := r.do(wire.ConversationCreateCmd{OpID: "c1", ProjectID: id, Provider: wire.ProviderClaude})
			out := r.do(wire.ConversationCreateCmd{OpID: "c2", ProjectID: *other.ID, Provider: wire.ProviderClaude})
			if in.ID == nil || out.ID == nil {
				t.Fatalf("conversation.create: %+v %+v", in, out)
			}

			if res := r.do(wire.ProjectDeleteCmd{OpID: "d-none", ID: "prj-absent", Chats: chats}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
				t.Fatalf("no such project: %+v", res)
			}
			res := r.do(wire.ProjectDeleteCmd{OpID: "d1", ID: id, Chats: chats})
			if res.R != wire.ResultAccepted {
				t.Fatalf("project.delete: %+v", res)
			}
			last := r.pub.all[len(r.pub.all)-1]
			if ev, ok := last.e.(wire.ProjectRemovedEv); !ok || ev.ID != id || last.seq != res.Seq {
				t.Fatalf("last published %+v at %d, answer %+v", last.e, last.seq, res)
			}

			snap, _ := r.rt.Conversations(context.Background())
			if len(snap.Projects) != 1 || snap.Projects[0].ID != *other.ID {
				t.Fatalf("projects: %+v", snap.Projects)
			}
			listed := map[string]*string{}
			for _, c := range snap.Conversations {
				listed[c.ID] = c.ProjectID
			}
			if p, ok := listed[*out.ID]; !ok || p == nil || *p != *other.ID {
				t.Fatalf("the other project's chat: %v", listed)
			}
			p, kept := listed[*in.ID]
			if chats == wire.ProjectChatsKeep && (!kept || p != nil) || chats == wire.ProjectChatsDelete && kept {
				t.Fatalf("%s: the project's chat: %v", chats, listed)
			}
			if chats == wire.ProjectChatsDelete {
				if res := r.do(wire.ConversationRestoreCmd{OpID: "r1", ID: *in.ID}); res.R != wire.ResultAccepted {
					t.Fatalf("restore: %+v", res)
				}
				if rec := r.rt.actor(*in.ID).c.rec; rec.ProjectID != nil {
					t.Fatalf("restored into %v", *rec.ProjectID)
				}
			}

			r.restart()
			snap, _ = r.rt.Conversations(context.Background())
			if len(snap.Projects) != 1 || snap.Projects[0].ID != *other.ID {
				t.Fatalf("after the restart: %+v", snap.Projects)
			}
		})
	}
}
