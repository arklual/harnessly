package convo

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What the page's right pane reads of a conversation (its links, the files and pictures it worked with, the words
// it exchanged with other agents, its subagents) is gathered here from the entries the feed's own reads took: the
// transcript has one reader, the conversation's actor, and the pane's part of every read goes out in the
// conversation's channel beside the feed's. A conversation starts gathering when a page first asks for its snapshot.

const (
	// paneTextMax is how much of an item's text goes out: a teammate's long report is cut, not dropped.
	paneTextMax = 16 << 10
	// paneBudget is what a snapshot's pane may weigh: its oldest items go first.
	paneBudget = 48 << 10
)

// paneFeed is one conversation's pane data so far.
type paneFeed struct {
	links sources.Collector
	// sent is the list of links as the pages hold it.
	sent []wire.PaneLink
	// items are the newest wire.PaneItemsMax, oldest first.
	items []wire.PaneItem
	// agents are the ids of the subagent calls seen: their results are items, other results are not.
	agents map[string]bool
	count  int64
}

func newPaneFeed(links *sources.Links) *paneFeed {
	return &paneFeed{links: sources.Collector{Links: links}, agents: map[string]bool{}}
}

// add takes a read's entries in and answers the items they made and whether the list of links changed.
func (p *paneFeed) add(entries []*jsvalue.Object) (items []wire.PaneItem, links bool) {
	p.count += int64(len(entries))
	for _, o := range entries {
		if it, ok := p.item(o); ok {
			items = append(items, it)
		}
	}
	p.items = append(p.items, items...)
	if n := len(p.items) - wire.PaneItemsMax; n > 0 {
		p.items = slices.Clone(p.items[n:])
	}
	p.links.Add(entries)
	now := paneLinks(p.links.List())
	if !slices.EqualFunc(now, p.sent, samePaneLink) {
		p.sent, links = now, true
	}
	return items, links
}

func samePaneLink(a, b wire.PaneLink) bool {
	return a.URL == b.URL && a.At == b.At && a.Count == b.Count && a.Kind == b.Kind &&
		(a.Label == nil) == (b.Label == nil) && (a.Label == nil || *a.Label == *b.Label)
}

// paneLinks is the newest links as the wire carries them.
func paneLinks(list []sources.Link) []wire.PaneLink {
	if len(list) > wire.PaneLinksMax {
		list = list[:wire.PaneLinksMax]
	}
	out := make([]wire.PaneLink, 0, len(list))
	for _, l := range list {
		if l.URL == "" {
			continue
		}
		kind := l.Kind
		if kind == "" {
			kind = "web"
		}
		var at wire.Ms
		if l.Timestamp > 0 { // false for NaN too
			at = wire.Ms(l.Timestamp)
		}
		out = append(out, wire.PaneLink{URL: l.URL, Label: l.Label, At: at, Count: l.Count, Kind: kind})
	}
	return out
}

// peerMarks are what a line of the person's side carries when other agents' words are in it.
var peerMarks = []string{"<cross-session-message", "<teammate-message", "<agent-message", "[Cross-session idle notice]"}

