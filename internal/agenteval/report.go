package agenteval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// CellStat is one scenario × model of a report.
type CellStat struct {
	Scenario string  `json:"scenario"`
	Set      string  `json:"set"`
	Model    string  `json:"model"`
	Pass     int     `json:"pass"`
	Runs     int     `json:"runs"`
	Infra    int     `json:"infra"`
	Lo       float64 `json:"lo"`
	Hi       float64 `json:"hi"`
	PassHat3 float64 `json:"passHat3,omitempty"`
	USD      float64 `json:"usd"`
	// Change is against the same cell of the earlier run: better, worse, same, new.
	Change string `json:"change,omitempty"`
	Before string `json:"before,omitempty"`
}

// Report is a run summed up.
type Report struct {
	Setup  string     `json:"setup"`
	Cells  []CellStat `json:"cells"`
	Models []string   `json:"models"`
	Runs   int        `json:"runs"`
	Infra  int        `json:"infra"`
	USD    float64    `json:"usd"`
	// Failures are the failed runs, each with its first failed expectation.
	Failures []Result `json:"-"`
}

// Summarize sums up results; against are an earlier run's, for the change column (nil: none).
func Summarize(results, against []Result, noise float64) Report {
	type k struct{ s, m string }
	cells := map[k]*CellStat{}
	models := map[string]bool{}
	var rep Report
	for _, r := range results {
		c := cells[k{r.Scenario, r.Model}]
		if c == nil {
			c = &CellStat{Scenario: r.Scenario, Set: r.Set, Model: r.Model}
			cells[k{r.Scenario, r.Model}] = c
		}
		models[r.Model] = true
		rep.Setup = r.Setup
		rep.USD += r.USD
		c.USD += r.USD
		switch r.State {
		case Infra:
			c.Infra++
			rep.Infra++
			continue
		case Pass:
			c.Pass++
		case Fail:
			rep.Failures = append(rep.Failures, r)
		}
		c.Runs++
		rep.Runs++
	}
	before := map[k][2]int{}
	for _, r := range against {
		if r.State == Infra {
			continue
		}
		b := before[k{r.Scenario, r.Model}]
		if r.State == Pass {
			b[0]++
		}
		b[1]++
		before[k{r.Scenario, r.Model}] = b
	}
	for key, c := range cells {
		c.Lo, c.Hi = Wilson(c.Pass, c.Runs)
		if c.Runs >= 3 {
			c.PassHat3 = PassHatK(c.Pass, c.Runs, 3)
		}
		if against != nil {
			b := before[key]
			c.Change = Change(b[0], b[1], c.Pass, c.Runs, noise)
			if b[1] > 0 {
				c.Before = fmt.Sprintf("%d/%d", b[0], b[1])
			}
		}
		rep.Cells = append(rep.Cells, *c)
	}
	sort.Slice(rep.Cells, func(i, j int) bool {
		a, b := rep.Cells[i], rep.Cells[j]
		if a.Set != b.Set {
			return a.Set < b.Set
		}
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		return a.Model < b.Model
	})
	for m := range models {
		rep.Models = append(rep.Models, m)
	}
	sort.Strings(rep.Models)
	return rep
}

func pct(x float64) string { return fmt.Sprintf("%.0f", math.Round(x*100)) }

var changeMark = map[string]string{"better": " ▲", "worse": " ▼", "new": " (new)"}

// Markdown is the report as a page: the table scenario × model, then the failures.
func (rep Report) Markdown(title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Setup `%s` · %d runs judged, %d infrastructure failures not counted · $%.2f\n\n", rep.Setup, rep.Runs, rep.Infra, rep.USD)
	b.WriteString("Each cell: passed/runs, 95% Wilson interval in brackets; ▲/▼ a change beyond noise against the earlier run.\n\n")
	b.WriteString("| Scenario |")
	for _, m := range rep.Models {
		b.WriteString(" " + m + " |")
	}
	b.WriteString("\n|---|")
	for range rep.Models {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	byRow := map[string]map[string]CellStat{}
	var rows []string
	for _, c := range rep.Cells {
		if byRow[c.Scenario] == nil {
			byRow[c.Scenario] = map[string]CellStat{}
			rows = append(rows, c.Scenario)
		}
		byRow[c.Scenario][c.Model] = c
	}
	for _, s := range rows {
		name := s
		for _, c := range byRow[s] {
			if c.Set == SetHeldOut {
				name += " (held out)"
			}
			break
		}
		b.WriteString("| " + name + " |")
		for _, m := range rep.Models {
			c, ok := byRow[s][m]
			if !ok || c.Runs == 0 {
				if ok && c.Infra > 0 {
					fmt.Fprintf(&b, " — (%d infra) |", c.Infra)
				} else {
					b.WriteString(" · |")
				}
				continue
			}
			fmt.Fprintf(&b, " %d/%d [%s–%s]%s |", c.Pass, c.Runs, pct(c.Lo), pct(c.Hi), changeMark[c.Change])
		}
		b.WriteString("\n")
	}
	totals := map[string][2]int{}
	for _, c := range rep.Cells {
		t := totals[c.Model]
		t[0] += c.Pass
		t[1] += c.Runs
		totals[c.Model] = t
	}
	b.WriteString("| **all** |")
	for _, m := range rep.Models {
		t := totals[m]
		fmt.Fprintf(&b, " **%d/%d** |", t[0], t[1])
	}
	b.WriteString("\n")
	if len(rep.Failures) > 0 {
		b.WriteString("\n## Failures\n\n")
		for _, r := range rep.Failures {
			fmt.Fprintf(&b, "- **%s** · %s · %s #%d — %s (`%s`)\n", r.Scenario, r.Model, r.Variant, r.Rep, failText(r), r.Transcript)
		}
	}
	return b.String()
}

func failText(r Result) string {
	var parts []string
	for _, v := range r.Verdicts {
		if !v.OK {
			parts = append(parts, v.Name+": "+clip(v.Why, 220))
		}
	}
	return strings.ReplaceAll(strings.Join(parts, "; "), "\n", " ")
}
