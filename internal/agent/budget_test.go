package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/budget"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func budgetParams(version int64, mode, user string) map[string]any {
	return map[string]any{"version": version, "mode": mode, "issuedAt": 1000, "entries": []hubproto.BudgetEntry{
		{User: user, Status: hubproto.BudgetStop, Pct: 100, ResetsAt: 2000, Reason: "limit", RuleID: "r1"},
	}}
}

func TestHubBudgetSetKeepsTheNewestAndSurvivesARestart(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	file := budget.Path(filepath.Join(r.rig.Home, ".harnessly"))

	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if hello.BudgetVersion != 0 || !contains(hello.Features, hubproto.FeatureBudget) {
		t.Fatalf("%+v", hello)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("a file before any list: %v", err)
	}

	res := decode[hubproto.BudgetSetResult](t, c.mustCall(hubproto.OpBudgetSet, budgetParams(5, "stop", "a@x.ru")))
	if res.Version != 5 {
		t.Fatalf("%+v", res)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, fi)
	}
	if _, err := os.Stat(file + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the temporary file stayed: %v", err)
	}
	set, err := budget.Load(file)
	if err != nil || set.Version != 5 || set.Mode != "stop" || len(set.Entries) != 1 || set.Entries[0].User != "a@x.ru" || set.Entries[0].RuleID != "r1" {
		t.Fatalf("%+v %v", set, err)
	}

	// An older and an equal version change nothing and answer the held one.
	for _, v := range []int64{4, 5} {
		res = decode[hubproto.BudgetSetResult](t, c.mustCall(hubproto.OpBudgetSet, budgetParams(v, "off", "b@x.ru")))
		if res.Version != 5 {
			t.Fatalf("version %d: %+v", v, res)
		}
	}
	if set, _ := budget.Load(file); set.Mode != "stop" || set.Entries[0].User != "a@x.ru" {
		t.Fatalf("an old list replaced the held one: %+v", set)
	}

	// A newer one replaces the whole list; the hello tells the hub which version the machine holds.
	res = decode[hubproto.BudgetSetResult](t, c.mustCall(hubproto.OpBudgetSet, budgetParams(6, "warn", "b@x.ru")))
	if res.Version != 6 {
		t.Fatalf("%+v", res)
	}
	if set, _ := budget.Load(file); set.Version != 6 || set.Mode != "warn" || set.Entries[0].User != "b@x.ru" {
		t.Fatalf("%+v", set)
	}
	if hello = decode[hubproto.Hello](t, c.mustCall("hello", nil)); hello.BudgetVersion != 6 {
		t.Fatalf("%+v", hello)
	}
	if a := r.audit(); !strings.Contains(a, `"op":"budget.set"`) {
		t.Errorf("audit %s", a)
	}

	// A restart keeps the list: the agent reads the version back from the file.
	k := newBudgetKeeper(filepath.Join(r.rig.Home, ".harnessly"))
	if k.Version() != 6 {
		t.Fatalf("version after a restart: %d", k.Version())
	}
}

func TestHubBudgetSetRefusesBadLists(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	for name, p := range map[string]map[string]any{
		"no version": {"mode": "stop", "entries": []any{}},
		"zero":       budgetParams(0, "stop", "a@x.ru"),
		"bad mode":   budgetParams(3, "kill", "a@x.ru"),
		"no mode":    {"version": 3, "entries": []any{}},
	} {
		if _, errText := c.call(hubproto.OpBudgetSet, p); errText == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := os.Stat(budget.Path(filepath.Join(r.rig.Home, ".harnessly"))); !os.IsNotExist(err) {
		t.Errorf("a refused list was written: %v", err)
	}
	if hello := decode[hubproto.Hello](t, c.mustCall("hello", nil)); hello.BudgetVersion != 0 {
		t.Errorf("%+v", hello)
	}
}

func TestHubBudgetIsKeptWhileUnlinked(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	c.mustCall(hubproto.OpBudgetSet, budgetParams(9, "stop", "a@x.ru"))
	_ = c.conn.CloseNow()
	r.waitLinkState(func(s LinkState) bool { return !s.Linked })
	set, err := budget.Load(budget.Path(filepath.Join(r.rig.Home, ".harnessly")))
	if err != nil || set.Version != 9 {
		t.Fatalf("%+v %v", set, err)
	}
	// The agent redials and says the version it still holds.
	c = r.connect()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if hello := decode[hubproto.Hello](t, c.mustCall("hello", nil)); hello.BudgetVersion == 9 {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("%+v", hello)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