// item is the entry as the pane's tools need it; false for an entry none of them reads.
func (p *paneFeed) item(o *jsvalue.Object) (wire.PaneItem, bool) {
	it := wire.PaneItem{ID: o.String("id")}
	if ts, ok := o.Number("timestamp"); ok && ts > 0 {
		it.At = wire.Ms(ts)
	}
	if it.ID == "" {
		return it, false
	}
	switch o.String("type") {
	case "tool-call":
		it.K, it.Tool, it.Use, it.Path = "tool", o.String("toolName"), o.String("toolUseId"), o.String("filePath")
		if pm := o.Object("peerMessage"); pm != nil {
			it.Peer = &wire.PanePeer{To: pm.String("to"), Text: paneClip(pm.String("text"))}
		}
		if sent := o.Object("sentFiles"); sent != nil {
			it.Files = jsvalue.Strings(sent.Array("paths"))
		}
		agent := it.Tool == "Agent" || it.Tool == "Task"
		if agent && it.Use != "" {
			p.agents[it.Use] = true
		}
		// The summary is read for the paths of a shell command and for whether a file was created.
		if it.Tool == "Bash" || it.Path != "" {
			it.Text = paneClip(o.String("summary"))
		}
		return it, it.Tool == "Bash" || it.Path != "" || it.Peer != nil || len(it.Files) > 0 || agent
	case "tool-result":
		it.K, it.Use = "result", o.String("toolUseId")
		return it, p.agents[it.Use]
	case "task-notification":
		it.K = "notice"
		return it, true
	case "user-message":
		text := o.String("text")
		for _, src := range jsvalue.Strings(o.Array("images")) {
			// A picture the transcript carries whole is megabytes of base64: it stays in the transcript.
			if src != "" && !strings.HasPrefix(src, "data:") {
				it.Images = append(it.Images, src)
			}
		}
		peers := slices.ContainsFunc(peerMarks, func(m string) bool { return strings.Contains(text, m) })
		if len(it.Images) == 0 && !peers && !strings.Contains(text, "/uploads/") {
			return it, false
		}
		it.K, it.Text = "user", paneClip(text)
		return it, true
	case "assistant-message":
		md := o.String("markdown")
		if !strings.Contains(md, "![") {
			return it, false
		}
		it.K, it.Text = "answer", paneClip(md)
		return it, true
	case "patch-apply":
		it.K = "patch"
		for _, f := range o.Array("files") {
			if fo, ok := f.(*jsvalue.Object); ok && fo.String("path") != "" {
				it.Files = append(it.Files, fo.String("path"))
			}
		}
		return it, len(it.Files) > 0
	case "exec-command-stream":
		it.K, it.Cwd, it.Text = "exec", o.String("cwd"), paneClip(o.String("command"))
		return it, it.Cwd != "" || it.Text != ""
	}
	return it, false
}

// paneClip cuts a text to paneTextMax bytes, at a character.
func paneClip(s string) string {
	if len(s) <= paneTextMax {
		return s
	}
	n := paneTextMax
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// whole is the pane as a snapshot carries it: every link, and the newest items that fit the budget.
func (p *paneFeed) whole() *wire.Pane {
	items := p.items
	total := 0
	sizes := make([]int, len(items))
	for i, it := range items {
		b, _ := json.Marshal(it)
		sizes[i] = len(b) + 1
		total += sizes[i]
	}
	for len(items) > 0 && total > paneBudget {
		total -= sizes[len(sizes)-len(items)]
		items = items[1:]
	}
	return &wire.Pane{Count: p.count, Links: slices.Clone(p.sent), Items: slices.Clone(items)}
}

// paneSnapshot is the pane of a snapshot, nil on a runtime that does not gather it. The first one starts the
// gathering, from the entries the feed already holds.
func (c *conv) paneSnapshot() *wire.Pane {
	if c.d.PaneLinks == nil {
		return nil
	}
	if c.pane == nil {
		c.pane = newPaneFeed(c.d.PaneLinks)
		c.pane.add(c.fd.Sources())
	}
	return c.pane.whole()
}

// paneRead takes the entries of a read of the feed in and tells the pages what the pane gained.
func (c *conv) paneRead(u feed.Update) {
	if c.pane == nil || !u.Whole && len(u.Read) == 0 {
		return
	}
	if u.Whole {
		c.pane = newPaneFeed(c.d.PaneLinks)
	}
	items, links := c.pane.add(u.Read)
	if c.deletedAt != 0 {
		return
	}
	out := wire.Pane{Count: c.pane.count, Items: items}
	if u.Whole || len(items) > wire.PaneItemsMax {
		// A read larger than a snapshot's pane goes out as one: the page holds the newest items either way.
		out = *c.pane.whole()
		out.Reset = true
	} else if links {
		out.Links = slices.Clone(c.pane.sent)
	}
	c.emit(wire.QueueChangedEv{Queue: c.box.Queue(), Pane: &out})
}

// paneReread starts the pane over on the feed as it is now: the conversation went to another transcript.
func (c *conv) paneReread() {
	if c.pane == nil {
		return
	}
	c.paneRead(feed.Update{Read: c.fd.Sources(), Whole: true})
}
