// Package jobs is the hub's queue of heavy jobs and its pool of builder machines (docs/hub/builders.md): a job
// asks for a place with place(need); the pool answers with a machine and the reason, or with "wait" (every fitting
// builder is busy) or with "local" (no online builder fits, the submitter's own heavy slots take it, with draft limits).
package jobs

import (
	"fmt"
	"sort"
	"strings"
)

// Need is what a launch asks of its place. HRN-1671 widens it for chats (provider, Claude account, repository) and adds
// a kind to Kinds; the heavy kind below is the one the builders serve.
type Need struct {
	// Kind picks the rules of Kinds. "heavy" is a heavy command that wants a builder.
	Kind string
	// Caps are the labels the machine must carry (blender, gpu, ffmpeg, docker...).
	Caps []string
	// Machine is the person's explicit choice; "" is auto. An explicit machine passes the same filters.
	Machine string
}

// Candidate is a machine place() may choose, with what the hub knows of it now.
type Candidate struct {
	ID   string
	Name string
	// Role is store.RoleAgents or store.RoleBuilder.
	Role string
	// Online: its link is up. Offers: its agent says it runs jobs (the operator opted in there).
	Online, Offers bool
	Caps           []string
	// Slots is how many jobs it runs at once; Running how many it runs now.
	Slots, Running int
}

// Free is how many more jobs it takes now.
func (c Candidate) Free() int { return max(c.Slots-c.Running, 0) }

// Placement is the answer of Place.
type Placement struct {
	// Machine is the chosen candidate; nil with Wait or Local.
	Machine *Candidate
	// Wait: a fitting builder exists but has no free slot; the job stays queued.
	Wait bool
	// Local: no online builder fits; the submitter's own slots run it.
	Local bool
	// Reason says why, in words a person reads in the job's card.
	Reason string
}

// Rules are what a kind of need filters and ranks by.
type Rules struct {
	// Eligible says whether the machine may take this kind at all, and when not why (empty is "not of this kind").
	Eligible func(c Candidate, n Need) (ok bool, why string)
}

// Kinds are the kinds Place knows. HRN-1671 adds "chat" here.
var Kinds = map[string]Rules{
	"heavy": {Eligible: heavyEligible},
}

func heavyEligible(c Candidate, n Need) (bool, string) {
	switch {
	case c.Role != "builder":
		return false, ""
	case !c.Online:
		return false, "offline"
	case !c.Offers:
		return false, "its agent does not run jobs (AGENT_HUB_JOBS is not on)"
	}
	if miss := missing(n.Caps, c.Caps); len(miss) > 0 {
		return false, "has no " + strings.Join(miss, ", ")
	}
	return true, ""
}

func missing(need, have []string) []string {
	var out []string
	for _, n := range need {
		found := false
		for _, h := range have {
			if strings.EqualFold(n, h) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, n)
		}
	}
	return out
}

// Place decides where a need goes. Every answer carries its reason.
func Place(n Need, cands []Candidate) Placement {
	rules, ok := Kinds[n.Kind]
	if !ok {
		return Placement{Reason: fmt.Sprintf("no rules for %q", n.Kind), Local: n.Kind == ""}
	}
	var fit []Candidate
	var builders []Candidate
	why := map[string]string{}
	for _, c := range cands {
		if ok, w := rules.Eligible(c, n); ok {
			fit = append(fit, c)
		} else if w != "" {
			why[c.Name] = w
		}
		if c.Role == "builder" {
			builders = append(builders, c)
		}
	}
	if n.Machine != "" {
		return placeExplicit(n, cands, fit, why)
	}
	if len(fit) == 0 {
		return Placement{Local: true, Reason: noBuilder(n, builders, why)}
	}
	sort.SliceStable(fit, func(i, j int) bool {
		a, b := fit[i], fit[j]
		la, lb := load(a), load(b)
		switch {
		case la != lb:
			return la < lb
		case a.Slots != b.Slots:
			return a.Slots > b.Slots
		}
		return a.Name < b.Name
	})
	for i := range fit {
		if fit[i].Free() > 0 {
			c := fit[i]
			return Placement{Machine: &c, Reason: fmt.Sprintf("builder %s: %d of %d slots busy, %s", c.Name, c.Running, c.Slots, capsText(n, c))}
		}
	}
	return Placement{Wait: true, Reason: fmt.Sprintf("all %d fitting builders are busy (%s); the job waits in the queue", len(fit), names(fit))}
}

func placeExplicit(n Need, cands, fit []Candidate, why map[string]string) Placement {
	var asked *Candidate
	for i := range cands {
		if cands[i].ID == n.Machine || strings.EqualFold(cands[i].Name, n.Machine) {
			asked = &cands[i]
			break
		}
	}
	if asked == nil {
		return Placement{Local: true, Reason: fmt.Sprintf("the chosen builder %q is not in the pool; runs on this machine's own slots", n.Machine)}
	}
	for i := range fit {
		if fit[i].ID != asked.ID {
			continue
		}
		c := fit[i]
		if c.Free() == 0 {
			return Placement{Wait: true, Reason: fmt.Sprintf("the chosen builder %s is busy (%d of %d slots); the job waits for it", c.Name, c.Running, c.Slots)}
		}
		return Placement{Machine: &c, Reason: fmt.Sprintf("the chosen builder %s: %d of %d slots busy, %s", c.Name, c.Running, c.Slots, capsText(n, c))}
	}
	w := why[asked.Name]
	if w == "" {
		w = "is not a builder"
	}
	return Placement{Local: true, Reason: fmt.Sprintf("the chosen builder %s cannot take it (%s); runs on this machine's own slots", asked.Name, w)}
}

func load(c Candidate) float64 {
	if c.Slots <= 0 {
		return 1
	}
	return float64(c.Running) / float64(c.Slots)
}

func capsText(n Need, c Candidate) string {
	if len(n.Caps) == 0 {
		return "no labels asked"
	}
	return "has " + strings.Join(n.Caps, ", ")
}

func names(cs []Candidate) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return strings.Join(out, ", ")
}

func noBuilder(n Need, builders []Candidate, why map[string]string) string {
	if len(builders) == 0 {
		return "no builders in the pool; runs on this machine's own heavy slots with draft limits"
	}
	var parts []string
	for _, b := range builders {
		parts = append(parts, b.Name+" "+why[b.Name])
	}
	sort.Strings(parts)
	return "no online builder fits (" + strings.Join(parts, "; ") + "); runs on this machine's own heavy slots with draft limits"
}
