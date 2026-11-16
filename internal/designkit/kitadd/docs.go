package kitadd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// Sources of what a card says, in the order they are tried. Every card records which one gave each part, so a person
// knows what to check first: "jsdoc", "readme", "docs" (a doc page or a story the package ships) are the authors'
// own words; "draft" is written by a model from the package's docs and the component's props; "generated" is a
// plain sentence from the name and the props, when no model was there.
const (
	srcJSDoc     = "jsdoc"
	srcReadme    = "readme"
	srcDocs      = "docs"
	srcDraft     = "draft"
	srcGenerated = "generated"
)

// compDoc is what a component's card says about it.
type compDoc struct {
	Description string   `json:"description"`
	When        []string `json:"whenToUse"`
	WhenNot     []string `json:"whenNotToUse"`
	Keywords    []string `json:"keywords"`
	// DescSrc, WhenSrc: where the description and the two lists came from.
	DescSrc string `json:"-"`
	WhenSrc string `json:"-"`
}

const docsFileLimit = 4000

// gatherDocs finds, for every component of the kit, a description, when to use and when not, and keywords: from the
// JSDoc the types carry, from the package's README and doc pages and stories, then from a draft by a model for what is
// still empty, and last from the name and props. No component is left for a person to fill in.
func gatherDocs(o *Options, dir string, b *built, rep *Report) map[string]compDoc {
	out := map[string]compDoc{}
	pages := docPages(dir, b.pkg)
	readme := ""
	if raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(b.pkg), "README.md")); err == nil {
		readme = string(raw)
	}
	var want []genComp
	for _, c := range b.comps {
		d := compDoc{}
		if c.Description != "" {
			d.Description, d.DescSrc = cleanDoc(c.Description), srcJSDoc
		}
		for _, p := range pages {
			if !p.mentions(c.ID) {
				continue
			}
			if s, when, not := p.section(c.ID); s != "" || len(when)+len(not) > 0 {
				if d.Description == "" && s != "" {
					d.Description, d.DescSrc = s, srcDocs
				}
				if len(d.When) == 0 && len(when) > 0 {
					d.When, d.WhenSrc = when, srcDocs
				}
				if len(d.WhenNot) == 0 && len(not) > 0 {
					d.WhenNot, d.WhenSrc = not, srcDocs
				}
			}
		}
		if readme != "" && (d.Description == "" || len(d.When) == 0) {
			rp := docPage{text: readme}
			if s, when, not := rp.section(c.ID); s != "" || len(when)+len(not) > 0 {
				if d.Description == "" && s != "" {
					d.Description, d.DescSrc = s, srcReadme
				}
				if len(d.When) == 0 && len(when) > 0 {
					d.When, d.WhenSrc = when, srcReadme
				}
				if len(d.WhenNot) == 0 && len(not) > 0 {
					d.WhenNot, d.WhenSrc = not, srcReadme
				}
			}
		}
		out[c.ID] = d
		if d.Description == "" || len(d.When) == 0 || len(d.WhenNot) == 0 || len(d.Keywords) == 0 {
			want = append(want, c)
		}
	}
	if len(want) > 0 && (o.DraftOn || o.Draft != nil) && !o.NoDraft {
		drafted := draftDocs(o, b, readme, want, out)
		for id, dr := range drafted {
			d := out[id]
			if d.Description == "" && dr.Description != "" {
				d.Description, d.DescSrc = dr.Description, srcDraft
			}
			if len(d.When) == 0 && len(dr.When) > 0 {
				d.When, d.WhenSrc = dr.When, srcDraft
			}
			if len(d.WhenNot) == 0 && len(dr.WhenNot) > 0 {
				d.WhenNot = dr.WhenNot
				if d.WhenSrc == "" {
					d.WhenSrc = srcDraft
				}
			}
			if len(d.Keywords) == 0 {
				d.Keywords = dr.Keywords
			}
			out[id] = d
		}
		if len(drafted) == 0 {
			rep.NotCarried = append(rep.NotCarried, "cards: no model answered for the drafts (claude -p); the missing «When to use» texts are generated from names and props")
		}
	}
	// What no source gave: a plain sentence from the name and the props, marked as generated.
	names := make([]string, 0, len(b.comps))
	for _, c := range b.comps {
		names = append(names, c.ID)
	}
	for _, c := range b.comps {
		d := out[c.ID]
		g := generatedDoc(c, b.pkg, names)
		if d.Description == "" {
			d.Description, d.DescSrc = g.Description, srcGenerated
		}
		if len(d.When) == 0 {
			d.When = g.When
			if d.WhenSrc == "" {
				d.WhenSrc = srcGenerated
			}
		}
		if len(d.WhenNot) == 0 {
			d.WhenNot = g.WhenNot
			if d.WhenSrc == "" {
				d.WhenSrc = srcGenerated
			}
		}
		if len(d.Keywords) == 0 {
			d.Keywords = g.Keywords
		}
		out[c.ID] = d
	}
	return out
}

