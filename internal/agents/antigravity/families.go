package antigravity

import (
	"regexp"
	"slices"
	"strings"
)

// Family is a model with the thinking levels it runs at: what the picker offers as a model and its efforts, and what
// the ACP session sets as its «model» and «thought_level» options.
type Family struct {
	ID, Label     string
	Efforts       []string
	DefaultEffort string
}

var (
	effortID    = regexp.MustCompile(`^(.+)-(low|medium|high|xhigh)$`)
	effortLabel = regexp.MustCompile(`^(.+?)\s*\((?i:low|medium|high|xhigh|extra)\)$`)
	effortOrder = []string{"low", "medium", "high", "xhigh"}
)

// Families groups the models `agy models` lists (ids with their thinking level in them: gemini-4-neon-high) by
// family. A family listed at one level only offers no level to pick: the model
// runs at it.
func Families(models []Model) []Family {
	var out []Family
	index := map[string]int{}
	for _, m := range models {
		id, label, effort := m.ID, m.Label, ""
		if g := effortID.FindStringSubmatch(m.ID); g != nil {
			id, effort = g[1], g[2]
			if l := effortLabel.FindStringSubmatch(m.Label); l != nil {
				label = l[1]
			}
		}
		i, ok := index[id]
		if !ok {
			i = len(out)
			index[id] = i
			out = append(out, Family{ID: id, Label: label})
		}
		if effort != "" && !slices.Contains(out[i].Efforts, effort) {
			out[i].Efforts = append(out[i].Efforts, effort)
		}
	}
	for i := range out {
		f := &out[i]
		if len(f.Efforts) < 2 {
			if len(f.Efforts) == 1 {
				// Its one level is part of its name: the session knows it by the whole id.
				f.ID += "-" + f.Efforts[0]
				f.Label = strings.TrimSpace(f.Label)
			}
			f.Efforts = nil
			continue
		}
		slices.SortFunc(f.Efforts, func(a, b string) int { return slices.Index(effortOrder, a) - slices.Index(effortOrder, b) })
		f.DefaultEffort = f.Efforts[len(f.Efforts)-1]
		if slices.Contains(f.Efforts, "high") {
			f.DefaultEffort = "high"
		}
	}
	return out
}
