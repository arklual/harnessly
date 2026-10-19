package background

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"harnessly/internal/jscompat"
)

// Counts is what the agent's own footer says is running (`· 2 shells, 1 monitor ·`); its `← N agent` counts peers,
// not background work.
type Counts struct{ Shell, Monitor int }

var (
	footerLike   = regexp.MustCompile(`bypass permissions|accept edits|plan mode|shift\+tab|\? for shortcuts|\d+ shells?\b|\d+ monitors?\b`)
	shellCount   = regexp.MustCompile(`(\d+) shells?\b`)
	monitorCount = regexp.MustCompile(`(\d+) monitors?\b`)
)

// ParseFooter reads the footer from a plain capture of the pane; nil when the last lines are not one.
func ParseFooter(pane string) *Counts {
	var lines []string
	for _, l := range strings.Split(pane, "\n") {
		if l = jscompat.Trim(l); l != "" {
			lines = append(lines, l)
		}
	}
	footer := strings.Join(lines[max(0, len(lines)-3):], " ")
	if !footerLike.MatchString(footer) {
		return nil
	}
	count := func(re *regexp.Regexp) int {
		if m := re.FindStringSubmatch(footer); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return 0
	}
	return &Counts{Shell: count(shellCount), Monitor: count(monitorCount)}
}

// Reconcile keeps the newest transcript tasks of each kind, as many as the footer says run, and pads with unnamed
// ones up to its counts. Without a footer the transcript list stands.
func Reconcile(tasks []Task, counts *Counts) []Task {
	if counts == nil {
		return tasks
	}
	pick := func(kind string, n int) []Task {
		var of []Task
		for _, t := range tasks {
			if t.Kind == kind {
				of = append(of, t)
			}
		}
		sort.SliceStable(of, func(i, j int) bool { return of[i].StartedAt > of[j].StartedAt })
		return of[:min(n, len(of))]
	}
	out := append(pick(Shell, counts.Shell), pick(Monitor, counts.Monitor)...)
	labelled := map[string]int{}
	for _, t := range out {
		labelled[t.Kind]++
	}
	for _, k := range []struct {
		kind string
		n    int
	}{{Shell, counts.Shell}, {Monitor, counts.Monitor}} {
		for i := 0; i < k.n-labelled[k.kind]; i++ {
			out = append(out, Task{ID: k.kind + "-" + strconv.Itoa(i), Kind: k.kind, Label: "", StartedAt: 0})
		}
	}
	for _, t := range tasks {
		if t.Kind == Agent || t.Kind == Wakeup {
			out = append(out, t)
		}
	}
	if out == nil {
		out = []Task{}
	}
	return out
}
