package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func jobsDo(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, method, path string, body any) hubtest.Resp {
	t.Helper()
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	auth, _ := hubproto.SignJobs(m.Priv, m.MachineID, s.Clock.Now(), host)
	return s.Browser().Do(method, hubproto.JobsPath+path, body, "Authorization", auth)
}

// a machine becomes a builder, a heavy job of another machine goes there, the result files come back; with no
// builder online the same job is answered "local" with the reason; the chosen builder is visible in the status.
func TestBuilderPoolJobFlow(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Render")
	home := owner.Machine(spaceID, nil)
	builder := owner.Machine(spaceID, nil)
	builder.Hello.Features = append(builder.Hello.Features, hubproto.FeatureJobRun)
	builder.Close()
	builder.Connect()

	var mu sync.Mutex
	var started hubproto.JobStart
	state := map[string]hubproto.JobState{}
	builder.Handle(hubproto.OpJobCaps, func(hubtest.Call) (any, error) {
		return hubproto.JobCaps{Caps: []string{"blender", "gpu"}, Slots: 2, Tools: []string{"blender"}}, nil
	})
	builder.Handle(hubproto.OpJobStart, func(c hubtest.Call) (any, error) {
		raw, _ := json.Marshal(c.Params)
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(raw, &started)
		state[started.JobID] = hubproto.JobState{Status: hubproto.HeavyRunning}
		return map[string]any{}, nil
	})
	builder.Handle(hubproto.OpJobPoll, func(hubtest.Call) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		return map[string]any{"jobs": state}, nil
	})

	submit := map[string]any{"need": map[string]any{"kind": "heavy", "caps": []string{"blender"}}, "argv": []string{"blender", "-b"},
		"outputs": []string{"out/*.png"}, "hasInput": true}

	// before the role: no builders, the job is the submitter's own, with the reason
	var created struct{ Job map[string]any }
	r := jobsDo(t, s, home, "POST", "", submit)
	r.JSON(&created)
	if r.Code != http.StatusOK || created.Job["local"] != true || !strings.Contains(created.Job["reason"].(string), "no builders") {
		t.Fatalf("before any builder: %s", r)
	}
	if r := jobsDo(t, s, home, "POST", "/"+created.Job["id"].(string)+"/finish", map[string]any{"exitCode": 0}); r.Code != http.StatusOK {
		t.Fatalf("finish: %s", r)
	}

	// the machine goes into the pool (owners only); labels and slots come from its agent
	member := s.SignIn("member@example.com")
	owner.Join(spaceID, member)
	if r := member.Do("PUT", "/api/machines/"+builder.MachineID+"/builder", map[string]any{"role": "builder"}); r.Code != http.StatusForbidden {
		t.Fatalf("a member made a builder: %s", r)
	}
	if r := owner.Do("PUT", "/api/machines/"+builder.MachineID+"/builder", map[string]any{"role": "builder"}); r.Code != http.StatusOK {
		t.Fatalf("role: %s", r)
	}
	var list struct {
		Builders []struct {
			ID    string
			Caps  []string
			Slots int
			Ready bool
		}
	}
	r = member.Do("GET", "/api/spaces/"+spaceID+"/builders", nil)
	r.JSON(&list)
	if len(list.Builders) != 1 || list.Builders[0].ID != builder.MachineID || list.Builders[0].Slots != 2 || !list.Builders[0].Ready || len(list.Builders[0].Caps) != 2 {
		t.Fatalf("builders: %s", r)
	}
	var mv struct{ Machine struct{ Role string } }
	owner.Do("GET", "/api/machines/"+builder.MachineID, nil).JSON(&mv)
	if mv.Machine.Role != "builder" {
		t.Fatalf("the machine's card does not show its role: %+v", mv)
	}

	// the job goes to the builder once its files are there
	submit["need"] = map[string]any{"kind": "heavy", "caps": []string{"blender"}, "machine": builder.MachineID}
	r = jobsDo(t, s, home, "POST", "", submit)
	r.JSON(&created)
	id, _ := created.Job["id"].(string)
	if r.Code != http.StatusOK || created.Job["local"] == true || created.Job["status"] != "queued" {
		t.Fatalf("submit: %s", r)
	}
	if r := jobsDo(t, s, builder, "GET", "/"+id+"/input", nil); r.Code != http.StatusNotFound {
		t.Fatalf("the builder got the input of a job not placed on it: %s", r)
	}
	if r := jobsDo(t, s, home, "PUT", "/"+id+"/input", []byte("archive")); r.Code != http.StatusOK {
		t.Fatalf("input: %s", r)
	}
	s.Hub.Jobs.Tick(context.Background())
	mu.Lock()
	got := started
	mu.Unlock()
	if got.JobID != id || !got.Input || got.Argv[0] != "blender" {
		t.Fatalf("the builder was not handed the job: %+v", got)
	}
	if r := jobsDo(t, s, builder, "GET", "/"+id+"/input", nil); r.Code != http.StatusOK || string(r.Body) != "archive" {
		t.Fatalf("the builder's input: %s", r)
	}
	// it hands the result files back and says it is done
	if r := jobsDo(t, s, builder, "PUT", "/"+id+"/out?path=out/frame.png", []byte("PNG")); r.Code != http.StatusOK {
		t.Fatalf("out: %s", r)
	}
	if r := jobsDo(t, s, home, "PUT", "/"+id+"/out?path=x", []byte("no")); r.Code == http.StatusOK {
		t.Fatalf("the submitter wrote a result file")
	}
	zero := 0
	mu.Lock()
	state[id] = hubproto.JobState{Status: hubproto.HeavyDone, ExitCode: &zero, Log: "Blender quit"}
	mu.Unlock()
	s.Hub.Jobs.Tick(context.Background())

	r = jobsDo(t, s, home, "GET", "/"+id, nil)
	var done struct {
		Job struct {
			Status  string
			Builder struct{ ID, Name string }
			Choice  struct{ Mode, Machine string }
			Outputs []struct {
				Path string
				Size int64
			}
			Log string
		}
	}
	r.JSON(&done)
	if done.Job.Status != "done" || done.Job.Builder.ID != builder.MachineID || done.Job.Choice.Mode != "builder" || len(done.Job.Outputs) != 1 || done.Job.Outputs[0].Path != "out/frame.png" {
		t.Fatalf("status: %s", r)
	}
	if r := jobsDo(t, s, home, "GET", "/"+id+"/out?path=out/frame.png", nil); r.Code != http.StatusOK || string(r.Body) != "PNG" {
		t.Fatalf("result file: %s", r)
	}
	if r := member.Do("GET", "/api/spaces/"+spaceID+"/jobs/"+id+"/out?path=../../x", nil); r.Code != http.StatusNotFound {
		t.Fatalf("path escape: %s", r)
	}
	if r := member.Do("GET", "/api/spaces/"+spaceID+"/jobs/"+id+"/out?path=out/frame.png", nil); r.Code != http.StatusOK || string(r.Body) != "PNG" {
		t.Fatalf("the page takes the file: %s", r)
	}

	// back to agents: the pool is empty again
	if r := owner.Do("PUT", "/api/machines/"+builder.MachineID+"/builder", map[string]any{"role": "agents"}); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	member.Do("GET", "/api/spaces/"+spaceID+"/builders", nil).JSON(&list)
	if len(list.Builders) != 0 {
		t.Fatalf("builders after going back: %+v", list)
	}
}
