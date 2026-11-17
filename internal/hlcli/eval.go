package hlcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agenteval"
)

const evalHelp = `harnessly eval: does harnessly by itself make an agent behave (docs/agent-evals.md)

  harnessly eval list     [--dir evals/agent]
  harnessly eval run      --models haiku,opus [--scenarios a,b] [--set dev|heldout|all] [--variants canon|all|N]
                          [--repeats 3] [--parallel 3] [--max-runs 150] [--max-usd 20] [--setup NAME] [--reuse]
                          [--runtime PATH] [--tools DIR:DIR] [--login DIR] [--root DIR] [--port-base 19400]
  harnessly eval report   [--run DIR] [--against DIR] [--root DIR]
  harnessly eval variants SCENARIO.yaml [--n 6] [--model haiku] [--write]
  harnessly eval affected [--base REF] [--head REF] [--check] [--names] [--json]
  harnessly eval suggest  [--base REF] [--head REF] [--model haiku] [--write]

A change of the agent's surface (evals/agent/surface.txt: the workspace prompt, skills, MCP tools, harnessly
commands) comes with a scenario: affected names the scenarios a change touches (their covers) and, with --check, exits 3
when the surface changed and no scenario did; suggest drafts one for the change with a cheap model.

A run starts a disposable stand per run (its own home, runtime, port and workspace, no memory, no plugins), asks the
agent the scenario's words, and judges the facts the run left. Results, transcripts and the report go to
<root>/runs/<time>-<setup>/ (root: ~/.harnessly-eval).
`

func (c *cli) eval(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.env.Stdout, evalHelp)
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return c.evalList(args[1:])
	case "run":
		return c.evalRun(args[1:])
	case "report":
		return c.evalReport(args[1:])
	case "variants":
		return c.evalVariants(args[1:])
	case "affected":
		return c.evalAffected(args[1:])
	case "suggest":
		return c.evalSuggest(args[1:])
	case "fake-imagegen":
		return agenteval.FakeImagegen(args[1:], c.env.Stdout)
	case "sandbox":
		var rw, ro []string
		rest := args[1:]
		for len(rest) > 0 && rest[0] != "--" {
			if len(rest) < 2 || (rest[0] != "--rw" && rest[0] != "--ro") {
				return usageErr("eval sandbox --rw DIR --ro DIR ... -- COMMAND")
			}
			if rest[0] == "--rw" {
				rw = append(rw, rest[1])
			} else {
				ro = append(ro, rest[1])
			}
			rest = rest[2:]
		}
		if len(rest) < 2 {
			return usageErr("eval sandbox --rw DIR --ro DIR ... -- COMMAND")
		}
		return agenteval.SandboxExec(rw, ro, rest[1:])
	}
	return usageErr("eval: list, run, report or variants")
}

func (c *cli) evalRoot(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.Join(c.env.Home, ".harnessly-eval")
}

func (c *cli) evalDir(v string) string {
	if v == "" {
		v = "evals/agent"
	}
	if !filepath.IsAbs(v) {
		v = filepath.Join(c.env.Cwd, v)
	}
	return v
}

