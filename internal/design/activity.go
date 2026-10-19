package design

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jsvalue"
	"harnessly/internal/transcript"
)

// Who works on which design: the paths each agent conversation's transcript names, read through an index per
// transcript that grows with the file (a poll costs only the bytes written since the previous one).

const (
	maxSourceIndexes  = 64
	sourceChunkBytes  = 8 * 1024 * 1024
	touchedTTL        = 4 * time.Second
	inferredEditMs    = 10 * 60_000
	sourcesBashTool   = "Bash"
	sourcesCodexMarks = "/.codex/sessions/"
)

var (
	absolutePathRe = regexp.MustCompile(`(?:^|[\s'"=(])(/(?:[\w.@+-]+/)*[\w.@+-]+)`)
	hiddenWorkPath = regexp.MustCompile(`^/(proc|sys|dev|tmp/claude-[^/]+/[^/]+/tasks)\b`)
	editTools      = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}
)

type sourceIndex struct {
	ino    uint64
	offset int64
	paths  []string
	seen   map[string]bool
	edited []string
	editAt map[string]float64
	used   int64
}

type sourcesState struct {
	mu      sync.Mutex
	indexes map[string]*sourceIndex
	clock   int64
	file    map[string]*sync.Mutex
	touched *touchedEntry
}

type touchedEntry struct {
	at  time.Time
	key string
	v   touched
}

type touched struct {
	order      []string
	editsOrder []string
	refs       map[string][]string
	edits      map[string][]boardEdit
}

type boardEdit struct {
	ref, board string
	at         float64
}

func str(o *jsvalue.Object, k string) string {
	s, _ := get(o, k).(string)
	return s
}

func addWorkPaths(ix *sourceIndex, entries []*jsvalue.Object) {
	add := func(p string) {
		if !ix.seen[p] {
			ix.seen[p] = true
			ix.paths = append(ix.paths, p)
		}
	}
	for _, e := range entries {
		switch str(e, "type") {
		case "tool-call":
			if fp := str(e, "filePath"); fp != "" {
				add(fp)
			}
			if str(e, "toolName") == sourcesBashTool {
				for _, m := range absolutePathRe.FindAllStringSubmatch(str(e, "summary"), -1) {
					add(m[1])
				}
			}
		case "patch-apply":
			for _, f := range e.Array("files") {
				if fo, ok := f.(*jsvalue.Object); ok {
					add(str(fo, "path"))
				}
			}
		case "exec-command-stream":
			if cwd := str(e, "cwd"); cwd != "" {
				add(cwd)
			}
			for _, m := range absolutePathRe.FindAllStringSubmatch(str(e, "command"), -1) {
				add(m[1])
			}
		}
	}
}

