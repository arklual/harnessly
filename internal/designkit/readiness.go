package designkit

// Readiness (K1): how far each component of a kit is built out, a report over what `kit check` already reads.
// Nothing here is a problem: only the files and fields that exist count, and a kit that stops at level 0 is a kit.
//
//	0  the component has code {import, export} (a preset takes its target's)
//	1  the kit has tokens.json
//	2  the component has a sample or variants, and its card an example (a part has no sample of its own)
//	3  the component has a card with «When not to use»
//
// A level counts when every step up to it holds; -1 is a component with no code yet. A section the kit lists in
// "omitted" (tokens for 1, examples for 2, docs for 3) holds by itself: it is a choice, not a gap. Patterns and
// templates may be omitted too, they have no level.

import (
	"path"
	"sort"
)

// TopLevel is the highest readiness level.
const TopLevel = 3

// ComponentReadiness is the level of one component and what it lacks for the next one.
type ComponentReadiness struct {
	Name string `json:"name"`
	// Theme is the theme folder when the kit has several, "" for one.
	Theme string `json:"theme,omitempty"`
	// Of is the component a preset expands to.
	Of    string `json:"of,omitempty"`
	Level int    `json:"level"`
	// Next is what is missing for level+1, "" at the top.
	Next string `json:"next"`
}

// compFacts is what the check saw of a component, for the readiness.
type compFacts struct {
	code, isPart, isPreset         bool
	preset                         string
	sample, card, example, whenNot bool
}

func (k *checker) readiness(folder string, omitted map[string]bool) {
	tokens := k.exists(path.Join(folder, "tokens.json"))
	level := func(f *compFacts) (int, string) {
		if !f.code {
			return -1, "level 0: code {import, export}"
		}
		if !tokens && !omitted["tokens"] {
			return 0, "level 1: tokens.json"
		}
		if !omitted["examples"] && !((f.sample || f.isPart) && f.example) {
			return 1, "level 2: a sample (or variants) and an example in the card"
		}
		if !omitted["docs"] && !f.whenNot {
			return 2, "level 3: a card with «When not to use»"
		}
		return TopLevel, ""
	}
	ids := make([]string, 0, len(k.cur))
	for id := range k.cur {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := k.cur[id]
		g := f
		if f.isPreset {
			t, ok := k.cur[f.preset]
			if !ok {
				continue // a broken preset is already an error
			}
			g = t
		}
		lv, next := level(g)
		r := ComponentReadiness{Name: id, Of: f.preset, Level: lv, Next: next}
		if k.themes > 1 {
			r.Theme = folder
		}
		k.rep.Readiness = append(k.rep.Readiness, r)
	}
	k.rep.Level, k.rep.Next = -1, ""
	lowest := TopLevel + 1
	for _, r := range k.rep.Readiness {
		if r.Level < lowest {
			lowest, k.rep.Level, k.rep.Next = r.Level, r.Level, r.Next
		}
	}
}