var (
	jsdocTagRe = regexp.MustCompile(`(?m)^\s*@\w+.*$`)
	spaceRe    = regexp.MustCompile(`\s+`)
	mdLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	htmlTagRe  = regexp.MustCompile(`<[^>]+>`)
)

// cleanDoc is a doc text as one plain sentence or two, without markup.
func cleanDoc(s string) string {
	s = jsdocTagRe.ReplaceAllString(s, "")
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("`", "", "**", "", "__", "").Replace(s)
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if strings.HasSuffix(s, ":") || len(s) < 8 {
		return "" // "Demos:" and the like: a heading, not a sentence
	}
	if len(s) > 280 {
		if i := strings.LastIndex(s[:280], ". "); i > 60 {
			s = s[:i+1]
		} else {
			s = strings.TrimSpace(s[:277]) + "…"
		}
	}
	return s
}

// docPage is a markdown or mdx page or a stories file of the package.
type docPage struct {
	name string
	text string
}

// docPages are the doc pages and stories a package ships (not those of what it depends on).
func docPages(dir, pkg string) []docPage {
	root := filepath.Join(dir, "node_modules", filepath.FromSlash(pkg))
	var pages []docPage
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		low := strings.ToLower(d.Name())
		isDoc := strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".mdx")
		isStory := strings.Contains(low, ".stories.")
		if !isDoc && !isStory {
			return nil
		}
		if low == "readme.md" && filepath.Dir(p) == root || strings.HasPrefix(low, "changelog") || strings.HasPrefix(low, "license") {
			return nil
		}
		if n++; n > docsFileLimit {
			return filepath.SkipAll
		}
		if info, err := d.Info(); err != nil || info.Size() > 400<<10 {
			return nil
		}
		if raw, err := os.ReadFile(p); err == nil {
			pages = append(pages, docPage{name: d.Name(), text: string(raw)})
		}
		return nil
	})
	return pages
}

func (p docPage) mentions(id string) bool {
	stem := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.SplitN(p.name, ".", 2)[0], ".md"), ".mdx"))
	return stem == strings.ToLower(id) || stem == kebabCase(id) || strings.Contains(p.text, "<"+id) && strings.Contains(strings.ToLower(p.name), strings.ToLower(id))
}

var kebabRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func kebabCase(s string) string { return strings.ToLower(kebabRe.ReplaceAllString(s, "$1-$2")) }

var (
	storyDescRe = regexp.MustCompile("(?s)description:\\s*\\{\\s*component:\\s*(?:'([^']{20,})'|\"([^\"]{20,})\"|`([^`]{20,})`)")
	headingRe2  = regexp.MustCompile(`(?m)^#{1,4}\s+(.+?)\s*$`)
	bulletRe    = regexp.MustCompile(`(?m)^\s*[-*]\s+(.+?)\s*$`)
)