// addEditedPaths folds in the files the entries wrote or edited, newest first within a read, keeping each one's
// latest write.
func addEditedPaths(ix *sourceIndex, entries []*jsvalue.Object) {
	type touchedFile struct {
		path string
		at   float64
	}
	var files []*touchedFile
	byPath := map[string]*touchedFile{}
	add := func(p string, at float64) {
		if p == "" {
			return
		}
		if f := byPath[p]; f != nil {
			if at > f.at {
				f.at = at
			}
			return
		}
		f := &touchedFile{path: p, at: at}
		byPath[p] = f
		files = append(files, f)
	}
	for _, e := range entries {
		ts, _ := e.Number("timestamp")
		switch {
		case str(e, "type") == "tool-call" && str(e, "filePath") != "" && editTools[str(e, "toolName")]:
			add(str(e, "filePath"), ts)
		case str(e, "type") == "patch-apply":
			for _, f := range e.Array("files") {
				if fo, ok := f.(*jsvalue.Object); ok {
					add(str(fo, "path"), ts)
				}
			}
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].at > files[j].at })
	for _, f := range files {
		prev, ok := ix.editAt[f.path]
		if !ok {
			ix.edited = append(ix.edited, f.path)
		}
		ix.editAt[f.path] = maxf(f.at, prev)
	}
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// allowedTranscript: only a transcript of the agents' own folders is read.
func (l *Library) allowedTranscript(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil || !strings.HasSuffix(abs, ".jsonl") || strings.Contains(p, "\x00") {
		return false
	}
	for _, dir := range []string{l.cfg.ClaudeProjects, l.cfg.CodexSessions} {
		if dir != "" && strings.HasPrefix(abs, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// conversationSources are the work paths (newest first) and the edited files of a transcript.
func (l *Library) conversationSources(p string) (paths []string, edits []boardEditPath, ok bool) {
	if p == "" || !l.allowedTranscript(p) {
		return nil, nil, false
	}
	s := &l.sources
	s.mu.Lock()
	if s.file == nil {
		s.file, s.indexes = map[string]*sync.Mutex{}, map[string]*sourceIndex{}
	}
	fm := s.file[p]
	if fm == nil {
		fm = &sync.Mutex{}
		s.file[p] = fm
	}
	s.mu.Unlock()
	fm.Lock()
	defer fm.Unlock()
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, nil, false
	}
	ino := inode(st)
	s.mu.Lock()
	ix := s.indexes[p]
	if ix == nil || ix.ino != ino || ix.offset > st.Size() {
		ix = &sourceIndex{ino: ino, seen: map[string]bool{}, editAt: map[string]float64{}}
		s.indexes[p] = ix
	}
	s.clock++
	ix.used = s.clock
	for len(s.indexes) > maxSourceIndexes {
		oldest, at := "", int64(-1)
		for k, v := range s.indexes {
			if at < 0 || v.used < at {
				oldest, at = k, v.used
			}
		}
		delete(s.indexes, oldest)
	}
	s.mu.Unlock()
	codex := strings.Contains(p, sourcesCodexMarks)
	for ix.offset < st.Size() {
		n := min(int64(sourceChunkBytes), st.Size()-ix.offset)
		buf := make([]byte, n)
		read, _ := f.ReadAt(buf, ix.offset)
		if read <= 0 {
			break
		}
		buf = buf[:read]
		last := -1
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				last = i
				break
			}
		}
		if last < 0 {
			if n < sourceChunkBytes {
				break
			}
			ix.offset += int64(read)
			continue
		}
		content := string(buf[:last+1])
		var entries []*jsvalue.Object
		if codex {
			entries = transcript.ParseCodexContent(content, transcript.CodexOptions{})
		} else {
			entries = transcript.ParseContent(content)
		}
		addWorkPaths(ix, entries)
		addEditedPaths(ix, entries)
		ix.offset += int64(last + 1)
	}
	for i := len(ix.paths) - 1; i >= 0; i-- {
		if !hiddenWorkPath.MatchString(ix.paths[i]) {
			paths = append(paths, ix.paths[i])
		}
	}
	for _, e := range ix.edited {
		edits = append(edits, boardEditPath{path: e, at: ix.editAt[e]})
	}
	return paths, edits, true
}

type boardEditPath struct {
	path string
	at   float64
}

// folderUnder is the first folder under root a path lies in.
func folderUnder(root, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return strings.Split(rel, string(filepath.Separator))[0]
}