func (c *cli) evalList(args []string) error {
	fs := flag.NewFlagSet("eval list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the scenarios' folder")
	if err := fs.Parse(args); err != nil {
		return usageErr("eval list: %v", err)
	}
	list, err := agenteval.Load(c.evalDir(*dir))
	for _, s := range list {
		agents := "claude, codex"
		if len(s.Agents) > 0 {
			agents = strings.Join(s.Agents, ", ")
		}
		c.out("%-28s %-7s %d variant(s) %d expectation(s) [%s]  %s\n", s.ID, s.Set, len(s.Variants), len(s.Expect), agents, s.Title)
	}
	return err
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func (c *cli) evalRun(args []string) error {
	fs := flag.NewFlagSet("eval run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the scenarios' folder (default evals/agent)")
	models := fs.String("models", "haiku", "models: haiku, opus, claude:<id>, codex:<model>")
	effort := fs.String("effort", "", "explicit reasoning effort: low or medium (empty: model default)")
	only := fs.String("scenarios", "", "only these scenario ids")
	set := fs.String("set", "dev", "dev, heldout or all")
	variants := fs.String("variants", "canon", "canon, all or a number of stored variants")
	repeats := fs.Int("repeats", 3, "runs per prompt")
	parallel := fs.Int("parallel", 3, "stands at once")
	maxRuns := fs.Int("max-runs", 150, "at most this many agent runs (retries count)")
	maxUSD := fs.Float64("max-usd", 0, "stop starting runs past this spend (0: no limit)")
	retries := fs.Int("retries", 2, "tries again after a failure of the stand")
	setup := fs.String("setup", "", "the name of the environment under test (default: the runtime's build)")
	reuse := fs.Bool("reuse", false, "take runs already made with the same scenario, prompt, model and setup")
	runtime := fs.String("runtime", "", "the runtime binary (default: harnessly-runtime or runtime beside this one)")
	tools := fs.String("tools", "", "folders with claude and codex, colon-separated (default: the PATH)")
	login := fs.String("login", "", "folder with claude-credentials.json, claude-account.json, codex-auth.json (default <root>/login)")
	root := fs.String("root", "", "where runs are kept (default ~/.harnessly-eval)")
	portBase := fs.Int("port-base", 19400, "the first stand's port")
	realImages := fs.Bool("real-images", false, "draw pictures with Codex for real")
	setupFile := fs.String("setup-file", "", "the setup under test: runtime and agent settings (default <dir>/setups/default.yaml)")
	if err := fs.Parse(args); err != nil {
		return usageErr("eval run: %v", err)
	}
	if *effort != "" && *effort != "low" && *effort != "medium" {
		return usageErr("eval run: --effort must be low or medium")
	}
	all, err := agenteval.Load(c.evalDir(*dir))
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, id := range splitList(*only) {
		want[id] = true
	}
	picked := len(want) > 0
	var scenarios []*agenteval.Scenario
	for _, s := range all {
		if picked && !want[s.ID] || !picked && *set != "all" && s.Set != *set {
			continue
		}
		delete(want, s.ID)
		scenarios = append(scenarios, s)
	}
	for id := range want {
		return usageErr("eval run: no scenario %s", id)
	}
	var ms []agenteval.Model
	for _, m := range splitList(*models) {
		mm, err := agenteval.ParseModel(m)
		if err != nil {
			return usageErr("eval run: %v", err)
		}
		mm.Effort = *effort
		ms = append(ms, mm)
	}
	self, _ := os.Executable()
	rt := *runtime
	if rt == "" {
		for _, n := range []string{"harnessly-runtime", "runtime"} {
			if p := filepath.Join(filepath.Dir(self), n); fileExists(p) {
				rt = p
				break
			}
		}
	}
	if rt == "" {
		return usageErr("eval run: --runtime is the runtime binary")
	}
	var toolDirs []string
	if *tools != "" {
		toolDirs = filepath.SplitList(*tools)
	} else {
		for _, n := range []string{"claude", "codex"} {
			if p, err := exec.LookPath(n); err == nil {
				toolDirs = append(toolDirs, filepath.Dir(p))
			}
		}
	}
	base := c.evalRoot(*root)
	sf := *setupFile
	if sf == "" {
		if p := filepath.Join(c.evalDir(*dir), "setups", "default.yaml"); fileExists(p) {
			sf = p
		}
	}
	var su agenteval.SetupFile
	if sf != "" {
		if su, err = agenteval.ReadSetup(sf); err != nil {
			return err
		}
	}
	if *setup == "" {
		*setup = strings.Trim(su.Name+"-"+buildID(rt), "-")
	}
	if agenteval.SandboxAvailable() == 0 {
		return errors.New("eval run: no Landlock on this machine, and stands run only inside it (their agents would see every file of the account)")
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	out := filepath.Join(base, "runs", stamp+"-"+safeName(*setup))
	loginDir := *login
	if loginDir == "" {
		loginDir = filepath.Join(base, "login")
	}
	o := agenteval.Options{Scenarios: scenarios, Models: ms, Variants: *variants, Repeats: *repeats, Parallel: *parallel,
		MaxRuns: *maxRuns, MaxUSD: *maxUSD, Retries: *retries, Setup: *setup, Out: out, PortBase: *portBase, Log: c.env.Stderr,
		Stand: agenteval.StandConfig{Root: filepath.Join(out, "stands"), Runtime: rt, Harnessly: self, Tools: toolDirs, Login: loginDir,
			RealImages: *realImages, RuntimeEnv: su.RuntimeEnv, AgentEnv: su.AgentEnv, ClaudeConfig: su.ClaudeConfig, PromptSuffix: su.PromptSuffix}}
	if *reuse {
		o.Reuse = map[string]agenteval.Result{}
		for _, prev := range previousRuns(base, "") {
			rs, _ := agenteval.ReadResults(filepath.Join(prev, "results.jsonl"))
			for _, r := range rs {
				o.Reuse[r.Key] = r
			}
		}
	}
	cells := o.Cells()
	fmt.Fprintf(c.env.Stderr, "%d scenario(s) × %d model(s) → %d run(s) (at most %d), %d at once → %s\n", len(scenarios), len(ms), len(cells), *maxRuns, *parallel, out)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	results, runErr := agenteval.Run(ctx, o)
	against := previousRuns(base, out)
	var prev []agenteval.Result
	if len(against) > 0 {
		prev, _ = agenteval.ReadResults(filepath.Join(against[0], "results.jsonl"))
	}
	if err := c.writeReport(out, results, prev, against); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(out, "stands"))
	return runErr
}

func (c *cli) writeReport(out string, results, prev []agenteval.Result, against []string) error {
	var before []agenteval.Result
	if len(against) > 0 {
		before = prev
	}
	rep := agenteval.Summarize(results, before, 0)
	title := "Agent evals · " + filepath.Base(out)
	md := rep.Markdown(title)
	if len(against) > 0 {
		md += "\nCompared with `" + filepath.Base(against[0]) + "`.\n"
	}
	if err := os.WriteFile(filepath.Join(out, "report.md"), []byte(md), 0o600); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "report.json"), b, 0o600)
	if c.json {
		c.out("%s\n", b)
	} else {
		c.out("%s", md)
	}
	return nil
}

