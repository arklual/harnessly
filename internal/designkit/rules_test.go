package designkit

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const kitDocs = "../../docs/agents/design-kits.md"

// Every rule the check can report is used by the code and explained on the docs page, and the page names no rule
// the check does not have: the registry, the code and the docs cannot drift apart.
func TestRulesAreUsedAndDocumented(t *testing.T) {
	var src strings.Builder
	for _, f := range []string{"check.go", "fields.go", "../hlcli/designkit.go"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(raw)
	}
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?:k\.add\(|Rule: )"([a-z-]+\.[a-z-]+)"`).FindAllStringSubmatch(src.String(), -1) {
		used[m[1]] = true
	}
	raw, err := os.ReadFile(kitDocs)
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]string{}
	for _, m := range regexp.MustCompile("(?m)^\\| `([a-z-]+\\.[a-z-]+)` \\| (error|warning) \\|").FindAllStringSubmatch(string(raw), -1) {
		documented[m[1]] = m[2]
	}
	var missing []string
	for _, r := range Rules {
		if !used[r.ID] {
			missing = append(missing, r.ID+": not used by the check")
		}
		if lv, ok := documented[r.ID]; !ok {
			missing = append(missing, r.ID+": not in the docs' table")
		} else if lv != r.Level {
			missing = append(missing, r.ID+": the docs say "+lv)
		}
		delete(used, r.ID)
		delete(documented, r.ID)
	}
	for id := range used {
		missing = append(missing, id+": used, not in the registry")
	}
	for id := range documented {
		missing = append(missing, id+": in the docs, not in the registry")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("rules out of sync:\n%s", strings.Join(missing, "\n"))
	}
}

// The docs' JSON examples are JSON: an agent copies them. A block that leaves parts out says so with ```jsonc.
func TestDocsJSONExamplesParse(t *testing.T) {
	raw, err := os.ReadFile(kitDocs)
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```json\n(.*?)```").FindAllStringSubmatch(string(raw), -1)
	if len(blocks) < 4 {
		t.Fatalf("only %d json blocks", len(blocks))
	}
	for _, b := range blocks {
		var v any
		if err := json.Unmarshal([]byte(b[1]), &v); err != nil {
			t.Errorf("a json block does not parse (%v):\n%s", err, b[1])
		}
	}
}