// section is, for the component id, its description and its «when to use» and «when not to use» lists from this page: the
// story's docs description, the first paragraph under a heading that names the component (or of the whole page when
// the page is the component's), and the bullets under headings «When to use» / «Usage» and «When not to use» / «Don't».
func (p docPage) section(id string) (desc string, when, not []string) {
	if strings.Contains(p.name, ".stories.") {
		if m := storyDescRe.FindStringSubmatch(p.text); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return cleanDoc(g), nil, nil
				}
			}
		}
		return "", nil, nil
	}
	text := p.text
	own := p.mentions(id)
	body := ""
	if own {
		body = text
	} else {
		for _, h := range headingRe2.FindAllStringSubmatchIndex(text, -1) {
			if strings.EqualFold(strings.Trim(text[h[2]:h[3]], "`<>/ "), id) {
				rest := text[h[1]:]
				if next := headingRe2.FindStringIndex(rest); next != nil && strings.HasPrefix(strings.TrimSpace(text[h[0]:h[1]]), "# ") {
					rest = rest[:next[0]]
				}
				body = rest
				break
			}
		}
	}
	if body == "" {
		return "", nil, nil
	}
	desc = firstParagraph(body)
	when = bulletsUnder(body, `(?i)when\s+to\s+use|use\s+cases?|usage|do\b`)
	not = bulletsUnder(body, `(?i)when\s+not\s+to\s+use|don'?t|do\s+not|avoid|alternatives`)
	return desc, when, not
}

func firstParagraph(s string) string {
	s = regexp.MustCompile("(?s)^---.*?---").ReplaceAllString(strings.TrimSpace(s), "")
	s = regexp.MustCompile("(?s)```.*?```").ReplaceAllString(s, "")
	for _, para := range strings.Split(s, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" || strings.HasPrefix(para, "#") || strings.HasPrefix(para, "import ") || strings.HasPrefix(para, "export ") ||
			strings.HasPrefix(para, "<") || strings.HasPrefix(para, "|") || strings.HasPrefix(para, "-") || strings.HasPrefix(para, "*") || strings.HasPrefix(para, "!") || strings.HasPrefix(para, ">") {
			continue
		}
		if c := cleanDoc(para); len(c) >= 20 {
			return c
		}
	}
	return ""
}

