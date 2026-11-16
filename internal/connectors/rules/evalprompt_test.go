package rules

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
	"go.yaml.in/yaml/v3"
)

// The evals connector-rule-* give the agent the very prompt a rule makes (evals/agent/connector-rule-*.yaml): the
// prompt of each is built here from the same event, so a change of the template or of Prompt shows up as a failing
// test until the scenario is brought along (UPDATE_EVALS=1 writes it).
func evalCases() map[string]string {
	notes := connectors.Conn{ID: "cxnotes", Connector: "folder", Account: "~/work/notes", State: connectors.StateOK}
	out := map[string]string{}
	for file, title := range map[string]string{
		"connector-rule-homework.yaml":  "Матан ДЗ 5",
		"connector-rule-injection.yaml": "ДЗ по истории",
	} {
		t, _ := byID("homework")
		path := title + ".md"
		env := &connectors.Envelope{Event: connectors.Event{Type: "note.created", ID: "note:" + path, Title: title,
			Ref: "file path=" + path, Facts: map[string]string{"path": path}},
			Fingerprint: "folder:cxnotes:note:" + path, Connector: "folder", Kind: connectors.Files, Connection: "cxnotes",
			Account: notes.Account, Read: connectors.ReadCommand("cxnotes", "file path="+path)}
		env.At = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
		out[file] = Prompt(t, notes, Setting{}, env, []connectors.Conn{notes})
	}
	return out
}

func TestEvalScenariosCarryTheRulesPrompt(t *testing.T) {
	for file, want := range evalCases() {
		path := "../../../evals/agent/" + file
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var sc struct {
			Prompt string `yaml:"prompt"`
		}
		if err := yaml.Unmarshal(raw, &sc); err != nil {
			t.Fatal(err)
		}
		if sc.Prompt == want {
			continue
		}
		if os.Getenv("UPDATE_EVALS") == "" {
			t.Fatalf("%s: the prompt is not the rule's; run with UPDATE_EVALS=1", file)
		}
		var node yaml.Node
		_ = yaml.Unmarshal(raw, &node)
		for i, n := range node.Content[0].Content {
			if n.Value == "prompt" {
				v := node.Content[0].Content[i+1]
				v.Value, v.Style = want, yaml.LiteralStyle
			}
		}
		var b strings.Builder
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		_ = enc.Encode(&node)
		_ = os.WriteFile(path, []byte(b.String()), 0o644)
	}
}
