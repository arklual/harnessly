package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/subagents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// madeBudget is the growth of a transcript the answer reads before it answers; a longer one is read behind.
const madeBudget = 4 << 20

// v2Made answers GET /api/v2/conversations/{id}/made: the documents and boards the conversation made, opened or
// changed (read off its transcript by the sources index), and the designs it is tied to that the person may see.
type v2Made struct {
	record func(conv string) (wire.Conversation, bool)
	// read is sources.Route.ReadBudget.
	read func(path, cwd string, budget int64) (res sources.Result, found, failed, complete bool)
	// designs is the design library; nil when this runtime has none.
	designs func(ctx context.Context) madeDesigns
	// task reads a task of the tracker (its key); nil when this runtime has no tracker.
	task madeTaskReader
	// chats are the machine's chats, to find the ones a chat delegated (their ParentID is it); nil: none are inherited.
	chats func(ctx context.Context) []wire.Conversation
}

// madeDepth is how deep the delegated chats a chat inherits from go: its subchats, theirs, and theirs.
const madeDepth = 3

// madeTaskReader is what the tracker says of one task: its title and its place among epics and subtasks. ok is false
// when it cannot be read (no token, not found, the tracker is down).
type madeTaskReader func(ctx context.Context, key string) (title string, info wire.MadeTaskInfo, ok bool)

const (
	// madeTasksWait is how long the answer waits for the tracker; a task not read by then is listed by its key.
	madeTasksWait = 4 * time.Second
	madeTasksPar  = 6
)

// madeDesigns is the design library as the made block reads it.
type madeDesigns interface {
	// Visible are the designs the person may see, by id.
	Visible() map[string]madeDesign
	// Tied are the designs the conversation is tied to: its folder is inside one, one is attached to it, or its
	// transcripts wrote into one.
	Tied(rec wire.Conversation) []string
}

type madeDesign struct {
	Title     string
	Cover     *string
	Kind      wire.DesignKind
	UpdatedAt wire.Ms
}

func (m v2Made) Made(ctx context.Context, conv string) (wire.ChatMade, error) {
	rec, ok := m.record(conv)
	if !ok {
		return wire.ChatMade{}, convo.ErrNotFound
	}
	out := wire.ChatMade{Items: []wire.MadeItem{}, Complete: true}
	c := madeCollect{m: m, ctx: ctx, out: &out, touched: map[string]sources.MadeLink{}, seen: map[string]bool{}}
	// The chat's own first: a thing it made itself is never shown as a subchat's.
	if err := c.chat(rec, nil); err != nil {
		return wire.ChatMade{}, err
	}
	if m.chats != nil {
		c.kids = map[string][]wire.Conversation{}
		for _, other := range m.chats(ctx) {
			if other.ParentID != nil {
				c.kids[*other.ParentID] = append(c.kids[*other.ParentID], other)
			}
		}
		c.inherit(rec.ID, 1, map[string]bool{rec.ID: true}, nil)
	}
	touched := c.touched
	if m.designs != nil {
		if lib := m.designs(ctx); lib != nil {
			out.Items = append(out.Items, designItems(lib, rec, touched)...)
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].At > out.Items[j].At })
	if len(out.Items) > wire.MadeMax {
		out.Items = out.Items[:wire.MadeMax]
	}
	m.readTasks(ctx, out.Items)
	return out, nil
}

