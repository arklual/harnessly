package docs

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// The card of a link: a link to a document or a board pasted into a chat, the start page or a task shows as
// a card with the thing's name, who changed it last and when, and a look at it: a board's picture, the first page of an
// HTML document, the first lines of a text one. The page asks the hub
// through its machine, as the person: the hub answers only what the person may read (the same rank as opening it,
// docs/hub/resource-access.md), so a card tells a stranger nothing a closed link would not.

// linkCard is what a card shows.
type linkCard struct {
	Slug  string `json:"slug"`
	Kind  string `json:"kind"` // artifact, board
	Title string `json:"title"`
	// Author is who made it (the hub's owner, else the service's author); "" when nobody is named.
	Author string `json:"author"`
	// At is when it last changed (a version, an edit, a comment).
	At string `json:"at"`
	// OpenThreads are its comment threads not resolved.
	OpenThreads int `json:"openThreads"`
	// Editor and EditedAt: who changed it last and when (a document's current version, a board's last edit); "" when
	// the service does not say.
	Editor   string `json:"editor"`
	EditedAt string `json:"editedAt"`
	// Preview: the hub has a picture of it (GET …/{slug}/preview, an SVG).
	Preview bool `json:"preview"`
	// Page: an HTML document whose first page GET …/{slug}/preview answers, for a frame without scripts' reach.
	Page bool `json:"page"`
	// Excerpt: the first lines of a text document, without its heading.
	Excerpt string `json:"excerpt,omitempty"`
}

func (s *Service) card(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	c := linkCard{Slug: doc.Slug, Kind: doc.Kind, Title: doc.Title, At: doc.LastActivityAt, OpenThreads: doc.OpenThreads, Author: doc.author, Editor: doc.editor, EditedAt: doc.edited}
	if doc.Owner != nil && doc.Owner.Name != "" {
		c.Author = doc.Owner.Name
	}
	if doc.Kind == "board" {
		c.Preview = s.boardPreview(r.Context(), doc.Slug) != ""
	} else if md, _, _, err := s.art.markdown(r.Context(), doc.Slug, ""); err == nil {
		// A document published as Markdown reads as its first lines; one published as HTML shows its first page.
		c.Excerpt = excerptOf(md, doc.Title)
		c.Page = md == ""
	}
	w.Header().Set("Cache-Control", "private, max-age=30")
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

// boardPreview is the address of a board's thumbnail on the boards service, "" when it has none. Only an address on
// the boards service itself is taken: the hub never fetches a place the service's answer names elsewhere.
func (s *Service) boardPreview(ctx context.Context, slug string) string {
	bs, err := s.boards(ctx)
	if err != nil {
		return ""
	}
	base, err := url.Parse(s.cfg.BoardsURL)
	if err != nil {
		return ""
	}
	for _, b := range bs {
		if b.Slug != slug || b.Preview == "" {
			continue
		}
		u, err := url.Parse(b.Preview)
		if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.User != nil {
			return ""
		}
		return u.String()
	}
	return ""
}

// maxPreview is the most of a thumbnail the hub passes on.
const maxPreview = 2 << 20

// preview answers a board's thumbnail for the person who may read the board: the hub fetches it with the board's key,
// which never reaches the browser. The picture is an SVG drawn by the service: it is served with a sandbox and no
// scripts, and the page shows it in an <img>, where it cannot run anything anyway.
func (s *Service) preview(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	if doc.Kind != "board" {
		return s.pagePreview(w, r, doc.Slug)
	}
	src := s.boardPreview(r.Context(), doc.Slug)
	if src == "" {
		return errNoDoc
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "image/svg+xml")
	res, err := s.hc.Do(req)
	if err != nil {
		// The error names the address, and the address carries the board's key: it must not reach the log.
		return &upstreamError{Status: http.StatusBadGateway}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/svg+xml") {
		return &upstreamError{Status: http.StatusNotFound}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxPreview+1))
	if err != nil {
		return err
	}
	if len(body) > maxPreview {
		return errNoDoc
	}
	h := w.Header()
	h.Set("Content-Type", "image/svg+xml")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(body)
	return nil
}

// pagePreview answers a document's page for the small frame of its card: the same page the viewer shows, without the
// viewer's bridge, in a sandbox of its own origin that may draw (scripts) but not call anywhere or send a form.
func (s *Service) pagePreview(w http.ResponseWriter, r *http.Request, slug string) error {
	res, err := s.art.page(r.Context(), slug, "", themeOf(r.URL.Query().Get("theme")))
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	page, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	h := w.Header()
	h.Del("X-Frame-Options")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "sandbox allow-scripts; frame-ancestors 'self'; connect-src 'none'; form-action 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(page)
	return nil
}

// excerptLines and excerptRunes bound what a card quotes of a text document.
const (
	excerptLines = 3
	excerptRunes = 280
)

var (
	mdLink   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdLead   = regexp.MustCompile(`^(\s*(#{1,6}|>|[-*+]|\d+[.)])\s+)+`)
	mdMarks  = strings.NewReplacer("**", "", "__", "", "`", "", "~~", "")
	mdIgnore = regexp.MustCompile(`^(\s*(---+|\*\*\*+|___+|\|.*\||<[^>]*>)\s*)$`)
)

// excerptOf is the first lines of a Markdown text as plain words: the heading that repeats the title, rules, tables,
// code and tags are left out.
func excerptOf(md, title string) string {
	var out []string
	fence := false
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
			continue
		}
		if fence || t == "" || mdIgnore.MatchString(t) {
			continue
		}
		heading := strings.HasPrefix(t, "#")
		t = strings.TrimSpace(mdMarks.Replace(mdLink.ReplaceAllString(mdLead.ReplaceAllString(t, ""), "$1")))
		if t == "" || (heading && len(out) == 0 && strings.EqualFold(t, strings.TrimSpace(title))) {
			continue
		}
		out = append(out, t)
		if len(out) == excerptLines {
			break
		}
	}
	text := strings.Join(out, "\n")
	if r := []rune(text); len(r) > excerptRunes {
		text = strings.TrimSpace(string(r[:excerptRunes-1])) + "…"
	}
	return text
}
