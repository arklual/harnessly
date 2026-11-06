package design

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Locks of a board made from an app's template.
//
// The rules live in meta/pages/<id>.json under "template", not in the board: the board file is rewritten whole by an
// agent or by a restore, with its markup. The record says which app and template the board came from, what stays
// editable (the keys of data-hl-key / data-hl-edit and the fields of data-hl-field, as the template had them) and a
// fold of the rest. A write is let through when the fold of the board after it equals the record's: the check looks at
// the result, so it holds for every path that writes a board.

// The lock levels of a template board.
const (
	TemplateLockStrict = "strict"
	TemplateLockOpen   = "open"
)

// PageTemplate is the record of a board made from a template.
type PageTemplate struct {
	App     string `json:"app"`
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	// Lock is strict (only Editable changes) or open (a starting point: everything changes).
	Lock string `json:"lock"`
	// Editable is what the person may change: data-hl-key -> data-hl-edit ("text", "slot", "props:a,b") and
	// "field:<name>" -> "field" for the elements bound with data-hl-field.
	Editable map[string]string `json:"editable,omitempty"`
	// Closed is the digest of the fold of everything else; Parts the digests per data-hl-key, to name what broke.
	Closed string            `json:"closed,omitempty"`
	Parts  map[string]string `json:"parts,omitempty"`
}

// Template is the page's record of the template it came from.
func (p Page) Template() (PageTemplate, bool) {
	if p.raw == nil {
		return PageTemplate{}, false
	}
	v, ok := p.raw.Get("template")
	if !ok {
		return PageTemplate{}, false
	}
	var t PageTemplate
	if json.Unmarshal(jsvalue.Stringify(v, ""), &t) != nil || t.App == "" {
		return PageTemplate{}, false
	}
	return t, true
}

func (p *Page) setTemplate(t *PageTemplate) {
	if p.raw == nil {
		p.raw = jsvalue.NewObject()
	}
	if t == nil {
		p.raw.Delete("template")
		return
	}
	raw, _ := json.Marshal(t)
	v, _ := jsvalue.Parse(raw)
	p.raw.Set("template", v)
}

// Locked says whether the record keeps the board from free edits.
func (t PageTemplate) Locked() bool { return t.Lock == TemplateLockStrict }

// NewPageTemplate is the record for a board made from src: the editable places are those the template marks, the
// fold is of the rest. lock is strict or open.
func NewPageTemplate(app, id, version, lock string, src []byte) PageTemplate {
	t := PageTemplate{App: app, ID: id, Version: version, Lock: lock}
	if lock != TemplateLockStrict {
		t.Lock = TemplateLockOpen
		return t
	}
	t.Editable = map[string]string{}
	if root, err := html.Parse(strings.NewReader(string(src))); err == nil {
		var walk func(n *html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				key, edit, field := lockAttr(n, "data-hl-key"), lockAttr(n, "data-hl-edit"), lockAttr(n, "data-hl-field")
				if key != "" && edit != "" {
					t.Editable[key] = edit
				}
				if field != "" {
					t.Editable["field:"+field] = "field"
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}
	t.Closed, t.Parts = foldBoard(string(src), t.Editable)
	return t
}

func lockAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// openMode is how an element is open under the record: "" closed, else its edit mode.
func openMode(n *html.Node, editable map[string]string) string {
	if key := lockAttr(n, "data-hl-key"); key != "" {
		if m, ok := editable[key]; ok {
			return m
		}
	}
	if f := lockAttr(n, "data-hl-field"); f != "" {
		if m, ok := editable["field:"+f]; ok {
			return m
		}
	}
	return ""
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:12])
}

// foldBoard is the canonical form of what is closed in a board: tags, attributes (sorted) and words, with the open
// places left out. The whole digest and the digest of each data-hl-key subtree.
func foldBoard(src string, editable map[string]string) (string, map[string]string) {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", nil
	}
	parts := map[string]string{}
	var b strings.Builder
	foldNode(root, editable, &b, parts)
	return digest(b.String()), parts
}