// boardOfEditedPath is the project and board a file <library>/<id>/project/<Board>.dc.html is.
func boardOfEditedPath(root, target string) (ref, board string, ok bool) {
	if !filepath.IsAbs(target) {
		return "", "", false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 || parts[1] != ProjectDirname || !IsLibraryID(parts[0]) || !IsBoardFile(parts[2]) {
		return "", "", false
	}
	return parts[0], BoardTitleOf(parts[2]), true
}

func (l *Library) scanTouched(known map[string]bool, chats []Chat) touched {
	root := l.Root()
	t := touched{refs: map[string][]string{}, edits: map[string][]boardEdit{}}
	for _, c := range chats {
		if !agentPanels[c.PanelType] || c.Transcript == "" {
			continue
		}
		paths, edits, ok := l.conversationSources(c.Transcript)
		if !ok {
			continue
		}
		var refs []string
		seen := map[string]bool{}
		for _, p := range paths {
			if !filepath.IsAbs(p) {
				continue
			}
			if ref := folderUnder(root, p); ref != "" && IsLibraryID(ref) && known[ref] && !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
		if len(refs) > 0 {
			if _, had := t.refs[c.TabID]; !had {
				t.order = append(t.order, c.TabID)
			}
			for _, r := range refs {
				if !contains(t.refs[c.TabID], r) {
					t.refs[c.TabID] = append(t.refs[c.TabID], r)
				}
			}
		}
		var written []boardEdit
		for _, e := range edits {
			if ref, board, ok := boardOfEditedPath(root, e.path); ok && known[ref] {
				written = append(written, boardEdit{ref: ref, board: board, at: e.at})
			}
		}
		if len(written) > 0 {
			if _, had := t.edits[c.TabID]; !had {
				t.editsOrder = append(t.editsOrder, c.TabID)
			}
			t.edits[c.TabID] = written
		}
	}
	return t
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (l *Library) touchedByTab(known map[string]bool, chats []Chat) touched {
	keys := make([]string, 0, len(known))
	for k := range known {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	key := strings.Join(keys, "\n")
	s := &l.sources
	s.mu.Lock()
	if c := s.touched; c != nil && c.key == key && time.Since(c.at) < touchedTTL {
		s.mu.Unlock()
		return c.v
	}
	s.mu.Unlock()
	v := l.scanTouched(known, chats)
	s.mu.Lock()
	s.touched = &touchedEntry{at: time.Now(), key: key, v: v}
	s.mu.Unlock()
	return v
}

var stateOrder = map[string]int{"busy": 0, "needs-input": 1, "ready-for-review": 2, "idle": 3}

func orderOf(state string) int {
	if v, ok := stateOrder[state]; ok {
		return v
	}
	return 9
}

func (l *Library) workspaceNames() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(l.cfg.DataDir, "workspaces.json"))
	if err != nil {
		return out
	}
	v, err := jsvalue.Parse(b)
	if err != nil {
		return out
	}
	o, _ := v.(*jsvalue.Object)
	for _, w := range o.Array("workspaces") {
		if wo, ok := w.(*jsvalue.Object); ok {
			if id, ok := get(wo, "id").(string); ok {
				if name, ok := get(wo, "name").(string); ok {
					out[id] = name
				}
			}
		}
	}
	return out
}

// AttachAgents puts on every artifact the agent conversations whose transcripts name its files (selfTab marks the
// one that asked), and as inferred claims the boards a working chat wrote lately without claiming them.
func (l *Library) AttachAgents(artifacts []*Artifact, selfTab string) error {
	known := map[string]bool{}
	for _, a := range artifacts {
		known[a.ID] = true
	}
	if len(known) == 0 {
		return nil
	}
	tabs, _ := l.Tabs()
	clients := map[string]Chat{}
	for _, t := range tabs {
		clients[t.TabID] = t
	}
	names := l.workspaceNames()
	t := l.touchedByTab(known, tabs)
	byArtifact := map[string][]Agent{}
	for _, tabID := range t.order {
		c, ok := clients[tabID]
		if !ok {
			continue
		}
		var ws *string
		if n, ok := names[c.WorkspaceID]; ok {
			ws = &n
		}
		var last *float64
		if c.LastEventAt != 0 {
			v := c.LastEventAt
			last = &v
		}
		agent := Agent{TabID: tabID, Title: c.Title(), Workspace: ws, State: c.State, LastEventAt: last, Self: tabID == selfTab}
		for _, ref := range t.refs[tabID] {
			byArtifact[ref] = append(byArtifact[ref], agent)
		}
	}
	inferred, err := l.inferredClaims(t, clients, float64(nowMs()))
	if err != nil {
		return err
	}
	for _, a := range artifacts {
		agents := byArtifact[a.ID]
		sort.SliceStable(agents, func(i, j int) bool {
			x, y := agents[i], agents[j]
			if x.Self != y.Self {
				return x.Self
			}
			if orderOf(x.State) != orderOf(y.State) {
				return orderOf(x.State) < orderOf(y.State)
			}
			return lastOf(x) > lastOf(y)
		})
		if agents == nil {
			agents = []Agent{}
		}
		a.Agents = agents
		a.Claims = inferred[a.ID]
		if a.Claims == nil {
			a.Claims = []ClaimView{}
		}
	}
	return nil
}

func lastOf(a Agent) float64 {
	if a.LastEventAt == nil {
		return 0
	}
	return *a.LastEventAt
}

var workingStates = map[string]bool{"busy": true, "needs-input": true}

func (l *Library) inferredClaims(t touched, clients map[string]Chat, now float64) (map[string][]ClaimView, error) {
	out := map[string][]ClaimView{}
	var working []string
	for _, tabID := range t.editOrder() {
		if c, ok := clients[tabID]; ok && workingStates[c.State] {
			working = append(working, tabID)
		}
	}
	if len(working) == 0 {
		return out, nil
	}
	id, err := l.Identity()
	if err != nil {
		return nil, err
	}
	for _, tabID := range working {
		c := clients[tabID]
		for _, w := range t.edits[tabID] {
			if now-w.at > inferredEditMs {
				continue
			}
			mine := out[w.ref]
			skip := false
			for _, k := range mine {
				if k.Board == w.board && float64(k.Since) >= w.at {
					skip = true
				}
			}
			if skip {
				continue
			}
			var kept []ClaimView
			for _, k := range mine {
				if k.Board != w.board {
					kept = append(kept, k)
				}
			}
			out[w.ref] = append(kept, ClaimView{Board: w.board, Person: id.Person, TabID: tabID, ChatTitle: c.Title(), Since: round(w.at), Local: true, Inferred: true})
		}
	}
	return out, nil
}

// editOrder are the tabs with edits in the order they were read.
func (t touched) editOrder() []string { return t.editsOrder }
