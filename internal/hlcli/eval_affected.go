package hlcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agenteval"
)

// exitNeedsScenario is `eval affected --check`'s answer when the agent's surface changed without a scenario: CI shows
// the job as a warning (allow_failure: exit_codes).
const exitNeedsScenario = 3

// gitChanged lists the repository paths a change touched: base...head, and with no base the last commit.
func (c *cli) gitChanged(base, head string) ([]string, error) {
	rng := head + "~1.." + head
	if base != "" {
		rng = base + "..." + head
	}
	cmd := exec.Command("git", "diff", "--name-only", rng)
	cmd.Dir = c.env.Cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %v", rng, err)
	}
	return strings.Fields(string(out)), nil
}

// evalAffected says which scenarios a change touches, and whether it changed the agent's surface without a scenario.
func (c *cli) evalAffected(args []string) error {
	fs := flag.NewFlagSet("eval affected", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	base := fs.String("base", "", "the commit the change is measured from (default: the last commit alone)")
	head := fs.String("head", "HEAD", "the change's last commit")
	dir := fs.String("dir", "evals/agent", "the scenarios' folder, from the repository's root")
	check := fs.Bool("check", false, "exit 3 when the surface changed and no scenario was added or changed with it")
	names := fs.Bool("names", false, "print only the affected scenarios, comma-separated (for eval run --scenarios)")
	asJSON := fs.Bool("json", false, "print the impact as JSON")
	if err := fs.Parse(args); err != nil {
		return usageErr("eval affected [--base REF] [--head REF] [--check] [--names] [--json]")
	}
	changed, err := c.gitChanged(*base, *head)
	if err != nil {
		return err
	}
	root := c.evalDir(*dir)
	surface, err := agenteval.ReadSurface(filepath.Join(root, "surface.txt"))
	if err != nil {
		return err
	}
	list, err := agenteval.Load(root)
	if err != nil {
		return err
	}
	im := agenteval.Affected(changed, surface, list, *dir)
	switch {
	case *names:
		c.out("%s\n", strings.Join(im.Scenarios, ","))
	case *asJSON:
		b, _ := json.MarshalIndent(im, "", "  ")
		c.out("%s\n", b)
	default:
		if len(im.Surface) == 0 {
			c.out("The agent's surface did not change.\n")
		} else {
			c.out("The agent's surface changed: %s\n", strings.Join(im.Surface, ", "))
		}
		if len(im.Scenarios) > 0 {
			c.out("Scenarios that check it: %s\n", strings.Join(im.Scenarios, ", "))
		}
		if len(im.ScenarioFiles) > 0 {
			c.out("Scenarios changed with it: %s\n", strings.Join(im.ScenarioFiles, ", "))
		}
		if im.NeedsScenario() {
			c.out("No scenario was added or changed with it: add one to %s (`harnessly eval suggest` drafts it), or name "+
				"the covered paths in an existing scenario's covers. See docs/agent-evals.md, «New features».\n", *dir)
		}
	}
	if *check && im.NeedsScenario() {
		return exitCode(exitNeedsScenario)
	}
	return nil
}

const suggestBrief = `You write a scenario for harnessly's agent evals (docs/agent-evals.md). A change of harnessly changed what
an agent is told or given (its workspace prompt, skills, MCP tools or harnessly commands); the scenario checks that a
fresh agent, given one plain request of a person, now does the right thing. It is judged only by facts: tool calls
(called / not_called with args regexps; MCP tools are named harnessly.<tool>, shell commands are Bash with a command
argument), the right pane's intents (pane: {tab, target}), pictures drawn (images: N), files (file: {path, contains}),
and text regexps over what the agent said. Write the person's request in Russian, the way a person types it, and never
name the tool or command in it. Hosts are examples under example.test. Answer with the YAML of one scenario only, no
fences, in exactly the shape of this example:

%s
Scenarios that exist already (do not repeat them): %s

The change:
%s`

var yamlFence = regexp.MustCompile("(?s)^\\s*```[a-z]*\\n(.*?)\\n```\\s*$")

// evalSuggest drafts a scenario for a change with a cheap model; a person or an agent edits it before it counts.
func (c *cli) evalSuggest(args []string) error {
	fs := flag.NewFlagSet("eval suggest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	base := fs.String("base", "", "the commit the change is measured from (default: the last commit alone)")
	head := fs.String("head", "HEAD", "the change's last commit")
	dir := fs.String("dir", "evals/agent", "the scenarios' folder, from the repository's root")
	model := fs.String("model", "haiku", "the cheap model that drafts it")
	write := fs.Bool("write", false, "write it to the scenarios' folder as <id>.yaml (never over an existing file)")
	maxDiff := fs.Int("max-diff", 24000, "how many bytes of the diff the model reads")
	if err := fs.Parse(args); err != nil {
		return usageErr("eval suggest [--base REF] [--head REF] [--model haiku] [--write]")
	}
	changed, err := c.gitChanged(*base, *head)
	if err != nil {
		return err
	}
	root := c.evalDir(*dir)
	surface, err := agenteval.ReadSurface(filepath.Join(root, "surface.txt"))
	if err != nil {
		return err
	}
	list, err := agenteval.Load(root)
	if err != nil {
		return err
	}
	im := agenteval.Affected(changed, surface, list, *dir)
	if len(im.Surface) == 0 {
		c.out("The agent's surface did not change: no scenario to suggest.\n")
		return nil
	}
	rng := *head + "~1.." + *head
	if *base != "" {
		rng = *base + "..." + *head
	}
	cmd := exec.Command("git", append([]string{"diff", "-U3", rng, "--"}, im.Surface...)...)
	cmd.Dir = c.env.Cwd
	diff, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git diff: %v", err)
	}
	if len(diff) > *maxDiff {
		diff = append(diff[:*maxDiff], []byte("\n[… the rest of the diff is cut]\n")...)
	}
	example, err := os.ReadFile(filepath.Join(root, "file-as-card.yaml"))
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(list))
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	q := exec.Command("claude", "-p", fmt.Sprintf(suggestBrief, example, strings.Join(ids, ", "), diff), "--model", *model,
		"--output-format", "json", "--no-session-persistence", "--strict-mcp-config", "--setting-sources", "project", "--tools", "")
	outb, err := q.Output()
	if err != nil {
		return fmt.Errorf("claude: %v", err)
	}
	var res struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(outb, &res); err != nil {
		return fmt.Errorf("claude answered no JSON: %v", err)
	}
	body := strings.TrimSpace(res.Result)
	if m := yamlFence.FindStringSubmatch(body); m != nil {
		body = m[1]
	}
	body += "\n"
	s, err := agenteval.ParseScenario("suggested.yaml", []byte(body))
	if err != nil {
		c.out("%s", body)
		return fmt.Errorf("the draft does not read as a scenario (%v): edit it by hand", err)
	}
	if !*write {
		c.out("%s", body)
		return nil
	}
	file := filepath.Join(root, s.ID+".yaml")
	if _, err := os.Stat(file); err == nil {
		c.out("%s", body)
		return fmt.Errorf("%s exists: not overwritten", file)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		return err
	}
	c.out("Wrote %s: a draft; check its expectations and run it (`harnessly eval run --scenarios %s`).\n", file, s.ID)
	return nil
}
