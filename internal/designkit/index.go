package designkit

// The search of a kit: one index the kit screen (⌘K) and an agent (`harnessly design kit search`) look through, so
// both find the same things. An entry is a node of the canvas with the words it is found by: its name, description,
// purpose, the samples of its slots, a colour's values, a layout's number. The screen filters the same entries with the
// same rule (web: kit-search.ts).

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// The kinds of index entries, in the order the results are grouped.
const (
	KindComponent = "component"
	KindLayout    = "layout"
	KindTemplate  = "template"
	KindAsset     = "asset"
	KindColor     = "color"
	KindType      = "type"
	KindToken     = "token"
)

// IndexEntry is a node of the canvas the search finds.
type IndexEntry struct {
	Node  string `json:"node"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Path is where the node is, for a result's second line: «Шаблоны › Презентация › Обложки».
	Path string `json:"path"`
	// Words is what it is found by, lower case, separated by spaces.
	Words string `json:"words"`
}

var digitsRe = regexp.MustCompile(`\d+`)

func words(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p)
	}
	return b.String()
}

func buildIndex(c *Canvas) []IndexEntry {
	var out []IndexEntry
	for _, g := range c.Groups {
		for _, k := range g.Components {
			var props []string
			for _, p := range k.Props {
				props = append(props, p.Name)
			}
			out = append(out, IndexEntry{Node: k.NodeID, Kind: KindComponent, Title: k.Name, Path: g.Title,
				Words: words(k.Name, k.ID, k.Description, g.Title, strings.Join(props, " "))})
		}
	}
	for _, t := range c.Templates {
		out = append(out, IndexEntry{Node: t.NodeID, Kind: KindTemplate, Title: t.Title, Words: words(t.Title, t.ID, t.Description)})
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				var samples []string
				for _, sl := range l.Slots {
					samples = append(samples, sl.Sample)
				}
				// A layout is found by its number too: «s05», «5», «05».
				nums := strings.Join(digitsRe.FindAllString(l.ID, -1), " ")
				for _, n := range digitsRe.FindAllString(l.ID, -1) {
					if t := strings.TrimLeft(n, "0"); t != n && t != "" {
						nums += " " + t
					}
				}
				out = append(out, IndexEntry{Node: l.NodeID, Kind: KindLayout, Title: l.Name, Path: t.Title + " › " + s.Title,
					Words: words(l.Name, l.ID, nums, l.Purpose, s.Title, t.Title, strings.Join(samples, " "))})
			}
		}
	}
	for _, g := range c.Assets {
		for _, a := range g.Items {
			out = append(out, IndexEntry{Node: a.NodeID, Kind: KindAsset, Title: a.Name, Path: g.Title,
				Words: words(a.Name, strings.ReplaceAll(a.Name, "-", " "), g.Title)})
		}
	}
	for _, col := range c.Colors {
		modes := make([]string, 0, len(col.Values))
		for m := range col.Values {
			modes = append(modes, m)
		}
		sort.Strings(modes)
		var vals []string
		for _, m := range modes {
			vals = append(vals, col.Values[m], strings.TrimPrefix(col.Values[m], "#"))
		}
		out = append(out, IndexEntry{Node: col.ID, Kind: KindColor, Title: col.Name, Path: strings.Join(vals, " "),
			Words: words(col.Name, strings.Join(vals, " "), col.Usage)})
	}
	for _, s := range c.Type {
		out = append(out, IndexEntry{Node: s.ID, Kind: KindType, Title: s.Group + " · " + s.Name, Path: s.Size,
			Words: words(s.Group, s.Name, s.Family, s.Size)})
	}
	for _, t := range c.Tokens {
		out = append(out, IndexEntry{Node: t.ID, Kind: KindToken, Title: t.Name, Path: t.Value, Words: words(t.Name, t.Kind, t.Value, t.Usage)})
	}
	return out
}

// queryTerms splits a query into lower-case terms; a leading # of a colour stays out.
func queryTerms(q string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
		if f = strings.TrimPrefix(f, "#"); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// score is how well an entry answers the terms: 0 when a term is missing; a title that starts with the first term
// ranks above one that holds it, and that above words that hold it.
func score(e IndexEntry, terms []string) int {
	title := strings.ToLower(e.Title)
	s := 1
	for i, t := range terms {
		if !strings.Contains(e.Words, t) && !strings.Contains(title, t) {
			return 0
		}
		if i == 0 {
			switch {
			case title == t:
				s += 4
			case strings.HasPrefix(title, t):
				s += 3
			case strings.Contains(title, t):
				s += 2
			}
		}
	}
	return s
}

// Search answers a query over a canvas's index, best first, at most limit (all when limit <= 0).
func Search(index []IndexEntry, q string, limit int) []IndexEntry {
	terms := queryTerms(q)
	if len(terms) == 0 {
		return nil
	}
	type hit struct {
		e IndexEntry
		s int
		i int
	}
	var hits []hit
	for i, e := range index {
		if s := score(e, terms); s > 0 {
			hits = append(hits, hit{e, s, i})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].s != hits[b].s {
			return hits[a].s > hits[b].s
		}
		return hits[a].i < hits[b].i
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]IndexEntry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}
