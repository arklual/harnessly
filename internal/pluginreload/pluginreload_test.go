package pluginreload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type goldenCase struct {
	Raw      string          `json:"raw"`
	Parsed   map[string]any  `json:"parsed"`
	Applied  map[string]any  `json:"applied"`
	Mark     map[string]any  `json:"mark"`
	Pending  []string        `json:"pending"`
	Tab      json.RawMessage `json:"tab"`
	Decision struct {
		Action string `json:"action"`
		Kind   string `json:"kind"`
	} `json:"decision"`
	Screen *struct {
		Busy  bool    `json:"busy"`
		Menu  bool    `json:"menu"`
		Input *string `json:"input"`
	} `json:"screen"`
	ScreenDecision string `json:"screenDecision"`
	Result         struct {
		Status int `json:"status"`
		Body   struct {
			Error string `json:"error"`
		} `json:"body"`
	} `json:"result"`
	Outcome      string         `json:"outcome"`
	Kind         string         `json:"kind"`
	ApplyOutcome string         `json:"applyOutcome"`
	Next         map[string]any `json:"next"`
	Same         bool           `json:"same"`
}

func toMark(m map[string]any) Mark {
	if m == nil {
		return nil
	}
	out := Mark{}
	for k, v := range m {
		out[k] = v.(float64)
	}
	return out
}

func asJSON(m Mark) map[string]any {
	if m == nil {
		return nil
	}
	raw, _ := m.MarshalJSON()
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// The decisions against the previous runtime's own (testdata/gen.mjs; GOLDEN_FILE names a larger one-off run).
func TestDecisionsMatchThePreviousRuntime(t *testing.T) {
	file := os.Getenv("GOLDEN_FILE")
	if file == "" {
		file = filepath.Join("testdata", "ts-golden.json")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		parsed, ok := ParseMark([]byte(c.Raw))
		if ok != (c.Parsed != nil) || ok && !reflect.DeepEqual(asJSON(parsed), c.Parsed) {
			t.Errorf("#%d parse %q: %v %v, want %v", i, c.Raw, parsed, ok, c.Parsed)
		}
		applied, mark := toMark(c.Applied), toMark(c.Mark)
		if got := Pending(mark, applied); !(len(got) == 0 && len(c.Pending) == 0) && !reflect.DeepEqual(got, c.Pending) {
			t.Errorf("#%d pending %v, want %v", i, got, c.Pending)
		}
		var tab struct {
			PanelType *string `json:"panelType"`
			Provider  *string `json:"provider"`
			CliState  string  `json:"cliState"`
			Queued    bool    `json:"queued"`
		}
		_ = json.Unmarshal(c.Tab, &tab)
		deref := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		if got := Decide(Tab{PanelType: deref(tab.PanelType), Provider: deref(tab.Provider), CliState: tab.CliState, Queued: tab.Queued}, mark, applied); got.Action != c.Decision.Action || got.Kind != c.Decision.Kind {
			t.Errorf("#%d decision %+v, want %+v (%s)", i, got, c.Decision, c.Tab)
		}
		var screen *Screen
		if c.Screen != nil {
			screen = &Screen{Busy: c.Screen.Busy, Menu: c.Screen.Menu, Input: c.Screen.Input}
		}
		if got := ScreenDecision(screen); got != c.ScreenDecision {
			t.Errorf("#%d screen %q, want %q", i, got, c.ScreenDecision)
		}
		if got := Outcome(c.Result.Status, c.Result.Body.Error); got != c.Outcome {
			t.Errorf("#%d outcome %q, want %q", i, got, c.Outcome)
		}
		next := Apply(applied, mark, c.Kind, c.ApplyOutcome)
		same := reflect.ValueOf(next).Pointer() == reflect.ValueOf(applied).Pointer()
		if same != c.Same || !reflect.DeepEqual(asJSON(next), c.Next) {
			t.Errorf("#%d apply %v (same %v), want %v (same %v)", i, next, same, c.Next, c.Same)
		}
	}
}

// The applied marks survive a restart in their file.
func TestAppliedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "d", "applied.json")
	a := &Applied{File: file}
	a.Set("tab-b", Mark{"skills": 2})
	a.Set("tab-a", Mark{"plugins": 1792400400000, "skills": 3})
	a.Delete("tab-x")
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if string(raw) != `{"tab-b":{"skills":2},"tab-a":{"plugins":1792400400000,"skills":3}}`+"\n" {
		t.Fatalf("%s", raw)
	}
	again := &Applied{File: file}
	if !reflect.DeepEqual(again.Tabs(), []string{"tab-b", "tab-a"}) || again.Get("tab-a")["plugins"] != 1792400400000 {
		t.Fatalf("%v", again.Tabs())
	}
	again.Delete("tab-b")
	if !reflect.DeepEqual(again.Tabs(), []string{"tab-a"}) {
		t.Fatal(again.Tabs())
	}
	_ = os.WriteFile(file, []byte("not json"), 0o600)
	if broken := (&Applied{File: file}); len(broken.Tabs()) != 0 {
		t.Fatal("an unreadable file starts over")
	}
}