func foldNode(n *html.Node, editable map[string]string, w *strings.Builder, parts map[string]string) {
	switch n.Type {
	case html.TextNode:
		if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
			w.WriteString(t)
			w.WriteByte('\n')
		}
		return
	case html.ElementNode:
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			foldNode(c, editable, w, parts)
		}
		return
	default:
		return
	}
	start := w.Len()
	mode := openMode(n, editable)
	skip := map[string]bool{}
	skipKids := false
	switch {
	case mode == "text" || mode == "slot":
		skipKids = true
	case mode == "field":
		if a := lockAttr(n, "data-hl-attr"); a != "" {
			skip[a] = true
		} else {
			skipKids = true
		}
	case strings.HasPrefix(mode, "props:"):
		for _, a := range strings.Split(strings.TrimPrefix(mode, "props:"), ",") {
			skip[strings.TrimSpace(a)] = true
		}
	}
	attrs := make([]string, 0, len(n.Attr))
	for _, a := range n.Attr {
		if !skip[a.Key] {
			attrs = append(attrs, a.Key+"="+a.Val)
		}
	}
	sort.Strings(attrs)
	w.WriteString("<" + n.Data + " " + strings.Join(attrs, "\x00") + ">")
	if mode != "" {
		w.WriteString("[" + mode + "]")
	}
	if skipKids {
		w.WriteString("…")
	} else {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			foldNode(c, editable, w, parts)
		}
	}
	w.WriteString("</" + n.Data + ">\n")
	if key := lockAttr(n, "data-hl-key"); key != "" {
		parts[key] = digest(w.String()[start:])
	}
}

// lockError is the refusal of a write that breaks a lock; it names the first key that no longer fits.
func lockError(title string, t PageTemplate, board string, parts map[string]string) error {
	what := ""
	keys := make([]string, 0, len(t.Parts))
	for k := range t.Parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if parts[k] != t.Parts[k] {
			what = k
			break
		}
	}
	en := "The board \"" + title + "\" is locked by the app \"" + t.App + "\": only the marked fields change"
	ru := "Доска «" + title + "» закрыта приложением «" + t.App + "»: меняются только размеченные поля"
	if what != "" {
		en += " (closed part changed: " + what + ")"
		ru += " (изменена закрытая часть: " + what + ")"
	}
	_ = board
	return docErr(DocErrLocked, en, ru)
}

// pageOfFile is the page of the project whose board is file.
func (l *Library) pageOfFile(project, file string) (Page, bool) {
	m := l.ReadModel(project)
	if m == nil {
		return Page{}, false
	}
	file = pageFileOf(file)
	for _, p := range m.Pages {
		if pageFileOf(p.File) == file {
			return p, true
		}
	}
	return Page{}, false
}

// checkBoardLock refuses next as the new content of the board of p when the board is locked and next changes what is
// closed.
func (l *Library) checkBoardLock(p Page, next string) error {
	t, ok := p.Template()
	if !ok || !t.Locked() {
		return nil
	}
	closed, parts := foldBoard(next, t.Editable)
	if closed != "" && closed == t.Closed {
		return nil
	}
	return lockError(p.Title, t, p.File, parts)
}

// BoardLockReason is why a write of the board file must be refused, "" when it may go on: the file is the board of a
// locked page, or the page's own file in meta/pages. The agent's hook asks it (a write by an agent is not seen as a
// change of content, so the file itself is closed to it; the Document API is the way).
func (l *Library) BoardLockReason(project, absPath string) string {
	meta := filepath.Join(l.MetaDir(project), MetaPagesDirname)
	rel, err := filepath.Rel(filepath.Join(l.ArtifactDir(project)), absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	m := l.ReadModel(project)
	if m == nil {
		return ""
	}
	inMeta := filepath.Dir(absPath) == meta
	for _, p := range m.Pages {
		t, ok := p.Template()
		if !ok || !t.Locked() {
			continue
		}
		if (inMeta && filepath.Base(absPath) == p.ID+".json") || filepath.Join(l.ProjectDir(project), pageFileOf(p.File)) == absPath {
			return locale.Pick(
				"The board \""+p.Title+"\" is locked by the app \""+t.App+"\": its file is not edited directly. Change the marked fields through the app's tools (the Document API).",
				"Доска «"+p.Title+"» закрыта приложением «"+t.App+"»: её файл напрямую не правят. Размеченные поля меняются через инструменты приложения (Document API).")
		}
	}
	return ""
}

// UnbindTemplate drops the record of a board: it becomes an ordinary one. Who may do it (the app's owner) is the
// caller's to decide.
func (l *Library) UnbindTemplate(project, pageRef string) error {
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	m := l.ReadModel(project)
	if m == nil {
		return docErr(DocErrNotFound, "No such board", "Такой доски нет")
	}
	p, ok := m.Page(pageRef)
	if !ok {
		return docErr(DocErrNotFound, "No such board", "Такой доски нет")
	}
	if _, has := p.Template(); !has {
		return nil
	}
	p.setTemplate(nil)
	if _, err := l.writePage(project, p); err != nil {
		return docErr(DocErrFailed, "Could not save the board", "Не удалось сохранить доску")
	}
	return nil
}
