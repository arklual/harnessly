// Package outputs tells what a tool call made that the person opens (wire.TurnOutput): a page the Artifact tool or
// the artifacts CLI published, a design of the library the agent wrote into, a document of the repository, a merge
// request a command opened, a stand. A plain file is not one. The feed reads it per turn, the sources index per chat.
package outputs

import (
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Config names the hosts that tell a page of the catalogue and a stand.
type Config struct {
	// DocsHost serves the team's published pages (RUNTIME_DOCS_HOST); StandDomain the feature stands (RUNTIME_STAND_DOMAIN).
	DocsHost, StandDomain string
	// ReaderHost and ReaderPath are where the platform reads a document (https://<ReaderHost><ReaderPath><slug>,
	// RUNTIME_PLATFORM_HOST and RUNTIME_DOCS_READER_PATH): a document too, never a stand. "" for none.
	ReaderHost, ReaderPath string
	// CLI is the name the deployment gives the harnessly command (RUNTIME_CLI_NAME): `<CLI> open <link>` is read as
	// `harnessly open <link>` is.
	CLI string
}

// FromEnv is the Config the environment names.
func FromEnv() Config {
	cfg := Config{
		DocsHost:    strings.TrimSpace(os.Getenv("RUNTIME_DOCS_HOST")),
		StandDomain: strings.TrimSpace(os.Getenv("RUNTIME_STAND_DOMAIN")),
		ReaderHost:  strings.TrimSpace(os.Getenv("RUNTIME_PLATFORM_HOST")),
		ReaderPath:  strings.TrimSpace(os.Getenv("RUNTIME_DOCS_READER_PATH")),
		CLI:         strings.TrimSpace(os.Getenv("RUNTIME_CLI_NAME")),
	}
	if cfg.ReaderHost != "" && cfg.ReaderPath == "" {
		cfg.ReaderPath = "/agents/docs/"
	}
	return cfg
}

// Call is what a tool call says of itself that its result completes: the command it ran, what an Artifact call
// publishes, and whether it is the MCP tool that hands a file over.
type Call struct {
	Tool    string
	Command string
	Publish *jsvalue.Object
	Deliver bool
}

// TitleMax is the longest title a card gets, in runes.
const TitleMax = 120

var (
	// A merge request on GitLab, a pull request on GitHub.
	mrLinkRe = regexp.MustCompile(`^https?://[^/]+/.+?/(?:-/)?(?:merge_requests|pull)/\d+$`)
	// A claude.ai artifact; a page of the team's catalogue is told by its host (artifactLink).
	claudeArtifactRe = regexp.MustCompile(`^https?://claude\.ai/(?:code/)?artifact/`)
	// The artifacts CLI publishing a page: `<name>-artifact publish …`, `harnessly artifact publish …`.
	artifactCmdRe = regexp.MustCompile(`(?:^|[\s;&|(/-])artifacts?\s+publish\b`)
	// The name of one feature stand under the stands' domain.
	standNameRe = regexp.MustCompile(`^[\w-]+$`)
	// A dev server the agent shows: `harnessly open http://localhost:5173` (or the deployment's name of the command).
	openCmdRe = regexp.MustCompile(`(?:^|[\s;&|(/])([\w-]+)\s+open\s+['"]?(https?://[^\s'"]+)`)
	// The slug of a document in the reader's path.
	readerSlugRe = regexp.MustCompile(`^[\w-]+/?$`)
	// A file handed to the person: `harnessly deliver <path>`.
	deliverCmdRe = regexp.MustCompile(`(?:^|[\s;&|(])(?:harnessly)\s+deliver\b`)
	// --title "…", --title='…', -t "…".
	titleFlagRe = regexp.MustCompile(`(?:--title|\s-t)[=\s]+(?:"([^"]+)"|'([^']+)'|(\S+))`)
	// The folder of a design of the library: …/design-artifacts/<id>/….
	designPathRe = regexp.MustCompile(`/design-artifacts/([a-z0-9][a-z0-9-]{0,62})/`)
)

// IsDeliverTool: the MCP tool deliver_file, under whatever name its server is given.
func IsDeliverTool(tool string) bool {
	return tool == "deliver_file" || strings.HasSuffix(tool, "__deliver_file")
}

var linkRe = regexp.MustCompile("https?://[^\\s\"'<>()\\[\\]{}`]+")

func LinksIn(text string) []string {
	out := []string{}
	for _, l := range linkRe.FindAllString(text, 16) {
		out = append(out, strings.TrimRight(l, ".,;:!?*"))
	}
	return out
}

// Of is what one call made, by its kind.
func Of(cfg Config, c Call, links []string) []wire.TurnOutput {
	if c.Publish != nil {
		return artifactOf(cfg, c.Publish, links)
	}
	cmd := c.Command
	if c.Deliver || deliverCmdRe.MatchString(cmd) {
		// Only the record the tool wrote counts: a file the call merely names is not one the person was given.
		if info, ok := delivery.LinkIn(links); ok {
			title := info.Title
			if title == "" {
				title = info.Name
			}
			return []wire.TurnOutput{{Kind: wire.OutputDelivery, Op: wire.OutputCreated, Ref: info.Path, Title: title,
				Name: info.Name, Mime: info.Mime, Size: info.Size, SHA256: info.SHA256}}
		}
		return nil
	}
	switch {
	case artifactCmdRe.MatchString(cmd):
		if link := first(links, func(l string) bool { return ArtifactLink(cfg, l) }); link != "" {
			return []wire.TurnOutput{{Kind: wire.OutputArtifact, Op: wire.OutputCreated, Ref: link, Title: titleOr(cmd, link)}}
		}
	case strings.Contains(cmd, "glab mr create") || strings.Contains(cmd, "gh pr create"):
		if link := first(links, mrLinkRe.MatchString); link != "" {
			return []wire.TurnOutput{{Kind: wire.OutputMR, Op: wire.OutputCreated, Ref: link, Title: titleOr(cmd, mrName(link))}}
		}
	}
	var out []wire.TurnOutput
	if m := openCmdRe.FindStringSubmatch(cmd); m != nil && (m[1] == "harnessly" || (cfg.CLI != "" && m[1] == cfg.CLI)) {
		if o, ok := opened(cfg, m[2]); ok {
			out = append(out, o)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(cmd), "curl") {
		if link := first(links, func(l string) bool { return StandLink(cfg, l) }); link != "" {
			out = append(out, wire.TurnOutput{Kind: wire.OutputStand, Op: wire.OutputCreated, Ref: link, Title: hostOf(link)})
		}
	}
	return out
}

// artifactOf is the page an Artifact call published: the address in its result, its title or its file's name.
func artifactOf(cfg Config, pub *jsvalue.Object, links []string) []wire.TurnOutput {
	link := first(links, func(l string) bool { return ArtifactLink(cfg, l) })
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
		title = SlugOf(link)
	}
	return []wire.TurnOutput{{Kind: wire.OutputArtifact, Op: op, Ref: link, Title: Clip(title)}}
}

// opened is what `harnessly open <link>` showed: a page of the catalogue (a document of the reader among them), a
// merge request, a stand or a dev server of the machine. Any other site makes no card: it is no stand (a
// document opened through the reader was a «stand» card).
func opened(cfg Config, link string) (wire.TurnOutput, bool) {
	switch {
	case ArtifactLink(cfg, link):
		return wire.TurnOutput{Kind: wire.OutputArtifact, Op: wire.OutputCreated, Ref: link, Title: SlugOf(link)}, true
	case mrLinkRe.MatchString(link):
		return wire.TurnOutput{Kind: wire.OutputMR, Op: wire.OutputCreated, Ref: link, Title: mrName(link)}, true
	case StandLink(cfg, link) || localLink(link):
		return wire.TurnOutput{Kind: wire.OutputStand, Op: wire.OutputCreated, Ref: link, Title: hostOf(link)}, true
	}
	return wire.TurnOutput{}, false
}

// localLink is a server of this machine: http(s)://localhost:<port>, 127.0.0.1, 0.0.0.0, [::1].
func localLink(link string) bool {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Port() == "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return true
	}
	return false
}

