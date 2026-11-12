//go:build linux

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type designProfileLauncher struct{ requests []convo.LaunchRequest }

func (l *designProfileLauncher) Launch(r convo.LaunchRequest) error {
	l.requests = append(l.requests, r)
	return nil
}
func (*designProfileLauncher) Resume(wire.Conversation) error { return nil }

// Creation must carry the profile into the conversation and the launch, rather than run the vendor's model
// on the native CLI account. The real library, conversation runtime, and profile resolver run without a vendor call.
func TestDesignCreationKeepsBackendProfile(t *testing.T) {
	for _, tc := range []struct{ profile, model string }{{"glm", "glm-5.3"}, {"kimi", "kimi-k3.1-pro"}, {"", "sonnet"}} {
		t.Run(tc.profile+tc.model, func(t *testing.T) {
			data := t.TempDir()
			store := &providers.Store{Dir: filepath.Join(data, "providers")}
			if tc.profile != "" {
				if _, err := store.Set(tc.profile, strings.Repeat("test-only-", 4)); err != nil {
					t.Fatal(err)
				}
			}
			launcher := &designProfileLauncher{}
			rt := convo.NewInline(convo.Deps{MachineID: "m-test", Publisher: &handoffTestPublisher{}, Launcher: launcher, Profiles: providers.Launcher{Store: store}})
			t.Cleanup(rt.Stop)
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			lib := design.New(design.Config{LibraryDir: root, DataDir: data, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
			t.Cleanup(lib.StopWatching)
			d := &v2Designly{v: &v2Live{rt: rt, starter: &v2Launcher{}}, api: &design.API{L: lib}, file: filepath.Join(data, "designly.json")}
			effort := wire.EffortHigh
			made, err := d.Create(context.Background(), wire.DesignlyCreate{Title: "Test", Brief: "A simple page", Kind: wire.DesignSite, Provider: wire.ProviderClaude, Profile: tc.profile, Model: &tc.model, Effort: &effort})
			if err != nil {
				t.Fatal(err)
			}
			rec, ok := rt.Record(made.Conv)
			if !ok || rec.Model == nil || rec.Model.Profile != tc.profile || rec.Model.ID != tc.model || rec.Model.Effort == nil || *rec.Model.Effort != effort {
				t.Fatalf("conversation lost selection: %+v", rec.Model)
			}
			if len(launcher.requests) != 1 {
				t.Fatalf("launches: %d", len(launcher.requests))
			}
			got := launcher.requests[0]
			if got.Provider != wire.ProviderClaude || got.Model == nil || got.Model.Profile != tc.profile || got.Model.ID != tc.model {
				t.Fatalf("launch lost selection: %+v", got)
			}
		})
	}
}
