package screen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// screens.golden.json holds, for every capture in testdata/tui, what the previous runtime's parsers answered for it
// (written by running them over the same files). Every parser of this package must answer the same.

var goldenProbes = []string{"half-typed", "Also add the word DELTA.", "my unsent draft", "/model", "Explain this codebase"}

func nullable(s string, ok bool) any {
	if !ok {
		return nil
	}
	return s
}

// record is every reading of one capture, keyed as in the golden file.
func record(name, text string) map[string]any {
	plain := StripANSI(text)
	input, inputOK := ReadInputLine(text)
	frame := ObserveSendNow(text)
	still := map[string]any{}
	for _, p := range goldenProbes {
		still[p] = CodexStillInBox(text, p)
	}
	var codexUpdate any
	if u := MatchCodexUpdatePrompt(text); u != nil {
		codexUpdate = u
	}
	var trust any
	if tp := MatchTrustPrompt(text); tp != nil {
		trust = tp
	}
	var menu any
	if m := FindMenu(plain); m != nil {
		menu = m
	}
	r := map[string]any{
		"name":              name,
		"busy":              IsBusy(plain),
		"busyAnywhere":      IsBusyAnywhere(plain),
		"menu":              menu,
		"panelHints":        CountPanelHints(plain),
		"panelSnapshot":     PanelSnapshot(plain),
		"confirm":           IsLaunchConfirm(plain),
		"modelSet":          CountModelSet(plain),
		"effortSet":         CountEffortSet(plain),
		"kept":              CountKept(plain),
		"input":             nullable(input, inputOK),
		"queued":            QueueInPromptBox(text),
		"sendNow":           map[string]any{"input": nullable(frame.Input, frame.InputLine), "menu": frame.Menu, "queued": frame.Queued},
		"permission":        ParsePermissionOptions(text),
		"hasPermission":     HasPermissionPrompt(text),
		"choice":            ParseChoiceOptions(text),
		"plan":              ParsePlanOptions(text),
		"promptBox":         IsPromptBoxReady(text),
		"launchedPromptBox": IsLaunchedPromptBoxReady(text),
		"codexComposer":     IsCodexComposerReady(text),
		"codexUpdate":       codexUpdate,
		"trust":             trust,
		"question":          IsQuestionOnScreen(text),
		"stillInBox":        still,
	}
	// Through JSON, so both sides compare as plain JSON values.
	raw, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

// compareRecords checks every record of a golden file against texts by name.
func compareRecords(t *testing.T, goldenPath string, text func(name string) string) {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var want []map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("empty golden file")
	}
	for _, w := range want {
		name := w["name"].(string)
		got := record(name, text(name))
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !reflect.DeepEqual(got[k], w[k]) {
				g, _ := json.Marshal(got[k])
				e, _ := json.Marshal(w[k])
				t.Errorf("%s: %s\n got  %s\n want %s", name, k, g, e)
			}
		}
		if len(got) != len(w) {
			t.Errorf("%s: %d readings, golden has %d", name, len(got), len(w))
		}
	}
}

func TestGoldenScreens(t *testing.T) {
	compareRecords(t, filepath.Join("testdata", "screens.golden.json"), func(name string) string {
		return fixture(t, name)
	})
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tui", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