// Wrote is what a write or an edit of a file made: a design of the library, a document of the repository; nothing
// for any other file.
func Wrote(p string, created bool) (wire.TurnOutput, bool) {
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

func first(links []string, is func(string) bool) string {
	for _, l := range links {
		if is(l) {
			return l
		}
	}
	return ""
}

// ArtifactLink tells a claude.ai artifact or a page of the team's catalogue, the host RUNTIME_DOCS_HOST names.
func ArtifactLink(cfg Config, link string) bool {
	if claudeArtifactRe.MatchString(link) {
		return true
	}
	host := cfg.DocsHost
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if host != "" && strings.EqualFold(u.Host, host) {
		return true
	}
	// The platform's reader of a document: https://<ReaderHost><ReaderPath><slug>, nothing else of the platform.
	if cfg.ReaderHost == "" || cfg.ReaderPath == "" || !strings.EqualFold(u.Host, cfg.ReaderHost) {
		return false
	}
	rest, ok := strings.CutPrefix(u.Path, cfg.ReaderPath)
	return ok && readerSlugRe.MatchString(rest)
}

// StandLink tells a feature stand, a preview of a branch: one name under RUNTIME_STAND_DOMAIN (`.preview.example.com`).
func StandLink(cfg Config, link string) bool {
	domain := strings.TrimPrefix(cfg.StandDomain, ".")
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

// SlugOf is the last part of a page's address, its name when nothing else names it.
func SlugOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return path.Base(p)
	}
	return hostOf(link)
}

func Clip(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > TitleMax {
		return string(r[:TitleMax-1]) + "…"
	}
	return s
}

// Publishes: the call puts a new page out (the Artifact tool, the artifacts CLI), as against one that only shows or
// reads a page.
func Publishes(c Call) bool {
	return c.Publish != nil || artifactCmdRe.MatchString(c.Command)
}
