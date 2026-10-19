package limits

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"harnessly/internal/nodejson"
)

// testdata/ts-golden.json holds the previous runtime's answers on generated inputs: its own functions run under node.
// A TS_GOLDEN environment variable points the test at a larger set made the same way.

type golden struct {
	Merge []struct {
		Prev   Data `json:"prev"`
		Next   Data `json:"next"`
		Result any  `json:"result"`
	} `json:"merge"`
	Codexbar []struct {
		Stdout string         `json:"stdout"`
		Now    float64        `json:"now"`
		Result map[string]any `json:"result"`
	} `json:"codexbar"`
	CodexUsage []struct {
		Body   any     `json:"body"`
		Now    float64 `json:"now"`
		Result any     `json:"result"`
	} `json:"codexUsage"`
	CodexJsonl []struct {
		Lines  []string `json:"lines"`
		Now    float64  `json:"now"`
		Result any      `json:"result"`
	} `json:"codexJsonl"`
	History []struct {
		Data    Data            `json:"data"`
		Windows json.RawMessage `json:"windows"`
		Prev    *struct {
			T float64         `json:"t"`
			W json.RawMessage `json:"w"`
		} `json:"prev"`
		Now    float64 `json:"now"`
		Record bool    `json:"record"`
	} `json:"history"`
	Sanitize []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"sanitize"`
}

func loadGolden(t *testing.T) golden {
	path := os.Getenv("TS_GOLDEN")
	if path == "" {
		path = "testdata/ts-golden.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// asData is a value as JSON data: what JSON.stringify would have written, read back.
func asData(v any) any {
	var out any
	_ = json.Unmarshal(nodejson.Marshal(v), &out)
	return out
}

func same(t *testing.T, what string, i int, got, want any) {
	t.Helper()
	if g, w := asData(got), asData(want); !reflect.DeepEqual(g, w) {
		t.Errorf("%s #%d:\n got  %s\n want %s", what, i, nodejson.Marshal(g), nodejson.Marshal(w))
	}
}

func TestMatchesTheOldRuntime(t *testing.T) {
	g := loadGolden(t)
	for i, c := range g.Merge {
		if m, ok := c.Result.(map[string]any); ok && m["throws"] == true {
			continue // an extra item without a window threw a TypeError there; Go compares it as unmeasured
		}
		same(t, "merge", i, Merge(c.Prev, c.Next), c.Result)
	}
	for i, c := range g.Codexbar {
		data, failure := ParseCodexbar([]byte(c.Stdout), c.Now)
		got := map[string]any{"ok": true, "data": data}
		if data == nil {
			got = map[string]any{"ok": false, "message": failure}
		}
		same(t, "codexbar", i, got, c.Result)
	}
	for i, c := range g.CodexUsage {
		got := ParseCodexUsage(c.Body, c.Now)
		if got == nil {
			same(t, "codex usage", i, nil, c.Result)
		} else {
			same(t, "codex usage", i, got, c.Result)
		}
	}
	for i, c := range g.CodexJsonl {
		got := latestCodexLimits(c.Lines, c.Now)
		if got == nil {
			same(t, "codex transcript", i, nil, c.Result)
		} else {
			same(t, "codex transcript", i, got, c.Result)
		}
	}
	for i, c := range g.History {
		w := HistoryWindows(c.Data)
		if want := orderedWindows(t, c.Windows); !reflect.DeepEqual(w.IDs, want.IDs) || !reflect.DeepEqual(asData(w.Tuples), asData(want.Tuples)) {
			t.Errorf("history windows #%d: got %v %v want %v %v", i, w.IDs, w.Tuples, want.IDs, want.Tuples)
		}
		var prev *Windows
		var prevT float64
		if c.Prev != nil {
			p := orderedWindows(t, c.Prev.W)
			prev, prevT = &p, c.Prev.T
		}
		if got := ShouldRecord(prevT, prev, w, c.Now); got != c.Record {
			t.Errorf("should record #%d: got %v want %v", i, got, c.Record)
		}
	}
	for _, c := range g.Sanitize {
		if got := Sanitize(c.In); got != c.Out {
			t.Errorf("sanitize %q: got %q want %q", c.In, got, c.Out)
		}
	}
}

// orderedWindows reads a JavaScript object of tuples keeping its key order.
func orderedWindows(t *testing.T, raw json.RawMessage) Windows {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var w Windows
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		k, _ := dec.Token()
		var tuple [3]any
		if err := dec.Decode(&tuple); err != nil {
			t.Fatal(err)
		}
		var win Window
		for j, x := range tuple {
			win[j] = num(x)
		}
		w.add(k.(string), win)
	}
	return w
}

func TestHistoryLineIsWhatTheOldRuntimeWrote(t *testing.T) {
	w := HistoryWindows(Data{"ts": 100.0,
		"five_hour": map[string]any{"used_percentage": 46.0, "resets_at": 1000.0, "at": 100.0},
		"seven_day": map[string]any{"used_percentage": 81.5, "resets_at": 5000.0},
		"extra":     []any{map[string]any{"id": "claude-weekly-scoped-fable", "title": "x", "window": map[string]any{"used_percentage": 0.0, "resets_at": 5000.0, "at": 100.0}}},
	})
	want := `{"t":1790365540,"p":"claude","w":{"five_hour":[46,1000,100],"seven_day":[81.5,5000,100],"claude-weekly-scoped-fable":[0,5000,100]}}`
	if got := formatLine(1790365540, "claude", w); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if strings.Contains(want, " ") {
		t.Fatal("JSON.stringify writes no spaces")
	}
}
