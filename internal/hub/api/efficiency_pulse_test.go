package api_test

import (
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// The pulse and the summary are dark until the flag is on; a pulse is read only by its author.
func TestEfficiencyPulseIsPrivateAndFlagged(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	root := s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	vera := s.SignIn("vera@example.com")
	space, _ := anna.NewSpace("Example")
	anna.Join(space, vera)
	var made struct{ Org struct{ ID string } }
	r := anna.Do("POST", "/api/orgs", map[string]any{"name": "Example", "spaceIds": []string{space}})
	if r.Code != http.StatusCreated {
		t.Fatalf("org: %s", r)
	}
	r.JSON(&made)
	base := "/api/orgs/" + made.Org.ID + "/efficiency"

	if r := anna.Do("GET", base+"/pulse", nil); r.Code != http.StatusNotFound {
		t.Fatalf("flag off must hide the pulse: %s", r)
	}
	if r := anna.Do("GET", base+"/summary", nil); r.Code != http.StatusNotFound {
		t.Fatalf("flag off must hide the summary: %s", r)
	}
	if r := root.Do("PUT", "/api/orgs/"+made.Org.ID+"/features/efficiency", map[string]any{"enabled": true}); r.Code != http.StatusOK {
		t.Fatalf("flag: %s", r)
	}

	var p struct {
		Week     string
		Answered bool
		Answers  []struct {
			Week                     string
			Speed, Quality, Friction int
		}
	}
	anna.Do("GET", base+"/pulse", nil).JSON(&p)
	if p.Week == "" || p.Answered || len(p.Answers) != 0 {
		t.Fatalf("empty pulse: %+v", p)
	}
	if r := anna.Do("POST", base+"/pulse", map[string]any{"speed": 6, "quality": 3, "friction": 2}); r.Code != http.StatusUnprocessableEntity && r.Code != http.StatusBadRequest {
		t.Fatalf("6 is out of the scale: %s", r)
	}
	if r := anna.Do("POST", base+"/pulse", map[string]any{"speed": 4, "quality": 3, "friction": 2}); r.Code != http.StatusOK {
		t.Fatalf("answer: %s", r)
	}
	anna.Do("POST", base+"/pulse", map[string]any{"speed": 5, "quality": 3, "friction": 2}).JSON(&p)
	if !p.Answered || len(p.Answers) != 1 || p.Answers[0].Speed != 5 || p.Answers[0].Week != p.Week {
		t.Fatalf("a second answer replaces the first: %+v", p)
	}
	vera.Do("GET", base+"/pulse", nil).JSON(&p)
	if p.Answered || len(p.Answers) != 0 {
		t.Fatalf("somebody else's answers leaked: %+v", p)
	}

	var sum struct {
		Metrics []struct{ ID, Verdict string }
		Totals  struct{ CostUsd *float64 }
	}
	if r := anna.Do("GET", base+"/summary", nil); r.Code != http.StatusOK {
		t.Fatalf("summary: %s", r)
	} else {
		r.JSON(&sum)
	}
	if len(sum.Metrics) != 3 || sum.Metrics[0].Verdict != "nodata" {
		t.Fatalf("summary without facts: %+v", sum)
	}
}
