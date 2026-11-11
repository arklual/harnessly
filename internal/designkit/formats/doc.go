// Package formats moves a kit's tokens, descriptions and component drafts to and from other people's formats:
// W3C Design Tokens (DTCG), CSS variables, Tailwind, DESIGN.md, a built Storybook, a Figma variables export.
//
// The kit's own format stays the only one inside (design-system.json v3, tokens.json as lists, component cards): the
// other formats exist at the border, in this package, and nothing else reads them. Every direction returns a Report
// that names what it carried and what it could not, so nothing is lost without a word.
package formats

import "strings"

// Report is what one conversion did. NotCarried is what the other format has no place for (or the kit has none for);
// ToDo is what an import leaves for a person to write before `kit check` is clean.
type Report struct {
	Carried    []string `json:"carried"`
	NotCarried []string `json:"notCarried"`
	ToDo       []string `json:"todo"`
	Wrote      []string `json:"wrote,omitempty"`
}

func (r *Report) carried(format string, args ...any) { r.Carried = appendf(r.Carried, format, args...) }
func (r *Report) notCarried(format string, args ...any) {
	r.NotCarried = appendf(r.NotCarried, format, args...)
}
func (r *Report) todo(format string, args ...any) { r.ToDo = appendf(r.ToDo, format, args...) }

// Lines renders the report for a terminal.
func (r *Report) Lines() string {
	var b strings.Builder
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString(title + "\n")
		for _, it := range items {
			b.WriteString("  - " + it + "\n")
		}
	}
	section("carried:", r.Carried)
	section("not carried (the other format or the kit has no place for it):", r.NotCarried)
	section("to write before `kit check` is clean:", r.ToDo)
	section("wrote:", r.Wrote)
	return b.String()
}
