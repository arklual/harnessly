package orgs_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// shapePaths lists the paths of every field of a decoded JSON value. A null stands for any shape (the TypeScript type is
// "something or null"), so nothing under a path that is null on either side is compared; an array is described by the
// union of its items, and an empty one says nothing.
func shapePaths(v any, path string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			p := path + "." + k
			out[p] = true
			shapePaths(c, p, out)
		}
	case []any:
		for _, c := range x {
			shapePaths(c, path+"[]", out)
		}
	}
}

// nullPaths are the paths whose value is null, or an empty array.
func nullPaths(v any, path string, out map[string]bool) {
	switch x := v.(type) {
	case nil:
		out[path] = true
	case map[string]any:
		for k, c := range x {
			nullPaths(c, path+"."+k, out)
		}
	case []any:
		if len(x) == 0 {
			out[path+"[]"] = true
		}
		for _, c := range x {
			nullPaths(c, path+"[]", out)
		}
	}
}

func shapeOf(v any) (paths, nulls map[string]bool) {
	paths, nulls = map[string]bool{}, map[string]bool{}
	shapePaths(v, "", paths)
	nullPaths(v, "", nulls)
	return
}

func under(p string, set map[string]bool) bool {
	for n := range set {
		if p == n || strings.HasPrefix(p, n+".") || strings.HasPrefix(p, n+"[]") {
			return true
		}
	}
	return false
}

// sameShape fails the test for a field one side has and the other lacks.
func sameShape(t *testing.T, name string, got, want any) {
	t.Helper()
	gp, gn := shapeOf(got)
	wp, wn := shapeOf(want)
	// params of an operation differ by its kind, and are free-form in the contract (unknown).
	skip := func(p string) bool { return under(p, gn) || under(p, wn) || strings.Contains(p, ".params.") }
	var diff []string
	for p := range gp {
		if !wp[p] && !skip(p) {
			diff = append(diff, "answer has "+p)
		}
	}
	for p := range wp {
		if !gp[p] && !skip(p) {
			diff = append(diff, "fixture has "+p)
		}
	}
	sort.Strings(diff)
	if len(diff) > 0 {
		t.Errorf("%s: %s", name, strings.Join(diff, "; "))
	}
}

func fixture(t *testing.T, rel string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return v
}

// The answers of the resources side have the same fields as the web's fixtures (web/app/hub-api/fixtures/machines and
// analytics/machines-admin.json), which contracts.test.ts checks against the TypeScript types.
func TestResourcesShapesMatchWebFixtures(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t, hubproto.FeatureMetrics, hubproto.FeatureDrain)
	id := a.MachineID
	rc.manage(t, id)

	cpu, ram, disk := 41.26, 62.0, 55.5
	a.Emit(map[string]any{"event": hubproto.EventMetricsSample, "sample": hubproto.MetricsSample{CPUPct: &cpu, RAMPct: &ram, DiskPct: &disk, ChatsNow: intp(2), At: rc.s.Clock.Now().UnixMilli()}})
	rc.s.Eventually("the load", func() bool {
		m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
		return m != nil && m.Load != nil
	})

	get := func(method, path string, body any) any {
		t.Helper()
		r := rc.anna.Do(method, path, body)
		if r.Code >= 300 {
			t.Fatalf("%s %s: %s", method, path, r)
		}
		var v any
		r.JSON(&v)
		return v
	}

	// The operation is kept in its running state while the shapes are read: the provider finishes at its first poll, so
	// ask for the answer of the request itself, then the list.
	resize := get("POST", "/api/machines/"+id+"/resize", map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80})
	sameShape(t, "resize", resize, fixture(t, "machines/resize.json"))
	opID := resize.(map[string]any)["op"].(map[string]any)["id"].(string)
	rc.waitOp(t, id, opID, "done")

	sameShape(t, "resources", get("GET", "/api/machines/"+id+"/resources", nil), fixture(t, "machines/resources.json"))
	sameShape(t, "quote", get("POST", "/api/machines/"+id+"/resources/quote", map[string]any{"cpu": 8, "ramGB": 16, "diskGB": 160}), fixture(t, "machines/quote.json"))
	sameShape(t, "ops", get("GET", "/api/machines/"+id+"/ops", nil), fixture(t, "machines/ops.json"))
	sameShape(t, "managed", get("PUT", "/api/machines/"+id+"/managed", map[string]any{"capability": "access", "managed": true}), fixture(t, "machines/managed.json"))
	sameShape(t, "metrics", get("GET", "/api/machines/"+id+"/metrics?range=1h", nil), fixture(t, "machines/metrics.json"))

	// The row of the machines list, with the feature on, against the row of the fixture that has everything filled.
	list := get("GET", rc.url("/machines"), nil).(map[string]any)
	want := fixture(t, "analytics/machines-admin.json").(map[string]any)
	sameShape(t, "machines list", list, want)
	rows := list["machines"].([]any)
	var row any
	for _, r := range rows {
		if r.(map[string]any)["name"] == "dev-box" {
			row = r
		}
	}
	var full any
	for _, r := range want["machines"].([]any) {
		if r.(map[string]any)["name"] == "runner-a" {
			full = r
		}
	}
	if row == nil || full == nil {
		t.Fatalf("rows: %v / %v", row, full)
	}
	sameShape(t, "machine row", row, full)

	// An operation that waits for chats is the one with chatsNow; a restart answers with jobId and the operation.
	a.Handle(hubproto.OpRuntimeRestart, func(c hubtest.Call) (any, error) {
		return hubproto.RestartEvent{JobID: c.Str("jobId"), State: hubproto.RestartRestarting}, nil
	})
	sameShape(t, "restart", get("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"}), fixture(t, "analytics/restart.json"))
}

