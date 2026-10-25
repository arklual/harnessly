package timeline

import (
	"context"
	"math"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

func contextBackground() context.Context { return context.Background() }

// snapshotPending marks a socket whose first screen is still being read: it is sent nothing until the screen says where
// it ends.
const snapshotPending = math.MaxInt64

// fileWatcher follows one transcript for every socket showing it.
type fileWatcher struct {
	path    string
	offset  int64
	pending string
	order   []*client
	behind  map[*client]int64
	session string
	prov    *Provider
	codex   *transcript.CodexParser

	watcher         *fsnotify.Watcher
	stopWatch       chan struct{}
	debounce        *time.Timer
	retries         int
	summaryResolved bool
	processing      bool
	pendingChange   bool
	removed         bool
}

func (fw *fileWatcher) has(c *client) bool {
	for _, x := range fw.order {
		if x == c {
			return true
		}
	}
	return false
}

func (fw *fileWatcher) remove(c *client) {
	for i, x := range fw.order {
		if x == c {
			fw.order = append(fw.order[:i], fw.order[i+1:]...)
			break
		}
	}
	delete(fw.behind, c)
}

func sessionIDOfPath(p string) string {
	if id := jscompat.Basename(p, ".jsonl"); agents.IsUUID(id) {
		return id
	}
	return ""
}

// readSessionStats is the session statistics the init carries: a Codex transcript's own, a Claude session's from the
// statusline figures; nil (null) when none.
func (s *Server) readSessionStats(path, sessionID string, codex bool) any {
	if codex {
		if st := transcript.CodexTimelineStats(path); st != nil {
			return st
		}
		return nil
	}
	if sessionID == "" || !agents.IsUUID(sessionID) || s.d.StatsDir == "" {
		return nil
	}
	raw, err := readFile(filepath.Join(s.d.StatsDir, sessionID+".json"))
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	return v
}

func optString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// subscribeAndUpdateSummary shows a transcript and stores it, with the conversation's title, on the tab.
func (s *Server) subscribeAndUpdateSummary(c *client, path, sessionID, session string, p *Provider) {
	jsonlSummary := s.subscribeToFile(c, path, sessionID, session, p)
	summary := s.resolveSummary(p, session, jsonlSummary)
	id := any(nil)
	if sessionID != "" {
		id = sessionID
	} else if fromPath := p.SessionIDFromPath(path); fromPath != "" {
		id = fromPath
	}
	s.effect(func() { s.d.Layout.AgentState(session, p.ID, id, path, summary) })
}

// resolveSummary is the conversation's title: the pane's, else the transcript's; nil for none.
func (s *Server) resolveSummary(p *Provider, session string, jsonlSummary *string) any {
	if t := p.ParsePaneTitle(s.d.Tmux.PaneTitle(session)); t != nil {
		return *t
	}
	return optString(jsonlSummary)
}

func (s *Server) setAgentSessionID(session string, p *Provider, id any) {
	s.effect(func() { s.d.Layout.AgentSessionID(session, p.ID, id) })
}

func (s *Server) noteLastUserMessage(session, message string) {
	s.effect(func() {
		s.d.Layout.LastUserMessage(session, message)
		if k := s.keeper(); k != nil {
			k.NotifyLastUserMessage(session, message)
		}
	})
}

// subscribeToFile sends the socket the transcript's first screen and follows the file; the transcript's own title, if
// it names one, is returned.
func (s *Server) subscribeToFile(c *client, path, sessionID, session string, p *Provider) *string {
	if !exists(path) {
		sendJSON(c, msg("type", "timeline:init", "entries", []any{}, "sessionId", sessionID, "totalEntries", 0.0, "startByteOffset", 0.0,
			"hasMore", false, "jsonlPath", path))
		return nil
	}
	fw := s.files[path]
	isNew := fw == nil
	if isNew {
		if len(s.files) >= maxWatchers {
			sendJSON(c, timelineError("max-watchers", "Too many active watchers"))
			return nil
		}
		fw = &fileWatcher{path: path, behind: map[*client]int64{}, session: session, prov: p}
		if p.ID == agents.Codex || s.isCodexPath(path) {
			fw.codex = transcript.NewCodexParser(path, s.d.Codex)
		}
		s.files[path] = fw
	}
	if !fw.has(c) {
		fw.order = append(fw.order, c)
	}
	fw.behind[c] = snapshotPending

	var res transcript.Chunk
	switch {
	case fw.codex != nil && isNew:
		res = fw.codex.ParseTail(maxInitEntries)
	case fw.codex != nil:
		res = transcript.ReadCodexTail(path, maxInitEntries, s.d.Codex)
	default:
		res = transcript.ReadTail(path, maxInitEntries)
	}
	if res.ErrorCount > 0 {
		sendJSON(c, timelineError("parse-error", "JSONL parsing: "+jscompat.Number(float64(res.ErrorCount))+" errors (lines skipped)"))
	}
	if isNew {
		fw.offset = res.FileSize
		s.startFileWatch(fw)
	}
	var first *string
	if res.HasMore {
		first = readFirstTimestamp(path)
	}
	meta, err := computeInitMeta(res.Entries, res.FileSize, first, res.CustomTitle)
	if err != nil {
		// The old runtime's subscribe threw here: nothing more was sent.
		s.d.Log("warn", "timeline: entry time is not a date", "path", path)
		return nil
	}
	resolved := sessionID
	if resolved == "" {
		resolved = p.SessionIDFromPath(path)
	}
	stats := s.readSessionStats(path, resolved, fw.codex != nil)
	entries := make([]any, len(res.Entries))
	for i, e := range res.Entries {
		entries[i] = e
	}
	init := msg("type", "timeline:init", "entries", entries, "sessionId", resolved, "totalEntries", float64(len(res.Entries)),
		"startByteOffset", float64(res.StartByteOffset), "hasMore", res.HasMore, "jsonlPath", path)
	if res.Summary != nil {
		init.Set("summary", *res.Summary)
	}
	init.Set("meta", meta)
	init.Set("sessionStats", stats)
	sendJSON(c, init)

	if session != "" {
		if last := findLastUserMessage(res.Entries); last != "" {
			s.noteLastUserMessage(session, last)
		}
	}
	// What the transcript gained while the first screen was read and sent goes out now, not with the next write.
	if fw.has(c) {
		fw.behind[c] = res.FileSize
		s.processFileChange(fw)
	}
	return res.Summary
}

func (s *Server) unsubscribe(c *client, path string) {
	fw := s.files[path]
	if fw == nil {
		return
	}
	fw.remove(c)
	if len(fw.order) == 0 {
		s.removeFileWatcher(path)
	}
}

func (s *Server) removeFileWatcher(path string) {
	fw := s.files[path]
	if fw == nil {
		return
	}
	fw.removed = true
	s.stopFileWatch(fw)
	if fw.debounce != nil {
		fw.debounce.Stop()
	}
	if fw.codex != nil {
		fw.codex.Dispose()
	}
	delete(s.files, path)
}

func (s *Server) stopFileWatch(fw *fileWatcher) {
	if fw.watcher != nil {
		close(fw.stopWatch)
		go fw.watcher.Close()
		fw.watcher = nil
	}
}

// startFileWatch watches the transcript; a change is read 50 ms after the last of a burst. A failing watch is set up
// again up to three times, then the sockets are told.
func (s *Server) startFileWatch(fw *fileWatcher) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	if err := w.Add(fw.path); err != nil {
		_ = w.Close()
		return
	}
	stop := make(chan struct{})
	fw.watcher, fw.stopWatch = w, stop
	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				s.mu.Lock()
				if !fw.removed && fw.watcher == w {
					if fw.debounce != nil {
						fw.debounce.Stop()
					}
					fw.debounce = time.AfterFunc(s.d.Timing.FileDebounce, func() {
						s.mu.Lock()
						defer s.mu.Unlock()
						if !fw.removed {
							s.processFileChange(fw)
						}
					})
				}
				s.mu.Unlock()
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
				s.mu.Lock()
				if !fw.removed && fw.watcher == w {
					s.watchFailed(fw)
				}
				s.mu.Unlock()
				return
			}
		}
	}()
}