// readTasks gives each task its title and place from the tracker, a few at a time and within madeTasksWait; one the
// tracker did not answer keeps no title (the page words it by its key).
func (m v2Made) readTasks(ctx context.Context, items []wire.MadeItem) {
	if m.task == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, madeTasksWait)
	defer cancel()
	var wg sync.WaitGroup
	gate := make(chan struct{}, madeTasksPar)
	for i := range items {
		if items[i].Kind != wire.MadeTask {
			continue
		}
		wg.Add(1)
		it := &items[i]
		safego.Go("made item", func() {
			defer wg.Done()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				return
			}
			key := it.Ref[strings.LastIndex(it.Ref, "/")+1:]
			type read struct {
				title string
				info  wire.MadeTaskInfo
				ok    bool
			}
			done := make(chan read, 1)
			safego.Go("made task", func() {
				title, info, ok := m.task(ctx, key)
				done <- read{title, info, ok}
			})
			select {
			case r := <-done:
				if !r.ok {
					return
				}
				base := strings.TrimSuffix(it.Ref, "/"+key)
				if r.info.Parent != nil && r.info.Parent.URL == "" {
					r.info.Parent.URL = base + "/" + r.info.Parent.Key
				}
				for j := range r.info.Children {
					r.info.Children[j].URL = base + "/" + r.info.Children[j].Key
				}
				it.Title = r.title
				it.Task = &r.info
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
}

var errMadeRead = errors.New("made: the transcript could not be read")

// madeCollect gathers what a chat made with what its subagents and delegated chats made: the chat that started them
// inherits all of it, each thing marked with who made it.
type madeCollect struct {
	m       v2Made
	ctx     context.Context
	out     *wire.ChatMade
	touched map[string]sources.MadeLink
	// seen are the things listed, by kind and ref: one made twice is listed once, as its first maker's.
	seen map[string]bool
	// kids are the delegated chats of each chat, by its id.
	kids map[string][]wire.Conversation
}

// chat reads one chat's transcript and its subagents' transcripts. from is nil for the chat asked about.
func (c *madeCollect) chat(rec wire.Conversation, from *wire.MadeFrom) error {
	if rec.TranscriptPath == "" {
		return nil
	}
	if err := c.transcript(rec.TranscriptPath, from, from == nil); err != nil {
		return err
	}
	dir := subagents.DirFor(rec.TranscriptPath)
	for _, path := range subagents.Files(rec.TranscriptPath) {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		who := from
		if who == nil {
			who = &wire.MadeFrom{Kind: wire.MadeFromSubagent, ID: strings.TrimPrefix(id, "agent-"), Title: subagentTitle(dir, id)}
		}
		// A subagent's transcript that cannot be read leaves out what it made, not the chat's own.
		_ = c.transcript(path, who, false)
	}
	return nil
}

// transcript adds what one transcript made. A transcript that fails is an error only when strict (the chat's own).
func (c *madeCollect) transcript(path string, from *wire.MadeFrom, strict bool) error {
	res, found, failed, complete := c.m.read(path, "", madeBudget)
	if failed {
		if strict {
			return errMadeRead
		}
		return nil
	}
	if found && !complete {
		c.out.Complete = false
	}
	for _, l := range res.Made {
		if l.Kind == wire.MadeDesign {
			if _, ok := c.touched[l.Ref]; !ok {
				c.touched[l.Ref] = l
			}
			continue
		}
		// A page that did not ask for tasks does not know the kind.
		if l.Kind == wire.MadeTask && !wire.MadeWantsTasks(c.ctx) {
			continue
		}
		key := string(l.Kind) + "\x00" + l.Ref
		if c.seen[key] {
			continue
		}
		c.seen[key] = true
		c.out.Items = append(c.out.Items, wire.MadeItem{Kind: l.Kind, Ref: l.Ref, Title: l.Title, Op: l.Op, At: l.At, From: from})
	}
	return nil
}

// inherit adds what the chat's delegated chats made, theirs too, down to madeDepth; each is marked with the subchat
// of the chat asked about it came through (via; nil at the first level).
func (c *madeCollect) inherit(conv string, depth int, visited map[string]bool, via *wire.MadeFrom) {
	if depth > madeDepth {
		return
	}
	for _, kid := range c.kids[conv] {
		if visited[kid.ID] {
			continue
		}
		visited[kid.ID] = true
		from := via
		if from == nil {
			from = &wire.MadeFrom{Kind: wire.MadeFromSubchat, ID: kid.ID, Title: kid.Title}
		}
		_ = c.chat(kid, from)
		c.inherit(kid.ID, depth+1, visited, from)
	}
}

// subagentTitle is the description Claude Code keeps beside a subagent's transcript; "" without one.
func subagentTitle(dir, id string) string {
	raw, err := os.ReadFile(filepath.Join(dir, id+".meta.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		Description string `json:"description"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return ""
	}
	return strings.TrimSpace(meta.Description)
}

// designItems are the visible designs the conversation touched or is tied to; a design nothing in the transcript
// dates is dated by its last change.
func designItems(lib madeDesigns, rec wire.Conversation, touched map[string]sources.MadeLink) []wire.MadeItem {
	visible := lib.Visible()
	ids := lib.Tied(rec)
	for id := range touched {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	var items []wire.MadeItem
	for _, id := range ids {
		d, ok := visible[id]
		if !ok {
			continue
		}
		kind := d.Kind
		item := wire.MadeItem{Kind: wire.MadeDesign, Ref: id, Title: d.Title, Op: wire.OutputChanged, At: d.UpdatedAt, Cover: d.Cover, DesignKind: &kind}
		if t, did := touched[id]; did {
			item.At, item.Op = t.At, t.Op
		}
		items = append(items, item)
	}
	return items
}

// madeLibrary is the design library of a v2Designly.
type madeLibrary struct {
	d *v2Designly
	// person is whose page asked: their own section of the access snapshot decides what they see.
	person string
}

func (m madeLibrary) Visible() map[string]madeDesign {
	lib := m.d.api.L
	st := m.d.state()
	out := map[string]madeDesign{}
	for _, a := range lib.VisibleToFor(lib.ListArtifacts(), m.person) {
		var cover *string
		if a.Cover != nil && *a.Cover != "" {
			cover = a.Cover
		}
		out[a.ID] = madeDesign{Title: a.Title, Cover: cover, Kind: kindOf(st, lib, a.ID), UpdatedAt: wire.Ms(a.UpdatedAt)}
	}
	return out
}

func (m madeLibrary) Tied(rec wire.Conversation) []string {
	lib := m.d.api.L
	st := m.d.state()
	var ids []string
	// The design the chat was started from and the ones it edited, as the design's own list counts them.
	if id, ok := st.CreatedFrom[rec.ID]; ok && lib.ReadArtifact(id) != nil {
		ids = append(ids, id)
	}
	if m.d.links != nil && rec.TranscriptPath != "" {
		for _, id := range m.d.links.Designs([]string{rec.TranscriptPath})[rec.TranscriptPath] {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
