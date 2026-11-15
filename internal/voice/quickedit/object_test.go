package quickedit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeObject struct {
	text, version string
	applied       [][]Edit
	confirmed     []bool
	undone        []string
	refuse        error
}

func (f *fakeObject) Read(context.Context, string) (string, string, error) {
	return f.text, f.version, nil
}

func (f *fakeObject) Apply(_ context.Context, version string, edits []Edit, confirmed bool) (Applied, error) {
	if f.refuse != nil {
		return Applied{}, f.refuse
	}
	if version != f.version {
		return Applied{}, &AppError{Code: "conflict", Message: "changed"}
	}
	for _, e := range edits {
		if strings.Contains(e.Old, "\n") && e.New == "" && !confirmed {
			return Applied{}, &AppError{Code: "needs_yes", Message: "removes"}
		}
	}
	f.applied = append(f.applied, edits)
	f.confirmed = append(f.confirmed, confirmed)
	for _, e := range edits {
		f.text = strings.Replace(f.text, e.Old, e.New, 1)
	}
	f.version = "v2"
	return Applied{Changed: Changed(len(edits)), Version: "v2", Hint: "line", IDs: []string{"a"}}, nil
}

func (f *fakeObject) Undo(_ context.Context, version string) error {
	f.undone = append(f.undone, version)
	return nil
}

func planner(answer string) Planner {
	return func(context.Context, string) (string, error) { return answer, nil }
}

func TestEditObjectAppliesThroughApp(t *testing.T) {
	s := New(Config{Plans: []Planner{planner(`{"said":"Переименовал","edits":[{"old":"Заявка","new":"Заказ"}]}`)}})
	obj := &fakeObject{text: `{"id":"a","text":"Заявка"}`, version: "v1"}
	res, err := s.EditObject(context.Background(), ObjectRequest{App: "x.boards", Object: "b1", Ask: "переименуй"}, obj)
	if err != nil || !res.Changed || res.Said != "Переименовал" || len(res.IDs) != 1 {
		t.Fatalf("res %+v err %v", res, err)
	}
	if obj.text != `{"id":"a","text":"Заказ"}` {
		t.Fatalf("text %q", obj.text)
	}
	if _, err := s.UndoObject(context.Background(), "x.boards", "b1", obj); err != nil || len(obj.undone) != 1 || obj.undone[0] != "v2" {
		t.Fatalf("undo %v %v", err, obj.undone)
	}
	if _, err := s.UndoObject(context.Background(), "x.boards", "b1", obj); err == nil {
		t.Fatal("second undo should have nothing")
	}
}

func TestEditObjectRemovalWaitsForYes(t *testing.T) {
	s := New(Config{Plans: []Planner{planner(`{"said":"Удалил","edits":[{"old":"a\nb","new":""}]}`)}})
	obj := &fakeObject{text: "a\nb\nc", version: "v1"}
	_, err := s.EditObject(context.Background(), ObjectRequest{App: "x", Object: "o", Ask: "удали"}, obj)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeNeedsYes || e.Detail != "Удалил" || len(obj.applied) != 0 {
		t.Fatalf("want needs yes, got %v", err)
	}
	// «да»: the planned edit, not a new plan.
	s.cfg.Plans = []Planner{planner(`{"said":"другое","edits":[]}`)}
	res, err := s.EditObject(context.Background(), ObjectRequest{App: "x", Object: "o", Confirm: true}, obj)
	if err != nil || !res.Changed || obj.text != "\nc" || !obj.confirmed[0] {
		t.Fatalf("confirm %+v %v %q", res, err, obj.text)
	}
	if _, err := s.EditObject(context.Background(), ObjectRequest{App: "x", Object: "o", Confirm: true}, obj); err == nil {
		t.Fatal("nothing should wait for yes now")
	}
}

func TestEditObjectSkipsAnswerThatDoesNotApply(t *testing.T) {
	bad := planner(`{"said":"x","edits":[{"old":"нет такого","new":"y"}]}`)
	good := func(ctx context.Context, p string) (string, error) {
		return `{"said":"ok","edits":[{"old":"c","new":"d"}]}`, nil
	}
	s := New(Config{Plans: []Planner{bad, good}})
	obj := &fakeObject{text: "abc", version: "v1"}
	res, err := s.EditObject(context.Background(), ObjectRequest{App: "x", Object: "o", Ask: "поменяй"}, obj)
	if err != nil || obj.text != "abd" || res.Said != "ok" {
		t.Fatalf("%+v %v %q", res, err, obj.text)
	}
}

func TestEditObjectKeepsAppCode(t *testing.T) {
	s := New(Config{Plans: []Planner{planner(`{"said":"x","edits":[{"old":"a","new":"b"}]}`)}})
	obj := &fakeObject{text: "a", version: "v1", refuse: &AppError{Code: "forbidden", Message: "read only"}}
	_, err := s.EditObject(context.Background(), ObjectRequest{App: "x", Object: "o", Ask: "поменяй"}, obj)
	var e *Error
	if !errors.As(err, &e) || e.Code != "edit_forbidden" || e.Status != 403 {
		t.Fatalf("got %v", err)
	}
}

func TestChangedTakesBoolOrCount(t *testing.T) {
	for in, want := range map[string]Changed{`{"changed":true}`: 1, `{"changed":false}`: 0, `{"changed":3}`: 3} {
		var a Applied
		if err := json.Unmarshal([]byte(in), &a); err != nil || a.Changed != want {
			t.Fatalf("%s: %v %v", in, a.Changed, err)
		}
	}
}
