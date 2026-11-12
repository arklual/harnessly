package machines

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type gateStub struct{ on bool }

func (g gateStub) On(context.Context, string, string) bool { return g.on }

// A machine's windows are kept only where the resources feature is on, one row per provider and window, with the
// percentage held to 0..100; an unknown provider is not kept.
func TestOnLimitsKeepsLastReading(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := db.S()
	u := &store.User{Email: "alice@example.com", Name: "Alice"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	sp := &store.Space{Slug: "team", Name: "Team", CreatedBy: u.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	m := &store.Machine{SpaceID: sp.ID, Profile: "dir", Via: "command", KeyPub: "k", EncPub: "e", CreatedBy: u.ID}
	if err := st.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_700_000_000_000)
	s := &Service{db: db, gate: gateStub{on: true}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: func() time.Time { return now }}
	s.res.init()

	ev := hubproto.LimitsWindows{Providers: map[string]hubproto.ProviderWindows{
		"claude": {FiveHour: &hubproto.LimitWindow{UsedPct: 12}, SevenDay: &hubproto.LimitWindow{UsedPct: 180, ResetsAt: 1_700_500_000}},
		"other":  {SevenDay: &hubproto.LimitWindow{UsedPct: 5}},
	}}
	s.onLimits(ctx, *m, ev)
	got, err := st.ProviderWindowsOfMachine(ctx, m.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("windows %+v, %v", got, err)
	}
	for _, w := range got {
		if w.Provider != "claude" || w.SampledAt != now.UnixMilli() {
			t.Errorf("window %+v", w)
		}
		if w.Window == "seven_day" && (w.UsedPct != 100 || w.ResetsAt != 1_700_500_000) {
			t.Errorf("seven day %+v", w)
		}
	}
	// The next reading replaces the last one.
	ev.Providers["claude"] = hubproto.ProviderWindows{SevenDay: &hubproto.LimitWindow{UsedPct: 7}}
	s.onLimits(ctx, *m, ev)
	if got, _ = st.ProviderWindowsOfMachine(ctx, m.ID); len(got) != 2 {
		t.Fatalf("after a second reading %+v", got)
	}
	// Where the feature is off, nothing is kept.
	s2 := &Service{db: db, gate: gateStub{on: false}, log: s.log, now: s.now}
	s2.res.init()
	m2 := *m
	m2.ID = "none"
	s2.onLimits(ctx, m2, ev)
	if got, _ := st.ProviderWindowsOfMachine(ctx, "none"); len(got) != 0 {
		t.Fatalf("feature off: %+v", got)
	}
}
