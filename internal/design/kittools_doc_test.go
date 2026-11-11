package design

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

const kitMCPDocs = "../../docs/agents/design-kit-mcp.md"

// docExamples are the fenced blocks of the page written ```json <tool>.
func docExamples(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(kitMCPDocs)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile("(?s)```json (kit_[a-z_]+)\n(.*?)```").FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = m[2]
	}
	return out
}

// dynamicKeys name members whose keys are the kit's data (token families, levels, a sample's props), not the schema.
var dynamicKeys = map[string]bool{"tokens": true, "groups": true, "levels": true, "sample": true}

func keyPaths(prefix string, v any, into map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			p := prefix + k
			into[p] = true
			if dynamicKeys[k] {
				continue
			}
			// the props of a variant, a preset or a layout are values of the kit; a component's props are a list
			if _, isMap := e.(map[string]any); isMap && k == "props" {
				continue
			}
			keyPaths(p+".", e, into)
		}
	case []any:
		for _, e := range x {
			keyPaths(prefix[:len(prefix)-1]+"[].", e, into)
		}
	}
}

func sortedPaths(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The page's example of every tool has the fields the tool answers: the schema in the docs is under test.
func TestKitMCPDocsExamplesHaveTheFieldsOfTheAnswers(t *testing.T) {
	l := kitToolsLibrary(t)
	docs := docExamples(t)
	calls := map[string][]any{
		"kit_list":      {},
		"kit_index":     {"kit", "acme-ui"},
		"kit_component": {"kit", "acme-ui", "name", "Button"},
		"kit_search":    {"kit", "acme-ui", "query", "submit"},
		"kit_tokens":    {"kit", "acme-ui"},
		"kit_templates": {"kit", "acme-ui"},
		"kit_template":  {"kit", "acme-ui", "id", "record-page"},
		"kit_check":     {"kit", "acme-ui"},
	}
	for _, name := range KitToolNames {
		ex, ok := docs[name]
		if !ok {
			t.Errorf("%s: the page has no ```json %s example", name, name)
			continue
		}
		var shown any
		if err := json.Unmarshal([]byte(ex), &shown); err != nil {
			t.Errorf("%s: the example is not JSON: %v", name, err)
			continue
		}
		_, text, bad := callKit(t, l, name, calls[name]...)
		if bad {
			t.Errorf("%s: refused: %s", name, text)
			continue
		}
		var got any
		_ = json.Unmarshal([]byte(text), &got)
		a, b := map[string]bool{}, map[string]bool{}
		keyPaths("", shown, a)
		keyPaths("", got, b)
		if strings.Join(sortedPaths(a), " ") != strings.Join(sortedPaths(b), " ") {
			t.Errorf("%s: the example and the answer have different fields\nexample: %v\nanswer:  %v", name, sortedPaths(a), sortedPaths(b))
		}
		if m, _ := shown.(map[string]any); m["schema"] != designkit.ViewSchema {
			t.Errorf("%s: the example's schema is %v, the code's %s", name, m["schema"], designkit.ViewSchema)
		}
	}
	raw, _ := os.ReadFile(kitMCPDocs)
	for _, name := range KitToolNames {
		if !regexp.MustCompile("`" + name + "[` ]").Match(raw) {
			t.Errorf("the page does not name %s", name)
		}
	}
}
