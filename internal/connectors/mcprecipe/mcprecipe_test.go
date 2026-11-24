package mcprecipe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

type fakeMCP struct {
	jobs  []map[string]any
	specs []mcpconn.ToolSpec
	args  []string
}

func (f *fakeMCP) List() ([]mcpconn.Server, error) {
	return []mcpconn.Server{{ID: "s1", Slug: "gen", Name: "Gen"}}, nil
}
func (f *fakeMCP) ToolSpecs(context.Context, string) ([]mcpconn.ToolSpec, error) { return f.specs, nil }
func (f *fakeMCP) CallTool(_ context.Context, _, _ string, args json.RawMessage) (json.RawMessage, error) {
	f.args = append(f.args, string(args))
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"jobs": f.jobs}})
	return json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": string(body)}}})
}

func yes() *bool { b := true; return &b }

func instance() (connectors.Instance, map[string]json.RawMessage) {
	memo := map[string]json.RawMessage{}
	return connectors.Instance{ID: "c1", Connector: ID,
		Config: map[string]string{"server": "gen", "recipe": `{"trigger":"generation.done","tool":"list_jobs","args":{"since":"{{cursor}}","limit":20},
			"items":"data.jobs","id":"id","at":"done_at","title":"prompt","url":"link","cursor":"done_at"}`},
		Load: func(v any) error {
			b, ok := memo["m"]
			if !ok {
				return nil
			}
			return json.Unmarshal(b, v)
		},
		Store: func(v any) error { b, err := json.Marshal(v); memo["m"] = b; return err },
	}, memo
}

func TestRecipePollsNewThingsOnly(t *testing.T) {
	f := &fakeMCP{specs: []mcpconn.ToolSpec{{Name: "list_jobs", Annotations: &mcpconn.ToolAnnotations{ReadOnlyHint: yes()}}},
		jobs: []map[string]any{{"id": 1, "done_at": "2026-11-21T09:00:00Z", "prompt": "a cat", "link": "https://gen.example.com/1"}}}
	Service = f
	defer func() { Service = nil }()
	in, _ := instance()
	ctx := context.Background()
	c := conn{}
	acc, err := c.Check(ctx, in)
	if err != nil || !strings.Contains(acc.Label, "1 now") || !strings.Contains(acc.Label, "a cat") {
		t.Fatalf("dry run: %+v %v", acc, err)
	}
	// The first poll learns what is there: no events.
	evs, _, err := c.Poll(ctx, in, "")
	if err != nil || len(evs) != 0 {
		t.Fatalf("first poll: %v %v", evs, err)
	}
	f.jobs = append(f.jobs, map[string]any{"id": 2, "done_at": "2026-11-21T10:00:00Z", "prompt": "a dog", "link": "https://gen.example.com/2"})
	evs, _, err = c.Poll(ctx, in, "")
	if err != nil || len(evs) != 1 || evs[0].ID != "generation.done:2" || evs[0].Title != "a dog" || evs[0].Facts["trigger"] != "generation.done" || evs[0].At.Hour() != 10 {
		t.Fatalf("second poll: %+v %v", evs, err)
	}
	if !strings.Contains(f.args[len(f.args)-1], `"since":"2026-11-21T09:00:00Z"`) {
		t.Fatalf("the cursor was not passed: %s", f.args[len(f.args)-1])
	}
	// Nothing new: nothing.
	if evs, _, _ := c.Poll(ctx, in, ""); len(evs) != 0 {
		t.Fatalf("again: %v", evs)
	}
}

func TestRecipeRefusesToolsThatMayWrite(t *testing.T) {
	f := &fakeMCP{specs: []mcpconn.ToolSpec{{Name: "list_jobs"}}}
	Service = f
	defer func() { Service = nil }()
	in, _ := instance()
	ctx := context.Background()
	if _, err := (conn{}).Check(ctx, in); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("unmarked: %v", err)
	}
	in.Config["unmarked"] = "yes"
	if _, err := (conn{}).Check(ctx, in); err != nil {
		t.Fatalf("allowed unmarked: %v", err)
	}
	f.specs[0].Annotations = &mcpconn.ToolAnnotations{DestructiveHint: yes()}
	if _, err := (conn{}).Check(ctx, in); err == nil || !strings.Contains(err.Error(), "destructive") {
		t.Fatalf("destructive: %v", err)
	}
	if len(f.args) != 1 {
		t.Fatalf("the tool was called %d times", len(f.args))
	}
}

func TestPickAndWhen(t *testing.T) {
	v := map[string]any{"a": map[string]any{"b": []any{map[string]any{"c": "x"}}}}
	if pick(v, "a.b.0.c") != "x" || pick(v, "a.z") != nil || pick(v, "") == nil {
		t.Fatal("pick")
	}
	if when(1795249200.0).IsZero() || when(1795249200000.0).Year() != 2026 || when("2026-11-21T09:00:00Z").Hour() != 9 {
		t.Fatal("when")
	}
	if !later("10", "9") || later("2026-11-20", "2026-11-21") {
		t.Fatal("later")
	}
}
