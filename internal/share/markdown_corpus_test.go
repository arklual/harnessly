package share

import (
	"encoding/json"
	"os"
	"testing"
)

// MD_CORPUS (a JSON list of Markdown texts) is rendered to MD_OUT, to compare with the previous runtime's page on a corpus.
func TestMarkdownCorpus(t *testing.T) {
	in := os.Getenv("MD_CORPUS")
	if in == "" {
		t.Skip()
	}
	raw, _ := os.ReadFile(in)
	var docs []string
	_ = json.Unmarshal(raw, &docs)
	transform := func(u string) (string, bool) {
		p, ok := parseURL(u)
		if !ok {
			return "", false
		}
		return p.href, true
	}
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = `<div class="tw-share-md">` + renderMarkdownHTML(d, transform) + `</div>`
	}
	b, _ := json.Marshal(out)
	_ = os.WriteFile(os.Getenv("MD_OUT"), b, 0o644)
}
