package transcript

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// A Workflow call reads as the name its script gives itself, not as the first characters of the script.
func TestAWorkflowCallIsNamedByItsScript(t *testing.T) {
	var got []string
	for _, e := range ParseFile(filepath.Join("..", "workflows", "testdata", "conv.jsonl")) {
		if e.String("type") == "tool-call" && e.String("toolName") == "Workflow" {
			got = append(got, e.String("summary"))
		}
	}
	if len(got) != 2 || got[0] != "Workflow pricing-research" || got[1] != "Workflow checkout-review" {
		t.Fatalf("%q", got)
	}
	if s := summarizeToolCall("Workflow", jsvalue.NewObject("scriptPath", "/s/workflows/scripts/checkout-review-wf_3c9e1a7b-5d2.js")); s != "Workflow checkout-review" {
		t.Fatalf("%q", s)
	}
}
