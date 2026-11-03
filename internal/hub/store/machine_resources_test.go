package store

import (
	"context"
	"testing"
	"time"
)

func seedMachine(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.S().CreateMachine(context.Background(), &Machine{ID: id, Name: id, KeyPub: "k" + id, EncPub: "e" + id}); err != nil {
		t.Fatal(err)
	}
}

func TestMachineResourcesRoundTrip(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	if _, err := s.ResourcesByMachine(ctx, "m1"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	cpu, ram, disk, price := 4, 8, 80, int64(129000)
	at := time.UnixMilli(1_700_000_000_000).UTC()
	r := MachineResources{MachineID: "m1", Provider: "http", ExternalID: "vm-1", CPU: &cpu, RAMGB: &ram, DiskGB: &disk,
		PriceAmount: &price, PriceCurrency: "RUB", PricePeriod: "month", UpdatedAt: at}
	if err := s.PutResources(ctx, r); err != nil {
		t.Fatal(err)
	}
	cpu = 8
	r.CPU = &cpu
	if err := s.PutResources(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResourcesByMachine(ctx, "m1")
	if err != nil || *got.CPU != 8 || *got.PriceAmount != 129000 || got.PriceCurrency != "RUB" || got.Plan != "{}" || !got.UpdatedAt.Equal(at) {
		t.Fatalf("got %+v err %v", got, err)
	}
	// A machine without a price keeps NULLs.
	seedMachine(t, db, "m2")
	if err := s.PutResources(ctx, MachineResources{MachineID: "m2", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	all, err := s.ResourcesByMachines(ctx, []string{"m1", "m2", "m3"})
	if err != nil || len(all) != 2 || all["m2"].PriceAmount != nil || all["m2"].CPU != nil {
		t.Fatalf("got %+v err %v", all, err)
	}
}

func TestMachineHW(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	h := MachineHW{MachineID: "m1", Cores: 8, RAMTotal: 16 << 30, DiskTotal: 500 << 30, OS: "linux", Arch: "amd64", Laptop: true, UpdatedAt: time.Now().UTC()}
	for i := 0; i < 2; i++ {
		if err := s.PutHW(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.HWByMachines(ctx, []string{"m1"})
	if err != nil || got["m1"].Cores != 8 || !got["m1"].Laptop || got["m1"].RAMTotal != 16<<30 {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestMachineOps(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	seedMachine(t, db, "m2")
	t0 := time.UnixMilli(1_700_000_000_000).UTC()
	a := &MachineOp{MachineID: "m1", Kind: OpResize, CreatedAt: t0}
	b := &MachineOp{MachineID: "m1", Kind: OpReboot, CreatedAt: t0.Add(time.Second)}
	c := &MachineOp{MachineID: "m2", Kind: OpDrain, CreatedAt: t0.Add(2 * time.Second)}
	for _, o := range []*MachineOp{a, b, c} {
		if err := s.CreateOp(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	ok, err := s.SetOp(ctx, a.ID, OpRunning, "p-1", "", t0.Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("ok %v err %v", ok, err)
	}
	ok, _ = s.SetOp(ctx, a.ID, OpDone, "", "", t0.Add(2*time.Minute))
	if !ok {
		t.Fatal("running op must finish")
	}
	if ok, _ := s.SetOp(ctx, a.ID, OpCancelled, "", "", t0.Add(3*time.Minute)); ok {
		t.Fatal("a finished op must not change")
	}
	got, _ := s.OpByID(ctx, a.ID)
	if got.Status != OpDone || got.ProviderOpID != "p-1" || got.FinishedAt == nil {
		t.Fatalf("got %+v", got)
	}
	active, _ := s.OpsByMachine(ctx, "m1", true, 10)
	if len(active) != 1 || active[0].ID != b.ID {
		t.Fatalf("active %+v", active)
	}
	all, _ := s.OpsByMachine(ctx, "m1", false, 10)
	if len(all) != 2 || all[0].ID != b.ID {
		t.Fatalf("all %+v", all)
	}
	open, _ := s.OpenOps(ctx)
	if len(open) != 2 {
		t.Fatalf("open %+v", open)
	}
	by, _ := s.ActiveOpsByMachines(ctx, []string{"m2"})
	if len(by) != 1 || by[0].Kind != OpDrain {
		t.Fatalf("by machine %+v", by)
	}
	if _, err := s.OpByID(ctx, "nope"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestMachineMetrics(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	base := BucketMs(1_700_000_000_000)
	step := MetricStep.Milliseconds()
	for _, smp := range []struct {
		at       int64
		cpu, ram float64
		disk     float64
		chats    int
	}{{base + 1000, 10, 40, 50, 1}, {base + 61000, 30, 60, 50, 3}, {base + step + 1000, 5, 20, 51, 0}} {
		if err := s.AddMetricSample(ctx, "m1", smp.at, smp.cpu, smp.ram, smp.disk, smp.chats); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.MetricsRange(ctx, "m1", base, base+2*step)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got[0].Samples != 2 || got[0].CPUAvg != 20 || got[0].CPUMax != 30 || got[0].RAMAvg != 50 || got[0].ChatsMax != 3 || got[0].Bucket != base {
		t.Fatalf("first bucket %+v", got[0])
	}
	last, err := s.LastMetric(ctx, "m1")
	if err != nil || last.Bucket != base+step {
		t.Fatalf("last %+v err %v", last, err)
	}
	// Throttled pruning: two buckets older than the cutoff go in batches of one.
	n, err := s.PruneMetrics(ctx, base+2*step, 1)
	if err != nil || n != 1 {
		t.Fatalf("prune %d %v", n, err)
	}
	n, _ = s.PruneMetrics(ctx, base+2*step, 1)
	if n != 1 {
		t.Fatalf("second prune %d", n)
	}
	n, _ = s.PruneMetrics(ctx, base+2*step, 1)
	if n != 0 {
		t.Fatalf("third prune %d", n)
	}
	if _, err := s.LastMetric(ctx, "m1"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestMachineManaged(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	at := time.Now().UTC()
	if err := s.SetManaged(ctx, "m1", CapResources, true, "u1", at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetManaged(ctx, "m1", CapResources, false, "u2", at); err != nil {
		t.Fatal(err)
	}
	got, err := s.ManagedByMachine(ctx, "m1")
	if err != nil || len(got) != 1 || got[CapResources].Managed || got[CapResources].UpdatedBy != "u2" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestMachineWeeks(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	seedMachine(t, db, "m2")
	cost := int64(2500)
	add := func(key, sess, machine string, ts int64, c *int64) {
		t.Helper()
		if _, err := s.AddCall(ctx, UsageCall{OrgKey: "o", Provider: "claude", MsgKey: key, SessionKey: sess, MachineID: machine, Ts: ts, Model: "m", ListCostMicros: c, ReceivedAt: ts}); err != nil {
			t.Fatal(err)
		}
	}
	add("a", "s1", "m1", 1000, &cost)
	add("b", "s1", "m1", 2000, &cost)
	add("c", "s2", "m1", 3000, nil)
	add("d", "s3", "m1", 99999, &cost) // out of the span
	got, err := s.MachineWeeks(ctx, []string{"m1", "m2"}, 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	w := got["m1"]
	if w.Chats != 2 || w.Calls != 3 || w.Unpriced != 1 || w.ListCostMicros != 5000 {
		t.Fatalf("%+v", w)
	}
	if _, ok := got["m2"]; ok {
		t.Fatal("a machine with no calls is in the map")
	}
}

func TestManagedByMachines(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	seedMachine(t, db, "m1")
	seedMachine(t, db, "m2")
	at := time.Unix(1700000000, 0).UTC()
	if err := s.SetManaged(ctx, "m1", CapResources, true, "u1", at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetManaged(ctx, "m1", CapBudget, true, "u1", at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetManaged(ctx, "m1", CapBudget, false, "u1", at); err != nil {
		t.Fatal(err)
	}
	got, err := s.ManagedByMachines(ctx, []string{"m1", "m2"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["m1"][CapResources].Managed || got["m1"][CapBudget].Managed || got["m1"][CapEnv].Managed {
		t.Errorf("m1 = %+v", got["m1"])
	}
	if len(got["m2"]) != 0 {
		t.Errorf("m2 = %+v, want nothing", got["m2"])
	}
	one, err := s.ManagedByMachine(ctx, "m1")
	if err != nil || !one[CapResources].Managed {
		t.Errorf("one = %+v %v", one, err)
	}
}
