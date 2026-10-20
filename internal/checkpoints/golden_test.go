package checkpoints

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/checkpoints/scripttest"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The previous runtime's checkpoint store ran the script in testdata/golden.json (cmd/runtime/testdata/timeline/gen.mjs)
// and recorded each step's result, the shadow repository and the working folder after it. The same script here must
// give the same: the same trees, refs, subjects, rewind log bytes, and working folder.

type scriptLimits struct {
	MaxFileBytes   int64 `json:"maxFileBytes"`
	MaxFiles       int   `json:"maxFiles"`
	MaxCheckpoints int   `json:"maxCheckpoints"`
	MinIntervalMs  int64 `json:"minIntervalMs"`
	MaxDiffBytes   int   `json:"maxDiffBytes"`
	GitTimeoutMs   int64 `json:"gitTimeoutMs"`
}

type step struct {
	Op            string                         `json:"op"`
	Now           *int64                         `json:"now"`
	Scope         string                         `json:"scope"`
	Name          string                         `json:"name"`
	Tab           string                         `json:"tab"`
	Cwd           string                         `json:"cwd"`
	Limits        string                         `json:"limits"`
	ID            string                         `json:"id"`
	Busy          bool                           `json:"busy"`
	Actions       []scripttest.Action            `json:"actions"`
	BeforeApply   []scripttest.Action            `json:"beforeApply"`
	BeforeReplace map[string][]scripttest.Action `json:"beforeReplace"`
	Count         int                            `json:"count"`
	At            int64                          `json:"at"`
	File          string                         `json:"file"`
	Text          string                         `json:"text"`
}

type script struct {
	Roots  []string                `json:"roots"`
	Limits map[string]scriptLimits `json:"limits"`
	Steps  []step                  `json:"steps"`
}

func checkpointJSON(c Checkpoint) map[string]any {
	return map[string]any{"id": c.ID, "kind": c.Kind, "at": c.At, "tree": c.Tree, "files": c.Files}
}

func asJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

// runScript runs a script the way gen.mjs ran it and returns each step's result as decoded JSON.
func runScript(t testing.TB, sc script, ns string) []any {
	w := scripttest.New(t, sc.Roots)
	var now int64
	scopes := map[string]*Scope{}
	store := func(name string) *Store {
		if name == "" {
			name = "default"
		}
		l := sc.Limits[name]
		return &Store{Dir: filepath.Join(w.Root, "data", "checkpoints"), Home: filepath.Join(w.Root, "home"), Namespace: ns, TmpTag: "hs",
			Limits: &Limits{MaxFileBytes: l.MaxFileBytes, MaxFiles: l.MaxFiles, MaxCheckpoints: l.MaxCheckpoints, MinIntervalMs: l.MinIntervalMs,
				MaxDiffBytes: l.MaxDiffBytes, GitTimeout: time.Duration(l.GitTimeoutMs) * time.Millisecond},
			Now: func() int64 { return now }}
	}
	results := make([]any, 0, len(sc.Steps))
	for _, st := range sc.Steps {
		if st.Now != nil {
			now = *st.Now
		}
		s := scopes[st.Scope]
		var result any
		switch st.Op {
		case "fs":
			w.Apply(st.Actions)
		case "scope":
			scope, err := store(st.Limits).Scope(st.Tab, filepath.Join(w.Root, st.Cwd))
			if err != nil {
				result = map[string]any{"ok": false}
				break
			}
			if st.Name != "" {
				scopes[st.Name] = scope
			}
			result = map[string]any{"ok": true, "dir": w.Scrub(scope.Dir), "root": w.Scrub(scope.Root)}
		case "take":
			c, reason := s.Take(*st.Now)
			if reason != "" {
				result = map[string]any{"ok": false, "reason": reason}
			} else {
				result = map[string]any{"ok": true, "checkpoint": checkpointJSON(c)}
			}
		case "list":
			list, err := s.List()
			if err != nil {
				t.Fatal(err)
			}
			views := []any{}
			for _, c := range list {
				views = append(views, checkpointJSON(c))
			}
			result = views
		case "rewinds":
			result = s.Rewinds()
		case "diff":
			d, err := s.DiffTo(st.ID)
			switch {
			case err != nil:
				result = map[string]any{"error": true}
			case d != nil:
				result = d.JSON()
			}
		case "rewind":
			done := map[string]bool{}
			r, err := s.Rewind(st.ID, Hooks{
				IsBusy:      func() bool { return st.Busy },
				BeforeApply: func() { w.Apply(st.BeforeApply) },
				BeforeReplace: func(rel string) {
					if acts, ok := st.BeforeReplace[rel]; ok && !done[rel] {
						done[rel] = true
						w.Apply(acts)
					}
				},
			})
			switch {
			case err != nil:
				result = map[string]any{"error": true}
			case r.Reason != "":
				result = map[string]any{"ok": false, "reason": r.Reason}
			default:
				o := r.JSON()
				o.Set("before", jsvalue.NewObject("id", r.Before.ID, "kind", r.Before.Kind, "at", r.Before.At, "tree", r.Before.Tree, "files", r.Before.Files))
				res := jsvalue.NewObject("ok", true)
				for _, k := range o.Keys() {
					v, _ := o.Get(k)
					res.Set(k, v)
				}
				result = asJSON(res)
			}
		case "seedSaved":
			for i := 0; i < st.Count; i++ {
				sha := strings.TrimSpace(w.Git(w.Root, []byte(fmt.Sprintf("seed %d\n", i)), []string{"hash-object", "-w", "--stdin"}, "GIT_DIR="+s.Dir))
				w.Git(w.Root, nil, []string{"update-ref", fmt.Sprintf("refs/%s/saved/%d-%s", ns, st.At+int64(i), sha[:8]), sha}, "GIT_DIR="+s.Dir)
			}
		case "seedFile":
			if err := os.WriteFile(filepath.Join(s.Dir, st.File), []byte(st.Text), 0o666); err != nil {
				t.Fatal(err)
			}
		case "dump":
			entries, _ := os.ReadDir(filepath.Join(w.Root, "data", "checkpoints"))
			repos := []any{}
			for _, e := range entries {
				repos = append(repos, w.Scrub(e.Name()))
			}
			result = map[string]any{"repo": w.DumpRepo(s.Dir, ns), "tree": w.DumpTree(s.Root), "repos": repos}
		default:
			t.Fatalf("unknown op %q", st.Op)
		}
		if result == nil {
			results = append(results, nil)
		} else {
			results = append(results, asJSON(result))
		}
	}
	return results
}

func TestGoldenStore(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Script  script            `json:"script"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	got := runScript(t, golden.Script, "acme")
	for i, want := range golden.Results {
		var w any
		_ = json.Unmarshal(want, &w)
		if !reflect.DeepEqual(w, got[i]) {
			gb, _ := json.Marshal(got[i])
			t.Errorf("step %d (%s %s):\nwant %s\ngot  %s", i, golden.Script.Steps[i].Op, golden.Script.Steps[i].ID, want, gb)
		}
	}
}