// With the feature off the row carries the same keys, empty.
func TestMachineRowKeysWithTheFeatureOff(t *testing.T) {
	rc := newRes(t, true)
	rc.agent(t)
	rc.gate.on.Store(false)
	r := rc.anna.Do("GET", rc.url("/machines"), nil)
	var list struct{ Machines []map[string]any }
	r.JSON(&list)
	if len(list.Machines) != 1 {
		t.Fatalf("list: %s", r)
	}
	m := list.Machines[0]
	for _, k := range []string{"cloud", "hardware", "load", "week", "managed"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Errorf("%s = %v (present %v), want null", k, v, ok)
		}
	}
	if v, ok := m["activeOps"].([]any); !ok || len(v) != 0 {
		t.Errorf("activeOps = %v", m["activeOps"])
	}
}

// The week of a machine: its chats and calls, and the list cost of the priced ones in the organization's currency;
// calls without a price are counted, never costed, and a week with none priced has no spend.
func TestMachineWeekSpend(t *testing.T) {
	rc := newRes(t, true)
	rc.agent(t)
	st := rc.s.DB.S()
	ctx := context.Background()
	now := rc.s.Clock.Now()
	m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
	if m == nil || m.Week != nil {
		t.Fatalf("a machine with no calls has a week: %+v", m)
	}
	add := func(key, session string, ago time.Duration, cost *int64) {
		t.Helper()
		ts := now.Add(-ago).UnixMilli()
		if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: rc.org, Provider: "claude", MsgKey: key, SessionKey: session, MachineID: m.ID, Ts: ts,
			Model: "m", ListCostMicros: cost, ReceivedAt: ts}); err != nil {
			t.Fatal(err)
		}
	}
	two := int64(2_000_000)
	add("a", "s1", time.Hour, nil)
	m = machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
	if m.Week == nil || m.Week.Calls != 1 || m.Week.Unpriced != 1 || m.Week.Spend != nil {
		t.Fatalf("one unpriced call: %+v", m.Week)
	}
	add("b", "s1", 2*time.Hour, &two)
	add("c", "s2", 3*time.Hour, &two)
	add("d", "s3", 10*24*time.Hour, &two) // out of the week
	m = machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
	w := m.Week
	if w == nil || w.Chats != 2 || w.Calls != 3 || w.Unpriced != 1 || w.Spend == nil || w.Spend.USD != 4 {
		t.Fatalf("week = %+v", w)
	}
	rc.gate.on.Store(false)
	if m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box"); m.Week != nil {
		t.Errorf("week with the feature off: %+v", m.Week)
	}
}
