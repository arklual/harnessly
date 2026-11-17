package agenteval

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/pricing"
)

// Model is one column of the matrix: an agent CLI and the model it runs.
type Model struct {
	// Key names the column in the report ("haiku", "codex:gpt-5.6-sol").
	Key    string `json:"key"`
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

// ParseModel reads "haiku", "opus", "claude:<id>" or "codex:<model>".
func ParseModel(s string) (Model, error) {
	s = strings.TrimSpace(s)
	agent, model, ok := strings.Cut(s, ":")
	if !ok {
		agent, model = "claude", s
	}
	if agent != "claude" && agent != "codex" {
		return Model{}, fmt.Errorf("%q: a model is <name> (Claude) or claude:<id> / codex:<model>", s)
	}
	if model == "" && agent == "claude" {
		return Model{}, fmt.Errorf("%q: which model", s)
	}
	return Model{Key: s, Agent: agent, Model: model}, nil
}

// Cell is one run of the matrix.
type Cell struct {
	Scenario *Scenario
	Variant  Variant
	Model    Model
	Rep      int
}

// Key is what makes two runs the same run: the cache's key.
func (c Cell) Key(setup string) string {
	modelKey := c.Model.Key
	if c.Model.Effort != "" {
		modelKey += "@effort=" + c.Model.Effort
	}
	h := sha256.Sum256([]byte(strings.Join([]string{c.Scenario.ID, c.Scenario.Fingerprint(), c.Variant.ID, c.Variant.Text, modelKey,
		setup, fmt.Sprint(c.Rep)}, "\x00")))
	return hex.EncodeToString(h[:])[:16]
}

// States of a run.
const (
	Pass  = "pass"
	Fail  = "fail"
	Infra = "infra"
)

// Result is one run as the results file keeps it.
type Result struct {
	Key       string    `json:"key"`
	Scenario  string    `json:"scenario"`
	Set       string    `json:"set"`
	Variant   string    `json:"variant"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	Effort    string    `json:"effort,omitempty"`
	Rep       int       `json:"rep"`
	Setup     string    `json:"setup"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	Verdicts  []Verdict `json:"verdicts,omitempty"`
	Facts     *Facts    `json:"facts,omitempty"`
	TurnState string    `json:"turnState,omitempty"`
	WallS     int       `json:"wallS"`
	// ActiveS is from the start to the last thing an agent of the stand wrote: the work's time, without the wait for
	// quiet.
	ActiveS int `json:"activeS,omitempty"`
	// InterruptAt is when the scenario's interrupt was sent (Unix seconds).
	InterruptAt int64   `json:"interruptAt,omitempty"`
	USD         float64 `json:"usd"`
	CostUnknown bool    `json:"costUnknown,omitempty"`
	Transcript  string  `json:"transcript,omitempty"`
	StartedAt   string  `json:"startedAt"`
	Reused      bool    `json:"reused,omitempty"`
}

// Options steer a run of the matrix.
type Options struct {
	Scenarios []*Scenario
	Models    []Model
	// Variants: "canon" (default), "all", or a number of stored variants besides the canonical one.
	Variants string
	Repeats  int
	Parallel int
	MaxRuns  int
	MaxUSD   float64
	// Retries are the extra tries of a run that failed for the stand's reasons.
	Retries int
	// Setup names the environment under test (a harnessly build, an instruction variant): runs compare within one.
	Setup string
	// Out is this run's folder; Reuse are earlier results whose runs need not run again.
	Out      string
	Reuse    map[string]Result
	Stand    StandConfig
	PortBase int
	Log      io.Writer
}

// Cells expands the matrix.
func (o Options) Cells() []Cell {
	var out []Cell
	for _, s := range o.Scenarios {
		vs := s.Prompts()
		switch o.Variants {
		case "all":
		case "", "canon":
			vs = vs[:1]
		default:
			var n int
			if _, err := fmt.Sscan(o.Variants, &n); err == nil && n+1 < len(vs) {
				vs = vs[:n+1]
			}
		}
		for _, m := range o.Models {
			if !s.AppliesTo(m.Agent) {
				continue
			}
			for _, v := range vs {
				for r := 0; r < max(1, o.Repeats); r++ {
					out = append(out, Cell{Scenario: s, Variant: v, Model: m, Rep: r})
				}
			}
		}
	}
	return out
}

// Run runs the matrix and writes results.jsonl into o.Out as it goes.
func Run(ctx context.Context, o Options) ([]Result, error) {
	if err := os.MkdirAll(filepath.Join(o.Out, "transcripts"), 0o700); err != nil {
		return nil, err
	}
	resf, err := os.OpenFile(filepath.Join(o.Out, "results.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer resf.Close()
	cells := o.Cells()
	logf := func(format string, a ...any) {
		if o.Log != nil {
			fmt.Fprintf(o.Log, time.Now().Format("15:04:05 ")+format+"\n", a...)
		}
	}
	var (
		mu      sync.Mutex
		results []Result
		started atomic.Int64
		spent   float64
		stop    bool
	)
	record := func(r Result) {
		mu.Lock()
		defer mu.Unlock()
		results = append(results, r)
		spent += r.USD
		if o.MaxUSD > 0 && r.CostUnknown && !stop {
			stop = true
			logf("budget: model cost is unknown, no new runs")
		}
		if o.MaxUSD > 0 && spent >= o.MaxUSD && !stop {
			stop = true
			logf("budget: $%.2f spent, no new runs", spent)
		}
		b, _ := json.Marshal(r)
		_, _ = resf.Write(append(b, '\n'))
	}
	queue := make(chan Cell)
	go func() {
		defer close(queue)
		for _, c := range cells {
			select {
			case queue <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	par := max(1, o.Parallel)
	var wg sync.WaitGroup
	for w := 0; w < par; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for c := range queue {
				key := c.Key(o.Setup)
				if prev, ok := o.Reuse[key]; ok && prev.State != Infra {
					prev.Reused = true
					// Legacy results may have silently priced OpenAI usage as zero.
					// Reprice the copy, never overwrite the original evidence.
					if prev.Facts != nil {
						var known bool
						prev.USD, known = pricedCost(*prev.Facts)
						prev.CostUnknown = !known
					} else if prev.USD == 0 {
						prev.CostUnknown = true
					}
					record(prev)
					continue
				}
				mu.Lock()
				halted := stop
				mu.Unlock()
				if halted || ctx.Err() != nil || (o.MaxRuns > 0 && started.Load() >= int64(o.MaxRuns)) {
					continue
				}
				var r Result
				for try := 0; try <= o.Retries; try++ {
					started.Add(1)
					r = runOne(ctx, o, c, key, fmt.Sprintf("w%d", w), o.PortBase+w)
					if r.State != Infra || ctx.Err() != nil {
						break
					}
					logf("infra %s/%s/%s: %s — again", c.Scenario.ID, c.Model.Key, c.Variant.ID, r.Error)
				}
				price := fmt.Sprintf("$%.3f", r.USD)
				if r.CostUnknown {
					price = "cost unknown"
				}
				logf("%-4s %s · %s · %s #%d · %ds %s %s", r.State, c.Scenario.ID, c.Model.Key, c.Variant.ID, c.Rep, r.WallS, price, firstFail(r))
				record(r)
			}
		}(w)
	}
	wg.Wait()
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Variant != b.Variant {
			return a.Variant < b.Variant
		}
		return a.Rep < b.Rep
	})
	if err := ctx.Err(); err != nil {
		return results, err
	}
	infraCount := 0
	for _, r := range results {
		if r.State == Infra {
			infraCount++
		}
	}
	if infraCount > 0 {
		return results, fmt.Errorf("%w: %d run(s) failed; see the recorded results", ErrInfra, infraCount)
	}
	return results, nil
}

func firstFail(r Result) string {
	if r.State == Infra {
		return r.Error
	}
	for _, v := range r.Verdicts {
		if !v.OK {
			return "✗ " + v.Name + ": " + clip(v.Why, 160)
		}
	}
	return ""
}

func runOne(ctx context.Context, o Options, c Cell, key, worker string, port int) Result {
	t0 := time.Now()
	r := Result{Key: key, Scenario: c.Scenario.ID, Set: c.Scenario.Set, Variant: c.Variant.ID, Prompt: c.Variant.Text, Model: c.Model.Key,
		Rep: c.Rep, Effort: c.Model.Effort, Setup: o.Setup, StartedAt: t0.UTC().Format(time.RFC3339)}
	finish := func(state string, err error) Result {
		r.State, r.WallS = state, int(time.Since(t0).Seconds())
		if err != nil {
			r.Error = err.Error()
		}
		return r
	}
	cfg := o.Stand
	cfg.RuntimeEnv = merged(cfg.RuntimeEnv, c.Scenario.Setup.RuntimeEnv)
	stand, err := Up(cfg, worker, port)
	if err != nil {
		return finish(Infra, err)
	}
	defer stand.Down()
	if err := prepare(stand, c.Scenario); err != nil {
		return finish(Infra, err)
	}
	prompt := c.Variant.Text
	if sfx := strings.TrimSpace(o.Stand.PromptSuffix); sfx != "" {
		prompt += "\n\n" + sfx
	}
	tab, err := stand.ChatWithEffort(c.Model.Agent, c.Model.Model, c.Model.Effort, prompt)
	if err != nil {
		return finish(Infra, err)
	}
	timeout := time.Duration(c.Scenario.Timeout()) * time.Second
	deadline := t0.Add(timeout)
	if it := c.Scenario.Interrupt; it != nil {
		at := time.Now().Add(time.Duration(it.AfterS) * time.Second)
		r.TurnState = stand.Wait(ctx, tab, time.Until(at))
		// A chat that delegated ends its turn early; the person still speaks at the same moment.
		time.Sleep(time.Until(at))
		// The person speaks while something still works: the chat itself or an agent it started.
		if r.TurnState == "timeout" || stand.chatState(tab) == "busy" || stand.working() || time.Since(stand.lastActivity()) < 30*time.Second {
			if err := stand.Send(tab, it.Text); err != nil {
				return finish(Infra, fmt.Errorf("send the interrupt: %v", err))
			}
			r.InterruptAt = time.Now().Unix()
			time.Sleep(5 * time.Second)
			r.TurnState = stand.Wait(ctx, tab, time.Until(deadline))
		}
	} else {
		r.TurnState = stand.Wait(ctx, tab, timeout)
	}
	settled := true
	if c.Scenario.SettleS > 0 && r.TurnState != "timeout" && ctx.Err() == nil {
		var last time.Time
		last, settled = stand.Settle(ctx, tab, time.Duration(c.Scenario.SettleS)*time.Second, deadline)
		if !last.IsZero() {
			r.ActiveS = max(0, int(last.Sub(t0).Seconds()))
		}
	} else if last := stand.lastActivity(); !last.IsZero() {
		r.ActiveS = max(0, int(last.Sub(t0).Seconds()))
	}
	keep := filepath.Join(o.Out, "transcripts", key+".jsonl")
	facts, err := stand.Collect(c.Model.Agent, tab, keep)
	if err != nil {
		return finish(Infra, err)
	}
	r.Transcript = filepath.Join("transcripts", key+".jsonl")
	for _, chk := range c.Scenario.Checks() {
		interrupt := ""
		if r.InterruptAt > 0 {
			interrupt = fmt.Sprint(r.InterruptAt)
		}
		code, out := stand.Exec(ctx, chk, []string{fmt.Sprintf("EVAL_STARTED_AT=%d", t0.Unix()), "EVAL_INTERRUPT_AT=" + interrupt}, 5*time.Minute)
		facts.Checks = append(facts.Checks, CheckResult{Command: chk, Exit: code, Output: out})
	}
	r.Facts = &facts
	var costKnown bool
	r.USD, costKnown = pricedCost(facts)
	r.CostUnknown = !costKnown
	if len(facts.Calls) == 0 && len(facts.Said) == 0 {
		why := "the agent did nothing (turn " + r.TurnState + ")"
		if len(facts.APIErrors) > 0 {
			why = "the provider failed: " + facts.APIErrors[0]
		}
		return finish(Infra, errors.New(why))
	}
	r.Verdicts = c.Scenario.Check(facts)
	if r.TurnState == "timeout" {
		r.Verdicts = append(r.Verdicts, Verdict{Name: "finished in time", OK: false, Why: fmt.Sprintf("still working after %ds", c.Scenario.Timeout())})
	} else if !settled {
		r.Verdicts = append(r.Verdicts, Verdict{Name: "finished in time", OK: false, Why: fmt.Sprintf("agents still working after %ds", c.Scenario.Timeout())})
	}
	if Passed(r.Verdicts) {
		return finish(Pass, nil)
	}
	return finish(Fail, nil)
}

// cost prices each model's tokens at its own rate.
func cost(f Facts) float64 {
	total, _ := pricedCost(f)
	return total
}

// pricedCost keeps unknown prices distinct from free calls, including mixed-model runs.
func pricedCost(f Facts) (float64, bool) {
	price := func(model string, u Usage) (float64, bool) {
		if u.In == 0 && u.Out == 0 && u.CacheRead == 0 && u.CacheWrite == 0 {
			return 0, true
		}
		if pricing.Anthropic(model) {
			return pricing.ClaudeCost(model, float64(u.In), float64(u.Out), float64(u.CacheWrite), 0, float64(u.CacheRead))
		}
		// Facts normalizes input to the uncached part for both CLIs.
		return pricing.OpenAICost(model, float64(u.In+u.CacheRead), float64(u.CacheRead), float64(u.Out))
	}
	if len(f.ByModel) == 0 {
		return price(f.Model, Usage{In: f.TokensIn, Out: f.TokensOut, CacheRead: f.CacheRead, CacheWrite: f.CacheWrite})
	}
	total, known := 0.0, true
	for model, usage := range f.ByModel {
		usd, ok := price(model, usage)
		total += usd
		known = known && ok
	}
	return total, known
}

// prepare puts the scenario's setup into the stand.
func prepare(s *Stand, sc *Scenario) error {
	for p, content := range sc.Setup.Files {
		dst := filepath.Join(s.Work, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return infra("%v", err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o600); err != nil {
			return infra("%v", err)
		}
	}
	for _, d := range sc.Setup.Workspace {
		if err := copyTree(filepath.Join(sc.Dir(), filepath.FromSlash(d)), s.Work); err != nil {
			return infra("workspace fixture %s: %v", d, err)
		}
	}
	if sc.Setup.Run != "" {
		if code, out := s.Exec(context.Background(), sc.Setup.Run, nil, 2*time.Minute); code != 0 {
			return infra("setup run: exit %d: %s", code, out)
		}
	}
	for _, d := range sc.Setup.Design {
		src := filepath.Join(sc.Dir(), filepath.FromSlash(d))
		if err := copyTree(src, filepath.Join(s.Library, filepath.Base(src))); err != nil {
			return infra("design fixture %s: %v", d, err)
		}
	}
	for _, d := range sc.Setup.Kits {
		if err := installKit(filepath.Join(sc.Dir(), filepath.FromSlash(d)), designkit.DefaultDir(s.Data)); err != nil {
			return infra("kit fixture %s: %v", d, err)
		}
	}
	if len(sc.Setup.Design) > 0 {
		// The library is read by the runtime's store; give it a moment to see the new projects.
		time.Sleep(2 * time.Second)
	}
	return nil
}

// installKit installs a kit folder as `design kit install` does a tarball: packed, then designkit.Install under the id and
// version its design-system.json names.
func installKit(src, dir string) error {
	raw, err := os.ReadFile(filepath.Join(src, "design-system.json"))
	if err != nil {
		return err
	}
	var ds struct {
		Kit struct{ ID, Version string } `json:"kit"`
	}
	if err := json.Unmarshal(raw, &ds); err != nil {
		return err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err = filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err = tw.Write(b)
		return err
	})
	if err == nil {
		err = tw.Close()
	}
	if err != nil {
		return err
	}
	_, err = designkit.Install(dir, ds.Kit.ID, ds.Kit.Version, &buf, designkit.Options{})
	return err
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(to, 0o700)
		}
		return copyFile(p, to)
	})
}

// ReadResults reads a results file.
func ReadResults(path string) ([]Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Result
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 256<<20)
	for sc.Scan() {
		var r Result
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Key != "" {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}
