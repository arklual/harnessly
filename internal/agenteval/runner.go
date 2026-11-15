package agenteval

import (
	"bufio"
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

	"github.com/ly-solutions/harnessly/internal/pricing"
)

// Model is one column of the matrix: an agent CLI and the model it runs.
type Model struct {
	// Key names the column in the report ("haiku", "codex:gpt-5.6-sol").
	Key   string `json:"key"`
	Agent string `json:"agent"`
	Model string `json:"model"`
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
	h := sha256.Sum256([]byte(strings.Join([]string{c.Scenario.ID, c.Scenario.Fingerprint(), c.Variant.ID, c.Variant.Text, c.Model.Key,
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
	Key        string    `json:"key"`
	Scenario   string    `json:"scenario"`
	Set        string    `json:"set"`
	Variant    string    `json:"variant"`
	Prompt     string    `json:"prompt"`
	Model      string    `json:"model"`
	Rep        int       `json:"rep"`
	Setup      string    `json:"setup"`
	State      string    `json:"state"`
	Error      string    `json:"error,omitempty"`
	Verdicts   []Verdict `json:"verdicts,omitempty"`
	Facts      *Facts    `json:"facts,omitempty"`
	TurnState  string    `json:"turnState,omitempty"`
	WallS      int       `json:"wallS"`
	USD        float64   `json:"usd"`
	Transcript string    `json:"transcript,omitempty"`
	StartedAt  string    `json:"startedAt"`
	Reused     bool      `json:"reused,omitempty"`
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
				logf("%-4s %s · %s · %s #%d · %ds $%.3f %s", r.State, c.Scenario.ID, c.Model.Key, c.Variant.ID, c.Rep, r.WallS, r.USD, firstFail(r))
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
	return results, ctx.Err()
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
		Rep: c.Rep, Setup: o.Setup, StartedAt: t0.UTC().Format(time.RFC3339)}
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
	tab, err := stand.Chat(c.Model.Agent, c.Model.Model, c.Variant.Text)
	if err != nil {
		return finish(Infra, err)
	}
	r.TurnState = stand.Wait(ctx, tab, time.Duration(c.Scenario.Timeout())*time.Second)
	keep := filepath.Join(o.Out, "transcripts", key+".jsonl")
	facts, err := stand.Collect(c.Model.Agent, tab, keep)
	if err != nil {
		return finish(Infra, err)
	}
	r.Transcript = filepath.Join("transcripts", key+".jsonl")
	r.Facts = &facts
	r.USD = cost(facts)
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
	}
	if Passed(r.Verdicts) {
		return finish(Pass, nil)
	}
	return finish(Fail, nil)
}

func cost(f Facts) float64 {
	if f.Model == "" || !pricing.Anthropic(f.Model) {
		return 0
	}
	return pricing.ClaudeCostByFullID(f.Model, float64(f.TokensIn), float64(f.TokensOut), float64(f.CacheWrite), 0, float64(f.CacheRead))
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
	for _, d := range sc.Setup.Design {
		src := filepath.Join(sc.Dir(), filepath.FromSlash(d))
		if err := copyTree(src, filepath.Join(s.Library, filepath.Base(src))); err != nil {
			return infra("design fixture %s: %v", d, err)
		}
	}
	if len(sc.Setup.Design) > 0 {
		// The library is read by the runtime's store; give it a moment to see the new projects.
		time.Sleep(2 * time.Second)
	}
	return nil
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