func (c *cli) evalReport(args []string) error {
	fs := flag.NewFlagSet("eval report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	run := fs.String("run", "", "the run's folder (default: the latest)")
	againstDir := fs.String("against", "", "an earlier run to compare with (default: the one before)")
	root := fs.String("root", "", "where runs are kept")
	if err := fs.Parse(args); err != nil {
		return usageErr("eval report: %v", err)
	}
	base := c.evalRoot(*root)
	dir := *run
	if dir == "" {
		runs := previousRuns(base, "")
		if len(runs) == 0 {
			return usageErr("eval report: no runs in %s", base)
		}
		dir = runs[0]
	}
	results, err := agenteval.ReadResults(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		return err
	}
	var against []string
	if *againstDir != "" {
		against = []string{*againstDir}
	} else {
		against = previousRuns(base, dir)
	}
	var prev []agenteval.Result
	if len(against) > 0 {
		prev, _ = agenteval.ReadResults(filepath.Join(against[0], "results.jsonl"))
	}
	return c.writeReport(dir, results, prev, against)
}

// previousRuns are the run folders of root, newest first, older than (and other than) the given one.
func previousRuns(root, than string) []string {
	dirs, _ := filepath.Glob(filepath.Join(root, "runs", "*"))
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	var out []string
	for _, d := range dirs {
		if than != "" && (d == than || filepath.Base(d) > filepath.Base(than)) {
			continue
		}
		if fileExists(filepath.Join(d, "results.jsonl")) {
			out = append(out, d)
		}
	}
	return out
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeName(s string) string { return strings.Trim(unsafeName.ReplaceAllString(s, "-"), "-") }

// buildID names a runtime build by its file: size and time, enough to tell two builds apart.
func buildID(bin string) string {
	st, err := os.Stat(bin)
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("build-%s", st.ModTime().UTC().Format("0102-1504"))
}

const variantsBrief = `You write test prompts for an AI agent. Below is what a person says to the agent in a chat. Rewrite it %d
times. Every rewrite keeps exactly the same request and every concrete detail (names, file names, quoted words,
numbers), adds no new request or detail, and differs in wording from the original and from the other rewrites. Kinds, in
this order: "short" (a few words, as people type in a hurry), "long" (chatty, with context about why), "typos" (two or
three typos, no capitals or punctuation), "english" (entirely in English; names and quoted words stay as they are),
"formal" (polite, full sentences), "indirect" (describes the need without the obvious verb; never names a book, film,
brand or famous character that was not in the original). Answer with JSON only: an array of objects
{"kind": "...", "text": "..."}.

The person says:
%s`

func (c *cli) evalVariants(args []string) error {
	fs := flag.NewFlagSet("eval variants", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	n := fs.Int("n", 6, "how many variants")
	model := fs.String("model", "haiku", "the cheap model that writes them")
	write := fs.Bool("write", false, "append them to the scenario file (only when it has none)")
	if err := fs.Parse(reorder(args)); err != nil || fs.NArg() != 1 {
		return usageErr("eval variants SCENARIO.yaml [--n 6] [--model haiku] [--write]")
	}
	file := fs.Arg(0)
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	s, err := agenteval.ParseScenario(filepath.Base(file), raw)
	if err != nil {
		return err
	}
	cmd := exec.Command("claude", "-p", fmt.Sprintf(variantsBrief, *n, s.Prompt), "--model", *model, "--output-format", "json",
		"--no-session-persistence", "--strict-mcp-config", "--setting-sources", "project", "--tools", "")
	cmd.Stdin = nil
	outb, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude: %v", err)
	}
	var res struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(outb, &res); err != nil {
		return fmt.Errorf("claude answered no JSON: %v", err)
	}
	text := res.Result
	if i, j := strings.Index(text, "["), strings.LastIndex(text, "]"); i >= 0 && j > i {
		text = text[i : j+1]
	}
	var vs []agenteval.Variant
	if err := json.Unmarshal([]byte(text), &vs); err != nil {
		return fmt.Errorf("the variants are not a JSON array: %v", err)
	}
	var b strings.Builder
	b.WriteString("variants:\n")
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(s.Prompt)): true}
	kept := 0
	for _, v := range vs {
		t := strings.TrimSpace(v.Text)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		kept++
		q, _ := json.Marshal(t)
		fmt.Fprintf(&b, "  - id: v%d\n    kind: %s\n    text: %s\n", kept, safeName(v.Kind), q)
	}
	if !*write {
		c.out("%s", b.String())
		return nil
	}
	if len(s.Variants) > 0 {
		return usageErr("eval variants: %s has variants already; edit them by hand", file)
	}
	body := strings.TrimRight(string(raw), "\n") + "\n" + b.String()
	if _, err := agenteval.ParseScenario(filepath.Base(file), []byte(body)); err != nil {
		return fmt.Errorf("the scenario with variants does not read: %v", err)
	}
	return os.WriteFile(file, []byte(body), 0o644)
}

// reorder puts flags before the positional arguments, so `variants FILE --write` parses.
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && a != "--write" && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}