func (s *Server) watchFailed(fw *fileWatcher) {
	if fw.retries < maxWatchRetries {
		fw.retries++
		s.stopFileWatch(fw)
		time.AfterFunc(s.d.Timing.WatchRetry, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !fw.removed {
				s.startFileWatch(fw)
			}
		})
		return
	}
	raw := marshal(timelineError("watcher-failed", "File watch failed (retries exceeded)"))
	if cur := s.files[fw.path]; cur != nil {
		for _, c := range cur.order {
			if c.canSend() {
				c.send(raw)
			}
		}
	}
}

// processFileChange reads what the transcript gained, sends it, and tells the tab of a new prompt and its title.
func (s *Server) processFileChange(fw *fileWatcher) {
	if fw.processing {
		fw.pendingChange = true
		return
	}
	fw.processing = true
	defer func() {
		fw.processing = false
		if fw.pendingChange {
			fw.pendingChange = false
			s.processFileChange(fw)
		}
	}()
	prev := fw.offset
	newEntries := s.pushTranscriptChange(fw)
	if len(newEntries) > 0 {
		if last := findLastUserMessage(newEntries); last != "" {
			s.noteLastUserMessage(fw.session, last)
		}
		if !fw.summaryResolved && hasType(newEntries, "assistant-message") {
			fw.summaryResolved = true
			if summary := s.resolveSummary(fw.prov, fw.session, nil); summary != nil {
				session, prov := fw.session, fw.prov
				s.effect(func() { s.d.Layout.AgentSummary(session, prov.ID, summary) })
			}
		}
	}
	if fw.codex != nil && fw.offset != prev {
		if st := transcript.CodexTimelineStats(fw.path); st != nil {
			raw := marshal(msg("type", "timeline:stats-update", "sessionStats", st))
			for _, c := range fw.order {
				if c.canSend() {
					c.send(raw)
				}
			}
		}
	}
}

