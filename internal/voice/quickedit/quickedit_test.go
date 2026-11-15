package quickedit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func setup(t *testing.T, run Runner) (*Service, string) {
	root := t.TempDir()
	file := filepath.Join(root, "Главная.dc.html")
	if err := os.WriteFile(file, []byte(`<h1 style="font-size: 24px">Привет</h1>`), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(Config{Roots: []string{root}, Run: run}), file
}

func TestEditChangesTheFileAndUndoPutsItBack(t *testing.T) {
	var prompt string
	s, file := setup(t, func(_ context.Context, target, p string) (string, error) {
		prompt = p
		raw, _ := os.ReadFile(target)
		return "думаю…\nЗаголовок стал крупнее", os.WriteFile(target, []byte(strings.Replace(string(raw), "24px", "40px", 1)), 0o644)
	})
	res, err := s.Edit(context.Background(), Request{File: file, Ask: "сделай заголовок крупнее", Selection: "<h1>Привет</h1>"})
	if err != nil || !res.Changed || res.Said != "Заголовок стал крупнее" {
		t.Fatalf("edit %+v %v", res, err)
	}
	if !strings.Contains(prompt, "сделай заголовок крупнее") || !strings.Contains(prompt, "<h1>Привет</h1>") ||
		!strings.Contains(prompt, "font-size: 24px") {
		t.Fatalf("prompt %s", prompt)
	}
	if _, err := s.Undo(file); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(file); !strings.Contains(string(raw), "24px") {
		t.Fatalf("undo left %s", raw)
	}
	if _, err := s.Undo(file); err == nil {
		t.Fatal("a second undo")
	}
}

func TestOutsideTakenAndEmpty(t *testing.T) {
	s, file := setup(t, func(context.Context, string, string) (string, error) { return "", nil })
	var e *Error
	if _, err := s.Edit(context.Background(), Request{File: "/etc/hostname", Ask: "x"}); !errors.As(err, &e) || e.Code != CodeOutside {
		t.Fatalf("outside %v", err)
	}
	dot := filepath.Join(filepath.Dir(file), ".secrets")
	_ = os.WriteFile(dot, []byte("k"), 0o600)
	if _, err := s.Edit(context.Background(), Request{File: dot, Ask: "x"}); !errors.As(err, &e) || e.Code != CodeOutside {
		t.Fatalf("hidden %v", err)
	}
	s.cfg.Holder = func(string, string) string { return "чат «Другой»" }
	if _, err := s.Edit(context.Background(), Request{File: file, Ask: "x"}); !errors.As(err, &e) || e.Code != CodeTaken {
		t.Fatalf("taken %v", err)
	}
	if _, err := s.Edit(context.Background(), Request{File: file, Ask: "  "}); !errors.As(err, &e) || e.Code != CodeBadRequest {
		t.Fatalf("empty %v", err)
	}
}

func TestPlannedEditIsAppliedHere(t *testing.T) {
	s, file := setup(t, nil)
	s.cfg.Plans = []Planner{func(_ context.Context, p string) (string, error) {
		if !strings.Contains(p, "font-size: 24px") {
			t.Fatalf("prompt %s", p)
		}
		return "```json\n{\"said\":\"Крупнее\",\"edits\":[{\"old\":\"font-size: 24px\",\"new\":\"font-size: 40px\"}]}\n```", nil
	}}
	res, err := s.Edit(context.Background(), Request{File: file, Ask: "крупнее"})
	if err != nil || !res.Changed || res.Said != "Крупнее" {
		t.Fatalf("%+v %v", res, err)
	}
	if raw, _ := os.ReadFile(file); !strings.Contains(string(raw), "40px") {
		t.Fatalf("file %s", raw)
	}
	// A piece that is not in the file once hands over to the agent run.
	ran := false
	s.cfg.Plans = []Planner{func(context.Context, string) (string, error) {
		return `{"said":"x","edits":[{"old":"nope","new":"y"}]}`, nil
	}}
	s.cfg.Run = func(context.Context, string, string) (string, error) { ran = true; return "ok", nil }
	if _, err := s.Edit(context.Background(), Request{File: file, Ask: "ещё"}); err != nil || !ran {
		t.Fatalf("fallback %v %v", ran, err)
	}
}

func TestPlansRaceAndTheFirstThatAppliesWins(t *testing.T) {
	s, file := setup(t, nil)
	var mu sync.Mutex
	steps := map[string]bool{}
	done := make(chan struct{})
	s.cfg.Step = func(step string, _ time.Duration, ok bool, _ string) {
		mu.Lock()
		steps[step] = ok
		if len(steps) == 3 {
			close(done)
		}
		mu.Unlock()
	}
	release := make(chan struct{})
	s.cfg.PlanNames = []string{"broken", "slow", "fast"}
	s.cfg.Plans = []Planner{
		func(context.Context, string) (string, error) { return "", errors.New("down") },
		func(context.Context, string) (string, error) {
			<-release // answers only after the edit is done: measured, never written
			return `{"said":"Ещё крупнее","edits":[{"old":"font-size: 24px","new":"font-size: 48px"}]}`, nil
		},
		func(context.Context, string) (string, error) {
			return `{"said":"Крупнее","edits":[{"old":"font-size: 24px","new":"font-size: 30px"}]}`, nil
		},
	}
	res, err := s.Edit(context.Background(), Request{File: file, Ask: "крупнее"})
	if err != nil || !res.Changed || res.Said != "Крупнее" {
		t.Fatalf("%+v %v", res, err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("steps %v", steps)
	}
	mu.Lock()
	defer mu.Unlock()
	if !steps["fast"] || steps["broken"] || !steps["slow lost"] {
		t.Fatalf("steps %v", steps)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "font-size: 30px") {
		t.Fatalf("file %s", b)
	}
}

func TestBigFileIsShownAroundThePick(t *testing.T) {
	big := strings.Repeat("<p>filler line of the board</p>\n", 2000) + "<h1 style=\"color: red\">Ночь в музее</h1>\n" +
		strings.Repeat("<p>tail line of the board</p>\n", 2000)
	p := PlanPrompt([]byte(big), `<h1 style="color: red">Ночь в музее</h1>`, "синим")
	if !strings.Contains(p, "<file-part>") || !strings.Contains(p, "Ночь в музее") || len(p) > 3*windowSide {
		t.Fatalf("prompt of %d bytes", len(p))
	}
	if p := PlanPrompt([]byte(big), "", "синим"); !strings.Contains(p, "<file>") {
		t.Fatal("no pick: whole file")
	}
}

func TestHintIsTheChangedLines(t *testing.T) {
	before := []byte("<main>\n  <h1 style=\"color: #facc15\">Ночь</h1>\n  <p>текст</p>\n</main>\n")
	after := []byte("<main>\n  <h1 style=\"color: #ef4444\">Ночь</h1>\n  <p>текст</p>\n</main>\n")
	if h := hintOf(before, after); h != `<h1 style="color: #ef4444">Ночь</h1>` {
		t.Fatalf("hint %q", h)
	}
	if h := hintOf(before, before); h != "" {
		t.Fatalf("same %q", h)
	}
}

func TestStepsAreHeard(t *testing.T) {
	s, file := setup(t, nil)
	var steps []string
	s.cfg.Step = func(step string, _ time.Duration, ok bool, _ string) {
		steps = append(steps, step+":"+strconv.FormatBool(ok))
	}
	s.cfg.PlanNames = []string{"claude"}
	s.cfg.Plans = []Planner{func(context.Context, string) (string, error) {
		return `{"said":"Крупнее","edits":[{"old":"font-size: 24px","new":"font-size: 32px"}]}`, nil
	}}
	res, err := s.Edit(context.Background(), Request{File: file, Ask: "крупнее"})
	if err != nil || !strings.Contains(res.Hint, "32px") || strings.Join(steps, ",") != "claude:true" {
		t.Fatalf("%+v %v %v", res, err, steps)
	}
}
