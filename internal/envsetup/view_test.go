package envsetup

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestViewBeforeAndAfterTheFirstRun(t *testing.T) {
	f := newFixture(t)
	if v := f.r.View(); v.Hash != "" || len(v.Items) != 0 || !v.Allowed {
		t.Fatalf("no list: %+v", v)
	}
	f.setList(
		script("s1", "Плагины", 1, `{"from":"text","body":"echo hi"}`),
		script("s2", "Токены", 2, `{"from":"text","body":"echo t","secrets":["gitlab-token"]}`),
	)
	v := f.r.View()
	if v.Done || len(v.Items) != 2 || v.Items[0].Status != hubproto.EnvPending || v.Run != nil {
		t.Fatalf("before a run: %+v", v)
	}
	if v.Items[1].Secret != "gitlab-token" {
		t.Fatalf("the script waits for its secret: %+v", v.Items[1])
	}
	f.r.Tick(context.Background())
	v = f.r.View()
	if v.Run == nil || v.Run.Status == hubproto.EnvRunning {
		t.Fatalf("the run is the latest one, finished: %+v", v.Run)
	}
	if v.Items[0].Status != hubproto.EnvOK || v.Items[1].Status != hubproto.EnvPending || v.Done {
		t.Fatalf("after the run: %+v", v.Items)
	}
	f.safe.put("gitlab-token", "x")
	f.r.Tick(context.Background())
	if v = f.r.View(); !v.Done || v.Items[1].Secret != "" {
		t.Fatalf("the secret came, everything is done: %+v", v)
	}
}

func TestAskQueuesOneApplyRun(t *testing.T) {
	f := newFixture(t)
	if _, err := f.r.Ask(""); err == nil {
		t.Fatal("no list, no run")
	}
	f.setList(script("s1", "Плагины", 1, `{"from":"text","body":"echo hi"}`))
	id, err := f.r.Ask("")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := f.r.Ask("")
	if again != id {
		t.Fatalf("a waiting run is answered with its id: %s, %s", id, again)
	}
	f.r.Tick(context.Background())
	if run := f.runFile(id); run.Mode != hubproto.RunApply || run.Status != hubproto.EnvOK {
		t.Fatalf("the asked run: %+v", run)
	}
	f.allow(false)
	if _, err := f.r.Ask(""); err != ErrSetupOff {
		t.Fatalf("off: %v", err)
	}
}
