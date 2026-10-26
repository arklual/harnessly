package convo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/attach"
	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/msgindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Search over the messages of the conversations. The index (internal/msgindex) is fed two ways: the conversation's own
// feed puts the entries it reads as they come, and a scan reads whole the transcripts the index has not seen at their
// present size, a few at a time and unhurried, so the runtime is not slowed by thousands of old chats.

const (
	// indexFirst is how long after the start the first scan begins; indexEvery, the pause between scans.
	indexFirst = 15 * time.Second
	indexEvery = time.Minute
	// indexBudget is how long one scan reads; indexPause, the rest between two transcripts.
	indexBudget = 20 * time.Second
	indexPause  = 40 * time.Millisecond
	// indexQuiet: a transcript written to within this is the live feed's, not the scan's.
	indexQuiet = 20 * time.Second
	// indexWholeMax is the largest transcript read whole; a larger one gives the window the feed opens with.
	indexWholeMax = 64 << 20
)

// messageDoc is the entry as a message of the index; false for an entry that is not one.
func messageDoc(conv string, e wire.Entry) (msgindex.Doc, bool) {
	var role, text string
	switch e.T {
	case wire.EntryUser:
		// The block of places under the person's words (another chat's excerpt among them) is not what they wrote.
		role, text = msgindex.RoleUser, attach.Strip(e.Text)
	case wire.EntrySay:
		role, text = msgindex.RoleAgent, e.MD
	default:
		return msgindex.Doc{}, false
	}
	if strings.TrimSpace(text) == "" {
		return msgindex.Doc{}, false
	}
	d := msgindex.Doc{Conv: conv, Entry: e.ID, At: e.At, Role: role, Text: text}
	if e.TurnID != nil {
		d.Turn = *e.TurnID
	}
	return d, true
}

// indexUpdate puts what a read of the feed gained into the index. The index is marked as read up to the transcript's
// size only while it followed the feed without a gap: the mark it had was the size the feed stood at before.
func (c *conv) indexUpdate(u feed.Update) {
	ix := c.d.Index
	if ix == nil || c.deletedAt != 0 || c.fd == nil {
		return
	}
	var docs []msgindex.Doc
	for _, st := range u.Steps {
		if d, ok := messageDoc(c.rec.ID, st.Entry); ok {
			docs = append(docs, d)
		}
	}
	for _, e := range u.Updated {
		if d, ok := messageDoc(c.rec.ID, e); ok {
			docs = append(docs, d)
		}
	}
	ix.Put(docs...)
	size := c.fd.Size()
	if mark, ok := ix.Size(c.rec.ID); ok && mark == c.indexSeen {
		ix.Mark(c.rec.ID, size)
	}
	c.indexSeen = size
}

// source opens the transcript of a conversation the way its actor does.
func (d *Deps) source(rec wire.Conversation) feed.Source {
	switch {
	case d.Source != nil:
		return d.Source(rec)
	case rec.Provider == wire.ProviderCodex:
		return &feed.CodexSource{Path: rec.TranscriptPath}
	default:
		return &feed.ClaudeSource{Path: rec.TranscriptPath}
	}
}

// indexChat reads the transcript of a conversation whole and gives its messages to the index.
func (r *Runtime) indexChat(rec wire.Conversation) {
	st, err := os.Stat(rec.TranscriptPath)
	if err != nil || !st.Mode().IsRegular() {
		return
	}
	var fd *feed.Feed
	if st.Size() <= indexWholeMax {
		fd = feed.Everything(rec.ID, rec.Provider, r.d.source(rec), st.Size())
	} else {
		fd = feed.New(rec.ID, rec.Provider, r.d.source(rec))
	}
	fd.Load()
	var docs []msgindex.Doc
	fd.Messages(func(e wire.Entry) {
		if d, ok := messageDoc(rec.ID, e); ok {
			docs = append(docs, d)
		}
	})
	r.d.Index.Replace(rec.ID, st.Size(), docs)
}

// indexScan reads the transcripts the index has not seen at their present size, within the budget; it reports whether
// every conversation is read.
func (r *Runtime) indexScan() (complete bool) {
	start := time.Now()
	complete = true
	var recs []wire.Conversation
	for _, a := range r.all() {
		v := a.last.Load()
		switch {
		case v == nil:
		case v.deleted:
			r.d.Index.Remove(v.rec.ID)
		case v.rec.TranscriptPath != "":
			recs = append(recs, v.rec)
		}
	}
	// The newest chats first: they are the ones searched for.
	slices.SortFunc(recs, func(a, b wire.Conversation) int { return int(b.LastActivityAt - a.LastActivityAt) })
	for _, rec := range recs {
		select {
		case <-r.stop:
			return false
		default:
		}
		st, err := os.Stat(rec.TranscriptPath)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if size, ok := r.d.Index.Size(rec.ID); ok && size == st.Size() {
			continue
		}
		if r.d.Index.Read(rec.ID) && time.Since(st.ModTime()) < indexQuiet {
			continue
		}
		if time.Since(start) > indexBudget {
			return false
		}
		r.indexChat(rec)
		time.Sleep(indexPause)
	}
	return complete
}

// indexer keeps the index up to date for as long as the runtime runs.
func (r *Runtime) indexer() {
	defer r.wg.Done()
	wait := indexFirst
	for {
		select {
		case <-r.stop:
			return
		case <-r.d.Clock.After(wait):
		}
		r.indexed.Store(r.indexScan())
		wait = indexEvery
	}
}

// Indexing reports whether the scan has not yet read every conversation.
func (r *Runtime) Indexing() bool { return r.d.Index != nil && !r.indexed.Load() }

