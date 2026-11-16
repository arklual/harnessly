package convo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A project is one person's from its creation or by a change, and an empty owner makes it everyone's again; a change of
// the owner alone keeps the folder and the name, and the owner is kept over a restart.
func TestAProjectIsOnePersonsAndBecomesEveryonesAgain(t *testing.T) {
	r := newRig(t)
	folder := filepath.Join(r.dir, "diploma")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	made := r.do(wire.ProjectCreateCmd{OpID: "p1", Path: folder, Name: "Диплом", Owner: "Маша"})
	if made.ID == nil {
		t.Fatalf("project.create: %+v", made)
	}
	id := *made.ID
	last := func() wire.Project {
		evs := r.events(wire.EvProjectUpsert)
		return evs[len(evs)-1].e.(wire.ProjectUpsertEv).Project
	}
	if got := last(); got.Owner != "Маша" {
		t.Fatalf("created: %+v", got)
	}
	artem := "Артём"
	if res := r.do(wire.ProjectUpdateCmd{OpID: "u1", ID: id, Owner: &artem}); res.R != wire.ResultAccepted {
		t.Fatalf("project.update: %+v", res)
	}
	if got := last(); got.Owner != "Артём" || got.Name != "Диплом" || got.Path != folder {
		t.Fatalf("given to another: %+v", got)
	}
	everyone := ""
	r.do(wire.ProjectUpdateCmd{OpID: "u2", ID: id, Owner: &everyone})
	if got := last(); got.Owner != "" {
		t.Fatalf("everyone's: %+v", got)
	}
	r.do(wire.ProjectUpdateCmd{OpID: "u3", ID: id, Owner: &artem})
	r.do(wire.ProjectUpdateCmd{OpID: "u4", ID: id, Name: "Диплом 2"})
	r.rt = r.runtime()
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap, _ := r.rt.Conversations(context.Background())
	if len(snap.Projects) != 1 || snap.Projects[0].Owner != "Артём" || snap.Projects[0].Name != "Диплом 2" {
		t.Fatalf("after the restart: %+v", snap.Projects)
	}
	if err := snap.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A chat keeps who started it and who wrote last: a message a page names its person for moves the last one, a message
// of no person (an agent's, a CLI's) leaves it, and a chat delegated by an agent is for the one who drives its parent.
func TestAChatKeepsWhoStartedItAndWhoWroteLast(t *testing.T) {
	r := newRig(t)
	r.id = r.made(r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: r.dir, Provider: wire.ProviderClaude, By: "Маша",
		First: &wire.FirstMessage{ClientID: "m1", Text: "Начнём", AttachmentIDs: []string{}}}))
	if rec := r.recordOf(r.id); rec.StartedBy != "Маша" || rec.By != "Маша" {
		t.Fatalf("started: %q, %q", rec.StartedBy, rec.By)
	}
	res := r.do(wire.SendCmd{OpID: "m2", ClientID: "m2", ConversationID: r.id, Text: "Помогу", Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, AttachmentIDs: []string{}, By: "Артём"})
	if res.R == wire.ResultRefused {
		t.Fatalf("send: %+v", res)
	}
	if rec := r.recordOf(r.id); rec.StartedBy != "Маша" || rec.By != "Артём" {
		t.Fatalf("after another's message: %q, %q", rec.StartedBy, rec.By)
	}
	r.do(wire.SendCmd{OpID: "m3", ClientID: "m3", ConversationID: r.id, Text: "без имени", Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, AttachmentIDs: []string{}})
	if rec := r.recordOf(r.id); rec.By != "Артём" {
		t.Fatalf("a message of nobody: %q", rec.By)
	}
	child := r.made(r.do(wire.ConversationCreateCmd{OpID: "c2", Cwd: r.dir, Provider: wire.ProviderClaude, ParentID: r.id}))
	if rec := r.recordOf(child); rec.StartedBy != "Артём" || rec.By != "Артём" {
		t.Fatalf("delegated: %q, %q", rec.StartedBy, rec.By)
	}
	// The record is written at most every SaveEvery.
	r.tick(SaveEvery + time.Second)
	r.restart()
	if rec := r.recordOf(r.id); rec.StartedBy != "Маша" || rec.By != "Артём" {
		t.Fatalf("after the restart: %q, %q", rec.StartedBy, rec.By)
	}
}

// A name that is not one line of the allowed length is refused, in a message as in a project.
func TestAPersonsNameIsOneShortLine(t *testing.T) {
	bad := []string{" Маша", "Ма\nша", "очень-длинное-имя-которое-никто-не-выберет-себе"}
	for _, name := range bad {
		if err := (wire.SendCmd{OpID: "m", ClientID: "m", ConversationID: "tab-abcdef", Mode: wire.ModeQueue, Kind: wire.KindPrompt,
			By: name}).Validate(); err == nil {
			t.Errorf("send by %q passed", name)
		}
		if err := (wire.ProjectUpdateCmd{OpID: "u", ID: "prj-1", Owner: &name}).Validate(); err == nil {
			t.Errorf("owner %q passed", name)
		}
	}
	empty := ""
	if err := (wire.ProjectUpdateCmd{OpID: "u", ID: "prj-1", Owner: &empty}).Validate(); err != nil {
		t.Errorf("everyone's: %v", err)
	}
	if err := (wire.ProjectUpdateCmd{OpID: "u", ID: "prj-1"}).Validate(); err == nil {
		t.Error("an update of nothing passed")
	}
}
