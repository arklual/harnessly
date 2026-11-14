package convo

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type personKey struct{}

// a chat keeps the person it was started for — whose page session sent the create — and a delegated chat
// its root's, whoever's session asked; the record keeps it over a restart and never sends it to a page.
func TestCreateKeepsThePerson(t *testing.T) {
	r := newRig(t)
	r.deps = func(d *Deps) {
		d.PersonOf = func(ctx context.Context) string {
			p, _ := ctx.Value(personKey{}).(string)
			return p
		}
	}
	r.rt = r.runtime()
	as := func(person string, cmd wire.Command) wire.CmdResult {
		return r.rt.Do(context.WithValue(context.Background(), personKey{}, person), "dev-1", cmd)
	}
	root := r.made(as("roma@team.test", wire.ConversationCreateCmd{OpID: "p", Cwd: "/w", Provider: wire.ProviderClaude}))
	kid := r.made(as("", wire.ConversationCreateCmd{OpID: "c", Cwd: "/w", Provider: wire.ProviderClaude, ParentID: root}))
	mine := r.made(r.do(wire.ConversationCreateCmd{OpID: "m", Cwd: "/w", Provider: wire.ProviderClaude}))
	for id, want := range map[string]string{root: "roma@team.test", kid: "roma@team.test", mine: ""} {
		if rec, _ := r.rt.Record(id); rec.Person != want {
			t.Fatalf("%s: person %q, want %q", id, rec.Person, want)
		}
	}
	r.rt.Stop()
	r.restart()
	if rec, _ := r.rt.Record(kid); rec.Person != "roma@team.test" {
		t.Fatalf("after a restart: %q", rec.Person)
	}
}