func hasType(entries []*jsvalue.Object, t string) bool {
	for _, e := range entries {
		if entryType(e) == t {
			return true
		}
	}
	return false
}

func parsedEnd(offset int64, pending string) int64 { return offset - int64(len(pending)) }

func appendMessage(entries []*jsvalue.Object) []byte {
	list := make([]any, len(entries))
	for i, e := range entries {
		list[i] = e
	}
	return marshal(msg("type", "timeline:append", "entries", list))
}

// pushTranscriptChange reads what the transcript gained and sends it to the feed's sockets. A socket that cannot take
// a message now (too far behind, its first screen not out yet) keeps a mark and later gets everything from the mark
// on, so no entry is ever missing from its timeline.
func (s *Server) pushTranscriptChange(fw *fileWatcher) []*jsvalue.Object {
	var inc transcript.Incremental
	if fw.codex != nil {
		inc = fw.codex.ParseIncremental()
	} else {
		inc = transcript.ParseIncremental(fw.path, fw.offset, fw.pending)
	}
	from := parsedEnd(fw.offset, fw.pending)
	if inc.NewOffset < fw.offset {
		from = 0 // a file that shrank was written anew and read from its start
	}
	fw.offset, fw.pending = inc.NewOffset, inc.PendingBuffer
	to := parsedEnd(inc.NewOffset, inc.PendingBuffer)
	var message []byte
	for _, c := range append([]*client(nil), fw.order...) {
		if !c.open() {
			continue
		}
		mark, behind := fw.behind[c]
		if !behind {
			mark = from
		}
		if mark >= to {
			continue
		}
		if c.buffered() >= backpressure {
			fw.behind[c] = mark
			continue
		}
		delete(fw.behind, c)
		if mark == from {
			if len(inc.NewEntries) == 0 {
				continue
			}
			if message == nil {
				message = appendMessage(inc.NewEntries)
			}
			c.send(message)
			continue
		}
		entries, ok := s.readRange(fw, mark, to)
		if !ok {
			if _, again := fw.behind[c]; !again {
				fw.behind[c] = mark
			}
			continue
		}
		if len(entries) > 0 && c.open() {
			c.send(appendMessage(entries))
		}
	}
	return inc.NewEntries
}

// readRange is the entries of the whole lines between two line starts.
func (s *Server) readRange(fw *fileWatcher, from, to int64) ([]*jsvalue.Object, bool) {
	if to-from <= 0 {
		return nil, true
	}
	raw, err := readAt(fw.path, from, to-from)
	if err != nil {
		return nil, false
	}
	content := decodeUTF8(raw)
	if s.isCodexPath(fw.path) {
		return transcript.ParseCodexContentAt(content, from, s.d.Codex), true
	}
	return transcript.ParseContentAt(content, from), true
}