// liveIDs are the conversations that are not deleted.
func (r *Runtime) liveIDs() map[string]wire.Conversation {
	out := map[string]wire.Conversation{}
	for _, a := range r.all() {
		if v := a.last.Load(); v != nil && !v.deleted {
			out[v.rec.ID] = v.rec
		}
	}
	return out
}

// SearchMessages finds the messages that hold the words of the query, in the conversations that are not deleted.
func (r *Runtime) SearchMessages(_ context.Context, query string, limit int) (wire.MessageSearch, error) {
	out := wire.MessageSearch{Hits: []wire.MessageHit{}, Indexing: r.Indexing()}
	if r.d.Index == nil || len([]rune(strings.TrimSpace(query))) < 2 {
		return out, nil
	}
	alive := r.liveIDs()
	// Deleted chats take some of the room; ask for more and keep what is left.
	for _, h := range r.d.Index.Search(query, msgindex.Options{Limit: limit * 2}) {
		if _, ok := alive[h.Conv]; !ok {
			continue
		}
		hit := wire.MessageHit{ConvID: h.Conv, EntryID: h.Entry, At: h.At, Role: wire.MessageUser, Snippet: h.Snippet}
		if h.Role == msgindex.RoleAgent {
			hit.Role = wire.MessageAgent
		}
		if h.Turn != "" {
			turn := h.Turn
			hit.TurnID = &turn
		}
		out.Hits = append(out.Hits, hit)
		if len(out.Hits) >= limit {
			break
		}
	}
	return out, nil
}

// Digest limits of one message in the cut text, in characters.
const (
	digestFirstMax = 2500
	digestUserMax  = 1500
	digestAgentMax = 2500
)

func cut(text string, max int) string {
	text = strings.TrimSpace(text)
	rs := []rune(text)
	if len(rs) <= max {
		return text
	}
	return string(rs[:max]) + "…"
}

func speaker(role string) string {
	if role == msgindex.RoleUser {
		return "Person"
	}
	return "Agent"
}

// ChatDigest cuts a conversation to about budget characters: its title, the first request, and the latest messages
// that fit, the last answer among them; the whole of it is written as Markdown to a file the agent may read.
func (r *Runtime) ChatDigest(ctx context.Context, id string, budget int) (wire.ChatDigest, error) {
	rec, ok := r.liveIDs()[id]
	if !ok {
		return wire.ChatDigest{}, ErrNotFound
	}
	if r.d.Index == nil || r.d.ExportDir == "" {
		return wire.ChatDigest{}, fmt.Errorf("convo: no digest on this runtime")
	}
	if !r.d.Index.Read(id) && rec.TranscriptPath != "" {
		r.indexChat(rec)
	}
	if err := ctx.Err(); err != nil {
		return wire.ChatDigest{}, err
	}
	msgs := r.d.Index.Messages(id)
	path := filepath.Join(r.d.ExportDir, id+".md")
	if err := writeExport(path, rec, msgs); err != nil {
		return wire.ChatDigest{}, err
	}
	text, truncated := digestText(rec, msgs, budget)
	return wire.ChatDigest{ConvID: id, Title: rec.Title, Text: text, Path: path, Truncated: truncated}, nil
}

func writeExport(path string, rec wire.Conversation, msgs []msgindex.Doc) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", rec.Title)
	if rec.Cwd != "" {
		fmt.Fprintf(&b, "Folder: %s\n\n", rec.Cwd)
	}
	for _, m := range msgs {
		fmt.Fprintf(&b, "## %s, %s\n\n%s\n\n", speaker(m.Role), time.UnixMilli(m.At).UTC().Format("2006-01-02 15:04"), strings.TrimSpace(m.Text))
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == b.String() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func digestText(rec wire.Conversation, msgs []msgindex.Doc, budget int) (string, bool) {
	var head strings.Builder
	fmt.Fprintf(&head, "Chat «%s»", rec.Title)
	if rec.Cwd != "" {
		fmt.Fprintf(&head, " (folder %s)", rec.Cwd)
	}
	head.WriteString("\n")
	if len(msgs) == 0 {
		head.WriteString("\nNo messages yet.\n")
		return head.String(), false
	}
	first := -1
	for i, m := range msgs {
		if m.Role == msgindex.RoleUser {
			first = i
			break
		}
	}
	room := budget - len([]rune(head.String()))
	var firstText string
	if first >= 0 {
		firstText = "\nOriginal request:\n" + cut(msgs[first].Text, digestFirstMax) + "\n"
		room -= len([]rune(firstText))
	}
	// The latest messages that fit, newest first; the first request is not told twice.
	var tail []string
	truncated := false
	taken := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if i == first {
			continue
		}
		limit := digestUserMax
		if msgs[i].Role == msgindex.RoleAgent {
			limit = digestAgentMax
		}
		part := fmt.Sprintf("%s: %s\n", speaker(msgs[i].Role), cut(msgs[i].Text, limit))
		if n := len([]rune(part)); n > room {
			truncated = true
			break
		} else {
			room -= n
		}
		tail = append(tail, part)
		taken++
	}
	left := len(msgs) - taken
	if first >= 0 {
		left--
	}
	if left > 0 {
		truncated = true
	}
	slices.Reverse(tail)
	head.WriteString(firstText)
	if len(tail) > 0 {
		head.WriteString("\nLatest messages:\n")
		if left > 0 {
			fmt.Fprintf(&head, "(%d earlier messages are left out)\n", left)
		}
		for _, p := range tail {
			head.WriteString(p)
			head.WriteString("\n")
		}
	}
	return strings.TrimRight(head.String(), "\n") + "\n", truncated
}

// Index is the search index the runtime keeps (nil without one).
func (r *Runtime) Index() *msgindex.Index { return r.d.Index }
