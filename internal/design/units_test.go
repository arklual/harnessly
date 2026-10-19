package design

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"harnessly/internal/jsvalue"
)

type unitCase struct {
	Fn     string
	Input  map[string]json.RawMessage
	Output struct {
		OK    json.RawMessage
		Error *string
	}
}

func (c unitCase) str(k string) string {
	var s string
	_ = json.Unmarshal(c.Input[k], &s)
	return s
}

func (c unitCase) num(k string) int {
	var n int
	_ = json.Unmarshal(c.Input[k], &n)
	return n
}

func (c unitCase) parsed(k string) any {
	v, _ := jsvalue.Parse([]byte(c.str(k)))
	return v
}

func nullable[T any](v *T) any {
	if v == nil {
		return nil
	}
	return *v
}

// runUnit is the Go answer to a generated case: a value (encoded as JSON for the comparison) or a refusal.
func runUnit(c unitCase) (any, error) {
	switch c.Fn {
	case "cleanText":
		if s, ok := CleanText(c.str("s"), c.num("max")); ok {
			return s, nil
		}
		return nil, nil
	case "claimFileName":
		return ClaimFileName(c.str("s")), nil
	case "cleanBoardKey":
		return CleanBoardKey(c.str("s"))
	case "parseBoardPath":
		if p := ParseBoardPath(c.str("s")); p != "" {
			return p, nil
		}
		return nil, nil
	case "boardHref":
		return BoardHref(c.str("id"), c.str("path")), nil
	case "parsePagePath":
		if ref, file, ok := ParsePagePath(c.str("s")); ok {
			return map[string]string{"ref": ref, "file": file}, nil
		}
		return nil, nil
	case "isLibraryId":
		return IsLibraryID(c.str("s")), nil
	case "parseArtifactFile":
		m := ParseArtifactFile(c.parsed("json"))
		var boards any
		if m.Boards != nil {
			boards = m.Boards
		}
		return map[string]any{"title": nullable(m.Title), "source": nullable(m.Source), "boards": boards}, nil
	case "parseCanvas":
		boards, title := ParseCanvasJSON(c.parsed("json"))
		out := []map[string]any{}
		for _, b := range boards {
			out = append(out, map[string]any{"file": b.File, "title": b.Title, "x": b.X, "y": b.Y, "w": b.W, "h": b.H})
		}
		return map[string]any{"boards": out, "title": nullable(title)}, nil
	case "innerPageBoard":
		if b := InnerPageBoard(c.str("title"), c.str("source")); b != nil {
			return b, nil
		}
		return nil, nil
	case "parseBoardTweaks":
		if t := ParseBoardTweaks(c.str("source")); t != nil {
			return t, nil
		}
		return nil, nil
	case "lineOfText":
		if n := LineOfText(c.str("source"), c.str("text")); n > 0 {
			return n, nil
		}
		return nil, nil
	case "agentMessage":
		return AgentMessage(&Artifact{Title: c.str("title"), ProjectPath: c.str("path")}, c.str("text")), nil
	case "agentBlockMessage":
		return AgentBlockMessage(&Artifact{Title: c.str("title"), ProjectPath: c.str("path")}, c.str("text")), nil
	case "continuePrompt":
		return ContinuePrompt(&Artifact{Title: c.str("title"), Boards: make([]Board, c.num("boards"))}, c.str("task")), nil
	case "parseControl":
		v, _ := jsvalue.Parse([]byte(c.str("json")))
		if ctl := ParseControl(v); ctl != nil {
			return json.RawMessage(controlJSON(*ctl)), nil
		}
		return nil, nil
	case "sortRu", "sortRuPlain":
		var names []string
		_ = json.Unmarshal(c.Input["names"], &names)
		cmp := compareRu
		if c.Fn == "sortRuPlain" {
			cmp = compareRuPlain
		}
		sort.SliceStable(names, func(i, j int) bool { return cmp(names[i], names[j]) < 0 })
		return names, nil
	}
	return nil, nil
}

func canonical(b []byte) string {
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return string(b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// TestGeneratedUnits runs the committed generated cases, or the large set named by DESIGN_UNITS.
func TestGeneratedUnits(t *testing.T) {
	file := "testdata/units.json"
	if f := os.Getenv("DESIGN_UNITS"); f != "" {
		file = f
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cases []unitCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	failed := map[string]int{}
	for i, c := range cases {
		got, err := runUnit(c)
		if c.Output.Error != nil {
			if err == nil || err.Error() != *c.Output.Error {
				failed[c.Fn]++
				if failed[c.Fn] <= 3 {
					t.Errorf("#%d %s %v: got %v, %v; want error %q", i, c.Fn, c.Input, got, err, *c.Output.Error)
				}
			}
			continue
		}
		b, _ := json.Marshal(got)
		if canonical(b) != canonical(c.Output.OK) || err != nil {
			failed[c.Fn]++
			if failed[c.Fn] <= 3 {
				in, _ := json.Marshal(c.Input)
				t.Errorf("#%d %s %s:\n got %s (%v)\nwant %s", i, c.Fn, in, b, err, c.Output.OK)
			}
		}
	}
	if len(failed) > 0 {
		t.Errorf("failed per function: %v of %d", failed, len(cases))
	}
}
