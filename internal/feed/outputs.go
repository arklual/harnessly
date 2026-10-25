package feed

import (
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What a turn made that the person opens (wire.TurnOutput), read off its calls: a page the Artifact tool or
// the artifacts CLI published, a design of the library the agent wrote into, a document of the repository, a merge
// request a command opened, a stand. A plain file is not one: it stays in the turn's files.

// outputTitleMax is the longest title a card gets, in runes.
const outputTitleMax = 120

var (
	// A merge request on GitLab, a pull request on GitHub.
	mrLinkRe = regexp.MustCompile(`^https?://[^/]+/.+?/(?:-/)?(?:merge_requests|pull)/\d+$`)
	// A claude.ai artifact; a page of the team's catalogue is told by its host (artifactLink).
	claudeArtifactRe = regexp.MustCompile(`^https?://claude\.ai/(?:code/)?artifact/`)
	// The artifacts CLI publishing a page: `<name>-artifact publish …`, `harnessly artifact publish …`.
	artifactCmdRe = regexp.MustCompile(`(?:^|[\s;&|(/-])artifacts?\s+publish\b`)
	// The name of one feature stand under the stands' domain.
	standNameRe = regexp.MustCompile(`^[\w-]+$`)
	// A dev server the agent shows: `harnessly open http://localhost:5173`.
	openCmdRe = regexp.MustCompile(`(?:^|[\s;&|(])harnessly\s+open\s+['"]?(https?://[^\s'"]+)`)
	// --title "…", --title='…', -t "…".
	titleFlagRe = regexp.MustCompile(`(?:--title|\s-t)[=\s]+(?:"([^"]+)"|'([^']+)'|(\S+))`)
	// The folder of a design of the library: …/design-artifacts/<id>/….
	designPathRe = regexp.MustCompile(`/design-artifacts/([a-z0-9][a-z0-9-]{0,62})/`)
)

// call is what a tool call says of itself that its result completes: the command it ran, what an Artifact call
// publishes, and the turn it belongs to.
type call struct {
	tool    string
	command string
	publish *jsvalue.Object
	turn    *turn
}

// remember keeps what a call's result will need to tell what it made; only the calls that may make something.
func (b *builder) remember(o *jsvalue.Object, use, tool, summary string) {
	cur := b.cur()
	if cur == nil || use == "" {
		return
	}
	c := call{tool: tool, turn: cur}
	if v, ok := o.Get("publish"); ok {
		c.publish, _ = v.(*jsvalue.Object)
	}
	if tool == "Bash" {
		c.command = strings.TrimPrefix(summary, "$ ")
	}
	if c.publish == nil && c.command == "" {
		return
	}
	b.calls[use] = c
}

// settle reads what a finished call made off the links of its result.
func (b *builder) settle(use string, o *jsvalue.Object, ok bool) {
	c, known := b.calls[use]
	if !known {
		return
	}
	delete(b.calls, use)
	if !ok {
		return
	}
	links := []string{}
	for _, v := range o.Array("links") {
		if s, isStr := v.(string); isStr {
			links = append(links, s)
		}
	}
	for _, out := range made(c, links) {
		addOutput(c.turn, out)
	}
}

// ran is a command whose output came with it (Codex): what it made, at once.
func (b *builder) ran(command, stdout string) {
	cur := b.cur()
	if cur == nil || command == "" {
		return
	}
	links := linksIn(stdout)
	for _, out := range made(call{tool: "Bash", command: command}, links) {
		addOutput(cur, out)
	}
}

var linkRe = regexp.MustCompile("https?://[^\\s\"'<>()\\[\\]{}`]+")

func linksIn(text string) []string {
	out := []string{}
	for _, l := range linkRe.FindAllString(text, 16) {
		out = append(out, strings.TrimRight(l, ".,;:!?*"))
	}
	return out
}

// made is what one call made, by its kind.
func made(c call, links []string) []wire.TurnOutput {
	if c.publish != nil {
		return artifactOf(c.publish, links)
	}
	cmd := c.command
	switch {
	case artifactCmdRe.MatchString(cmd):
		if link := first(links, artifactLink); link != "" {
			return []wire.TurnOutput{{Kind: wire.OutputArtifact, Op: wire.OutputCreated, Ref: link, Title: titleOr(cmd, link)}}
		}
	case strings.Contains(cmd, "glab mr create") || strings.Contains(cmd, "gh pr create"):
		if link := first(links, mrLinkRe.MatchString); link != "" {
			return []wire.TurnOutput{{Kind: wire.OutputMR, Op: wire.OutputCreated, Ref: link, Title: titleOr(cmd, mrName(link))}}
		}
	}
	var out []wire.TurnOutput
	if m := openCmdRe.FindStringSubmatch(cmd); m != nil {
		out = append(out, opened(m[1]))
	}
	if !strings.HasPrefix(strings.TrimSpace(cmd), "curl") {
		if link := first(links, standLink); link != "" {
			out = append(out, wire.TurnOutput{Kind: wire.OutputStand, Op: wire.OutputCreated, Ref: link, Title: hostOf(link)})
		}
	}
	return out
}

// artifactOf is the page an Artifact call published: the address in its result, its title or its file's name.
func artifactOf(pub *jsvalue.Object, links []string) []wire.TurnOutput {
	link := first(links, artifactLink)
	op := wire.OutputCreated
	if updated := pub.String("url"); updated != "" {
		op = wire.OutputChanged
		if link == "" {
			link = updated
		}
	}
	if link == "" {
		return nil
	}
	title := pub.String("title")
	if title == "" {
		if f := pub.String("file"); f != "" {
			title = strings.TrimSuffix(path.Base(f), path.Ext(f))
		}
	}
	if title == "" {
		title = slugOf(link)
	}
	return []wire.TurnOutput{{Kind: wire.OutputArtifact, Op: op, Ref: link, Title: clipTitle(title)}}
}

// opened is what `harnessly open <link>` showed: a page of the catalogue, a merge request, or a stand.
func opened(link string) wire.TurnOutput {
	switch {
	case artifactLink(link):
		return wire.TurnOutput{Kind: wire.OutputArtifact, Op: wire.OutputCreated, Ref: link, Title: slugOf(link)}
	case mrLinkRe.MatchString(link):
		return wire.TurnOutput{Kind: wire.OutputMR, Op: wire.OutputCreated, Ref: link, Title: mrName(link)}
	}
	return wire.TurnOutput{Kind: wire.OutputStand, Op: wire.OutputCreated, Ref: link, Title: hostOf(link)}
}

// wrote is what a write or an edit of a file made: a design of the library, a document of the repository; nothing
// for any other file.
func wrote(p string, created bool) (wire.TurnOutput, bool) {
	op := wire.OutputChanged
	if created {
		op = wire.OutputCreated
	}
	if m := designPathRe.FindStringSubmatch(p); m != nil {
		// A page written into a design changes it: the design is made by the library, not by a write.
		return wire.TurnOutput{Kind: wire.OutputDesign, Op: wire.OutputChanged, Ref: m[1], Title: m[1]}, true
	}
	if isDoc(p) {
		return wire.TurnOutput{Kind: wire.OutputDoc, Op: op, Ref: p, Title: strings.TrimSuffix(path.Base(p), path.Ext(p))}, true
	}
	return wire.TurnOutput{}, false
}

// isDoc: a Markdown file in a folder of documents (docs/, .knowledge/), not one of the agent's own notes.
func isDoc(p string) bool {
	if !strings.EqualFold(path.Ext(p), ".md") {
		return false
	}
	return strings.Contains(p, "/docs/") || strings.Contains(p, "/.knowledge/") || strings.HasPrefix(p, "docs/")
}

// addOutput puts a thing into the turn's outputs once: what is named again keeps its place, and a thing the turn
// made stays made however often it changed it after.
func addOutput(t *turn, out wire.TurnOutput) {
	out.Title = clipTitle(out.Title)
	if out.Ref == "" || out.Title == "" {
		return
	}
	for i, have := range t.rec.Outputs {
		if have.Kind == out.Kind && have.Ref == out.Ref {
			if out.Op == wire.OutputCreated {
				t.rec.Outputs[i].Op = out.Op
			}
			return
		}
	}
	t.rec.Outputs = append(t.rec.Outputs, out)
}

func first(links []string, is func(string) bool) string {
	for _, l := range links {
		if is(l) {
			return l
		}
	}
	return ""
}

// artifactLink tells a claude.ai artifact or a page of the team's catalogue, the host RUNTIME_DOCS_HOST names.
func artifactLink(link string) bool {
	if claudeArtifactRe.MatchString(link) {
		return true
	}
	host := strings.TrimSpace(os.Getenv("RUNTIME_DOCS_HOST"))
	u, err := url.Parse(link)
	return host != "" && err == nil && (u.Scheme == "https" || u.Scheme == "http") && strings.EqualFold(u.Host, host)
}

// standLink tells a feature stand, a preview of a branch: one name under RUNTIME_STAND_DOMAIN (`.preview.example.com`).
func standLink(link string) bool {
	domain := strings.TrimPrefix(strings.TrimSpace(os.Getenv("RUNTIME_STAND_DOMAIN")), ".")
	u, err := url.Parse(link)
	if domain == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	name, ok := strings.CutSuffix(strings.ToLower(u.Hostname()), "."+strings.ToLower(domain))
	return ok && standNameRe.MatchString(name)
}

// titleOr is the --title the command gives, else the fallback.
func titleOr(cmd, fallback string) string {
	if m := titleFlagRe.FindStringSubmatch(cmd); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	return fallback
}

// mrName is a merge request as its chip calls it: «harnessly !80», «repo #12».
func mrName(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 {
		return link
	}
	n, kind := parts[len(parts)-1], parts[len(parts)-2]
	project := ""
	for _, p := range parts[:len(parts)-2] {
		if p != "-" {
			project = p
		}
	}
	mark := "!"
	if kind == "pull" {
		mark = "#"
	}
	return project + " " + mark + n
}

func hostOf(link string) string {
	if u, err := url.Parse(link); err == nil && u.Host != "" {
		return u.Host
	}
	return link
}

// slugOf is the last part of a page's address, its name when nothing else names it.
func slugOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return path.Base(p)
	}
	return hostOf(link)
}

func clipTitle(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > outputTitleMax {
		return string(r[:outputTitleMax-1]) + "…"
	}
	return s
}