func bulletsUnder(body, headingPattern string) []string {
	re := regexp.MustCompile(headingPattern)
	hs := headingRe2.FindAllStringSubmatchIndex(body, -1)
	for i, h := range hs {
		if !re.MatchString(body[h[2]:h[3]]) {
			continue
		}
		end := len(body)
		if i+1 < len(hs) {
			end = hs[i+1][0]
		}
		var out []string
		for _, m := range bulletRe.FindAllStringSubmatch(body[h[1]:end], -1) {
			if t := cleanDoc(m[1]); t != "" && len(out) < 5 {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// ---- drafts by a model ----

const draftChunk = 40

type draftDoc struct {
	Description string   `json:"description"`
	When        []string `json:"whenToUse"`
	WhenNot     []string `json:"whenNotToUse"`
	Keywords    []string `json:"keywords"`
}

// draftDocs asks a model (claude -p, sonnet) for the texts the package's own docs left out, 40 components a call.
// What it writes is a draft: the card says so.
func draftDocs(o *Options, b *built, readme string, want []genComp, have map[string]compDoc) map[string]draftDoc {
	ask := o.Draft
	if ask == nil {
		ask = claudeDraft
	}
	siblings := make([]string, 0, len(b.comps))
	for _, c := range b.comps {
		if c.Part == "" {
			siblings = append(siblings, c.ID)
		}
	}
	if len(readme) > 3000 {
		readme = readme[:3000]
	}
	out := map[string]draftDoc{}
	for start := 0; start < len(want); start += draftChunk {
		chunk := want[start:min(start+draftChunk, len(want))]
		var sb strings.Builder
		fmt.Fprintf(&sb, "You write short documentation for the components of the npm package %s %s, for a design tool's component cards.\n", b.pkg, b.version)
		sb.WriteString("Answer with one JSON object and nothing else: {\"<ComponentName>\": {\"description\": \"one sentence: what it is for\", \"whenToUse\": [\"up to 3 short situations\"], \"whenNotToUse\": [\"up to 3 short situations, each naming the component to take instead when one of the list fits\"], \"keywords\": [\"up to 6 words a person would search by\"]}}.\n")
		sb.WriteString("Write in English, plain words, no markdown. Do not invent props. Use what is given; where it says little, say what such a component is generally for.\n\n")
		if readme != "" {
			sb.WriteString("The package's README (start):\n" + readme + "\n\n")
		}
		sb.WriteString("Components of the package (for \"take X instead\"): " + strings.Join(siblings, ", ") + "\n\nWrite for these:\n")
		for _, c := range chunk {
			fmt.Fprintf(&sb, "- %s", c.ID)
			if c.Part != "" {
				fmt.Fprintf(&sb, " (a part of %s)", c.Part)
			}
			if d := have[c.ID].Description; d != "" {
				fmt.Fprintf(&sb, ": %s", d)
			}
			var ps []string
			for i, p := range c.Props {
				if i == 12 {
					break
				}
				ps = append(ps, p.Name+": "+p.Type)
			}
			if len(ps) > 0 {
				fmt.Fprintf(&sb, " [props: %s]", strings.Join(ps, "; "))
			}
			sb.WriteString("\n")
		}
		answer, err := ask(sb.String())
		if err != nil {
			o.logf("cards: the draft call failed: %v\n", firstLine(err.Error()))
			continue
		}
		i, j := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
		if i < 0 || j <= i {
			continue
		}
		var got map[string]draftDoc
		if json.Unmarshal([]byte(answer[i:j+1]), &got) != nil {
			continue
		}
		for _, c := range chunk {
			if d, ok := got[c.ID]; ok {
				d.Description = cleanDoc(d.Description)
				d.When, d.WhenNot, d.Keywords = cleanList(d.When, 3), cleanList(d.WhenNot, 3), cleanList(d.Keywords, 6)
				out[c.ID] = d
			}
		}
	}
	return out
}

func cleanList(in []string, max int) []string {
	var out []string
	for _, s := range in {
		if s = cleanDoc(s); s != "" && len(out) < max {
			out = append(out, s)
		}
	}
	return out
}

// claudeDraft runs `claude -p` with the sonnet model on the prompt. TMUX is dropped: a nested claude must not touch the
// chat it runs in.
func claudeDraft(prompt string) (string, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude is not on this machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-p", "--model", "sonnet", "--output-format", "text", "--tools", "")
	cmd.Stdin = strings.NewReader(prompt)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TMUX=") && !strings.HasPrefix(e, "TMUX_PANE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ---- the plain sentence ----

var (
	layoutRe = regexp.MustCompile(`^(Flex|Space|Stack|HStack|VStack|Group|Inline|Row|Col|Column|Grid|SimpleGrid|GridItem|Container|Center|Box|Layout|Wrap|WrapItem|Cluster|Spacer|Divider|Separator|AspectRatio|Masonry|Splitter)$`)
	textRe   = regexp.MustCompile(`^(Typography|Text|Title|Heading|Paragraph|Blockquote|Highlight|Mark|Code|Kbd|Link|Anchor|Label)$`)
)

var camelRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func words(id string) string { return strings.ToLower(camelRe.ReplaceAllString(id, "$1 $2")) }

// generatedDoc is what a card says when nothing else does: made from the name, the props and the neighbours' names.
func generatedDoc(c genComp, pkg string, all []string) compDoc {
	w := words(c.ID)
	d := compDoc{Keywords: strings.Fields(w)}
	if c.Part == designkit.PartProvider {
		d.Description = fmt.Sprintf("%s from %s: gives the components inside it their theme and settings.", c.ID, pkg)
		d.When = []string{"Once, around the whole application or board, so every component inside sees the same theme."}
		d.WhenNot = []string{"Nested to change one component's look: pass that component's own props or a local theme instead."}
		return d
	}
	if c.Part != "" && c.Part != designkit.PartProvider {
		d.Description = fmt.Sprintf("The %s of %s from %s.", w, words(c.Part), pkg)
		d.When = []string{fmt.Sprintf("Inside %s, where its content needs a %s.", c.Part, w)}
		d.WhenNot = []string{fmt.Sprintf("Alone: a %s is drawn inside %s, never by itself.", w, c.Part)}
		return d
	}
	if layoutRe.MatchString(c.ID) {
		d.Description = fmt.Sprintf("%s from %s: lays other components out in a row, a column or a grid with gaps from the theme.", c.ID, pkg)
		d.When = []string{
			"Every row, column, gap and aligned group on a screen: arrange components with it, not with a raw div and flex or margin styles.",
			"Spacing between components: set it here (gap, align, justify) so it follows the theme.",
		}
		d.WhenNot = []string{"A single component with nothing beside it: no wrapper is needed. Page-level structure (header, sidebar, content): take the package's layout shell if it has one."}
		d.Keywords = append(d.Keywords, "layout", "row", "column", "gap", "grid", "stack", "spacing")
		return d
	}
	if textRe.MatchString(c.ID) {
		d.Description = fmt.Sprintf("%s from %s: text on a screen in the theme's fonts, sizes and colours.", c.ID, pkg)
		d.When = []string{
			"Every heading, paragraph, label, caption and link on a screen: write text with it, not with a raw h1, p or span with font styles.",
			"A text in the theme's colour roles (secondary, success, danger) or truncated to some lines.",
		}
		d.WhenNot = []string{"Text inside a component that already draws its own (a button's label, a card's title prop): pass the text to that component."}
		d.Keywords = append(d.Keywords, "text", "heading", "title", "paragraph", "typography", "font")
		return d
	}
	d.Description = fmt.Sprintf("%s from %s.", strings.ToUpper(w[:1])+w[1:], pkg)
	var kinds []string
	for _, p := range c.Props {
		if p.Type == "enum" && len(p.Values) > 1 && len(kinds) < 2 {
			kinds = append(kinds, fmt.Sprintf("%s (%s)", p.Name, strings.Join(p.Values[:min(len(p.Values), 4)], ", ")))
		}
	}
	d.When = []string{fmt.Sprintf("The screen needs a %s.", w)}
	if len(kinds) > 0 {
		d.When = append(d.When, "Its look is set by "+strings.Join(kinds, " and ")+".")
	}
	if sib := nearestSibling(c.ID, all); sib != "" {
		d.WhenNot = []string{fmt.Sprintf("A %s fits the job better: take %s instead.", words(sib), sib)}
	} else {
		d.WhenNot = []string{fmt.Sprintf("The package has a component made for the job: look in its list before using %s.", c.ID)}
	}
	return d
}

// nearestSibling is the other component whose name shares the most with id: a Button's is IconButton.
func nearestSibling(id string, all []string) string {
	best, bestN := "", 0
	for _, o := range all {
		if o == id || strings.HasPrefix(o, id) && len(o) > len(id)+12 {
			continue
		}
		n := 0
		switch {
		case strings.HasSuffix(o, id) || strings.HasSuffix(id, o):
			n = min(len(o), len(id))
		case strings.HasPrefix(o, id) || strings.HasPrefix(id, o):
			n = min(len(o), len(id)) - 1
		}
		if n > bestN {
			best, bestN = o, n
		}
	}
	if bestN < 3 {
		return ""
	}
	return best
}

var _ = sort.Strings
