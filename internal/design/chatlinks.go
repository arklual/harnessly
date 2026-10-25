package design

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Which chats worked on which design, for chats of any folder: the designs whose files a conversation's agent wrote
// (Write, Edit, MultiEdit, NotebookEdit, apply_patch, a shell command that writes under the design's folder) or that it
// named to the design CLI (`open design <id>`, `design <cmd> <id>`). It is read from the transcripts, so it holds for
// the chats that worked on a design before it was kept anywhere. Each transcript is read once: the index keeps how far
// it got and what it found, grows with the file, and is kept on disk across restarts. A short growth is read when it is
// asked for; a long one (a chat never read, the first run on a machine) is read behind, slowly, by Run.

const (
	chatLinksVersion = 1
	// chatLinksSyncBytes is the most a lookup reads itself; the rest is Run's.
	chatLinksSyncBytes = 512 << 10
	// chatLinksLineMax is the longest line read whole; a longer one (a huge tool result) is skipped.
	chatLinksLineMax = 16 << 20
	// chatLinksPauseEvery and chatLinksPause keep the reading behind the work of the machine.
	chatLinksPauseEvery = 4 << 20
	chatLinksPause      = 15 * time.Millisecond
	chatLinksSaveEvery  = 10 * time.Second
	// chatLinksSubagentsMax bounds the subagent transcripts read per conversation.
	chatLinksSubagentsMax = 200
)

var (
	// writesShell: a shell command that may write a file it names.
	writesShell = regexp.MustCompile(`(?:^|[\s;&|(])(?:cp|mv|rsync|install|tee|touch|mkdir|ln|rm|unzip|tar|patch)\s|>|\bsed\s+(?:-[a-zA-Z]*\s+)*-[a-zA-Z]*i|\bperl\s+-[a-zA-Z]*i|\bwrite_text\(|\bwriteFileSync\(|\bopen\([^)]*['"][wa]`)
	// quietRedirect: a redirect of a stream to another or to nothing, which writes no file of a design.
	quietRedirect = regexp.MustCompile(`[0-9&]?>>?\s*(?:&[0-9-]|/dev/null\b)`)
	cliWords      = regexp.MustCompile(`"[^"]+"|'[^']+'|\S+`)
	patchFileRe   = regexp.MustCompile(`(?m)^\*\*\* (?:Add File|Update File|Delete File|Move to): (.+)$`)
)

type chatLinkFile struct {
	Ino     uint64   `json:"ino"`
	Offset  int64    `json:"offset"`
	Designs []string `json:"designs"`
}

type chatLinksFile struct {
	V     int                      `json:"v"`
	Root  string                   `json:"root"`
	Files map[string]*chatLinkFile `json:"files"`
}

// ChatLinks is the index of the designs the transcripts worked on.
type ChatLinks struct {
	lib  *Library
	file string
	cli  *regexp.Regexp

	mu      sync.Mutex
	st      chatLinksFile
	loaded  bool
	dirty   bool
	queue   []string
	queued  map[string]bool
	reading map[string]*sync.Mutex
	kick    chan struct{}
}

// NewChatLinks is the index of the library's designs, kept in file ("" keeps it in memory only).
func NewChatLinks(lib *Library, file string) *ChatLinks {
	names := []string{"harnessly"}
	if cli := strings.TrimSpace(lib.Config().Texts.CLI); cli != "" && cli != "harnessly" {
		names = append(names, regexp.QuoteMeta(cli))
	}
	// The words after `open design` or `design` up to a flag or the end of the command: a subcommand or two, then the
	// design (`design page create <id> --title …`).
	words := `((?:(?:"[^"]+"|'[^']+'|[^\s;&|<>"'-][^\s;&|<>]*)[ \t]*){1,4})`
	cli := regexp.MustCompile(`(?:^|[\s;&|(/])(?:` + strings.Join(names, "|") + `)\s+(?:open\s+)?design\s+` + words)
	return &ChatLinks{lib: lib, file: file, cli: cli, queued: map[string]bool{}, reading: map[string]*sync.Mutex{},
		kick: make(chan struct{}, 1)}
}

func (c *ChatLinks) roots() []string {
	root := c.lib.Root()
	out := []string{root}
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		out = append(out, real)
	}
	return out
}

// load reads the kept index once; an index of another library folder starts anew. Called with c.mu held.
func (c *ChatLinks) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	root := c.lib.Root()
	c.st = chatLinksFile{V: chatLinksVersion, Root: root, Files: map[string]*chatLinkFile{}}
	if c.file == "" {
		return
	}
	raw, err := os.ReadFile(c.file)
	if err != nil {
		return
	}
	var kept chatLinksFile
	if json.Unmarshal(raw, &kept) != nil || kept.V != chatLinksVersion || kept.Root != root || kept.Files == nil {
		return
	}
	c.st = kept
}

// Save writes the index when it changed.
func (c *ChatLinks) Save() error {
	c.mu.Lock()
	if !c.dirty || c.file == "" {
		c.mu.Unlock()
		return nil
	}
	raw, err := json.Marshal(c.st)
	c.dirty = false
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.file), 0o700); err != nil {
		return err
	}
	tmp := c.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.file)
}

// transcripts are a conversation's transcript and the transcripts of its subagents (Claude keeps them beside it).
func (c *ChatLinks) transcripts(p string) []string {
	if p == "" || !c.lib.allowedTranscript(p) {
		return nil
	}
	out := []string{p}
	dir := strings.TrimSuffix(p, ".jsonl") + string(filepath.Separator) + "subagents"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if len(out) > chatLinksSubagentsMax {
			break
		}
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// Designs are, for each transcript, the designs it worked on (which still exist is the caller's to check), in the
// order it first touched them. What a transcript wrote since the last look is read now when it is short; a long
// growth is left to Run, and the answer is what the index knew so far.
func (c *ChatLinks) Designs(paths []string) map[string][]string {
	out := map[string][]string{}
	for _, p := range paths {
		var designs []string
		for _, f := range c.transcripts(p) {
			designs = appendNew(designs, c.designsOf(f, chatLinksSyncBytes)...)
		}
		if len(designs) > 0 {
			out[p] = designs
		}
	}
	return out
}

// Backfill puts the transcripts on Run's list, to be read behind.
func (c *ChatLinks) Backfill(paths []string) {
	c.mu.Lock()
	for _, p := range paths {
		for _, f := range c.transcripts(p) {
			if !c.queued[f] {
				c.queued[f] = true
				c.queue = append(c.queue, f)
			}
		}
	}
	c.mu.Unlock()
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Run reads the transcripts left for it, one at a time and slowly, and keeps the index on disk; until ctx ends.
func (c *ChatLinks) Run(ctx context.Context) {
	lowerPriority()
	save := time.NewTicker(chatLinksSaveEvery)
	defer save.Stop()
	defer func() { _ = c.Save() }()
	for {
		c.mu.Lock()
		var next string
		if len(c.queue) > 0 {
			next = c.queue[0]
			c.queue = c.queue[1:]
			delete(c.queued, next)
		}
		c.mu.Unlock()
		if next != "" {
			c.read(ctx, next, -1)
			if ctx.Err() != nil {
				return
			}
			continue
		}
		_ = c.Save()
		select {
		case <-ctx.Done():
			return
		case <-c.kick:
		case <-save.C:
		}
	}
}

// designsOf is what the index knows of one transcript, after reading its growth when it is at most budget bytes; a
// longer growth goes to Run.
func (c *ChatLinks) designsOf(p string, budget int64) []string {
	if !c.read(context.Background(), p, budget) {
		c.Backfill([]string{p})
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.st.Files[p]; e != nil {
		return append([]string(nil), e.Designs...)
	}
	return nil
}

// read brings the index of one transcript up to the end of its last whole line. budget < 0 reads it all, slowly (Run);
// else a growth over budget bytes is not read and the answer is false.
func (c *ChatLinks) read(ctx context.Context, p string, budget int64) bool {
	c.mu.Lock()
	c.load()
	fm := c.reading[p]
	if fm == nil {
		fm = &sync.Mutex{}
		c.reading[p] = fm
	}
	c.mu.Unlock()
	fm.Lock()
	defer fm.Unlock()
	f, err := os.Open(p)
	if err != nil {
		return true
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return true
	}
	ino := inode(st)
	c.mu.Lock()
	e := c.st.Files[p]
	if e == nil || e.Ino != ino || e.Offset > st.Size() {
		e = &chatLinkFile{Ino: ino}
	}
	from, designs := e.Offset, append([]string(nil), e.Designs...)
	c.mu.Unlock()
	if from == st.Size() {
		return true
	}
	if budget >= 0 && st.Size()-from > budget {
		return false
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return true
	}
	slow := budget < 0
	used, found := c.scan(ctx, f, slow)
	designs = appendNew(designs, found...)
	c.mu.Lock()
	c.st.Files[p] = &chatLinkFile{Ino: ino, Offset: from + used, Designs: designs}
	c.dirty = true
	c.mu.Unlock()
	return true
}

// scan reads whole lines and answers how many bytes they took and the designs their tool calls touched.
func (c *ChatLinks) scan(ctx context.Context, r io.Reader, slow bool) (int64, []string) {
	br := bufio.NewReaderSize(r, 1<<20)
	roots := c.roots()
	marks := make([][]byte, 0, len(roots)+1)
	for _, root := range roots {
		marks = append(marks, []byte(root+string(filepath.Separator)))
	}
	marks = append(marks, []byte("design"))
	// used: the bytes of the whole lines read; pending: those of the line being read, kept up to chatLinksLineMax.
	var used, pending, sincePause int64
	var found []string
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(line) <= chatLinksLineMax {
				line = append(line, chunk...)
			}
			pending += int64(len(chunk))
			continue
		}
		if err != nil {
			// A partial last line is read again once it is whole.
			return used, found
		}
		n := pending + int64(len(chunk))
		whole := chunk
		if pending > 0 {
			line = append(line, chunk...)
			whole = line
		}
		if n <= chatLinksLineMax && hasAny(whole, marks) {
			found = appendNew(found, c.lineDesigns(whole, roots)...)
		}
		line, pending = line[:0], 0
		used += n
		sincePause += n
		if slow && sincePause >= chatLinksPauseEvery {
			sincePause = 0
			select {
			case <-ctx.Done():
				return used, found
			case <-time.After(chatLinksPause):
			}
		}
	}
}

func hasAny(b []byte, marks [][]byte) bool {
	for _, m := range marks {
		if bytes.Contains(b, m) {
			return true
		}
	}
	return false
}

// The parts of a transcript line a link is read from: Claude's tool uses and Codex's calls.
type linkLine struct {
	Type    string `json:"type"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Payload *struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Input     json.RawMessage `json:"input"`
	} `json:"payload"`
}

type linkToolUse struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"input"`
}

// lineDesigns are the designs one transcript line's tool calls touched.
func (c *ChatLinks) lineDesigns(line []byte, roots []string) []string {
	var l linkLine
	if !bytes.Contains(line, []byte(`"tool_use"`)) && !bytes.Contains(line, []byte(`_call"`)) {
		return nil
	}
	if json.Unmarshal(line, &l) != nil {
		return nil
	}
	var out []string
	switch {
	case l.Type == "assistant" && l.Message != nil:
		var blocks []linkToolUse
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			return nil
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			switch {
			case editTools[b.Name]:
				out = appendNew(out, designUnder(roots, b.Input.FilePath), designUnder(roots, b.Input.NotebookPath))
			case b.Name == sourcesBashTool:
				out = appendNew(out, c.shellDesigns(b.Input.Command, roots)...)
			}
		}
	case l.Type == "response_item" && l.Payload != nil:
		p := l.Payload
		switch p.Type {
		case "function_call":
			out = appendNew(out, c.shellDesigns(codexCommand(p.Arguments), roots)...)
		case "custom_tool_call":
			var input string
			if json.Unmarshal(p.Input, &input) != nil {
				return nil
			}
			if p.Name == "apply_patch" {
				out = appendNew(out, patchDesigns(input, roots)...)
			} else {
				out = appendNew(out, c.shellDesigns(input, roots)...)
				out = appendNew(out, patchDesigns(input, roots)...)
			}
		}
	}
	return out
}

// codexCommand is the shell command of a Codex call (`cmd` or `command`, a string or argv), "" for another call.
func codexCommand(args json.RawMessage) string {
	var s string
	if json.Unmarshal(args, &s) == nil {
		args = json.RawMessage(s)
	}
	var a struct {
		Cmd     json.RawMessage `json:"cmd"`
		Command json.RawMessage `json:"command"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	for _, raw := range []json.RawMessage{a.Cmd, a.Command} {
		if len(raw) == 0 {
			continue
		}
		var one string
		if json.Unmarshal(raw, &one) == nil {
			return one
		}
		var argv []string
		if json.Unmarshal(raw, &argv) == nil {
			return strings.Join(argv, " ")
		}
	}
	return ""
}

func patchDesigns(input string, roots []string) []string {
	var out []string
	for _, m := range patchFileRe.FindAllStringSubmatch(input, -1) {
		out = appendNew(out, designUnder(roots, strings.TrimSpace(m[1])))
	}
	return out
}

// shellDesigns are the designs a command named to the design CLI, and the ones under whose folders it may write.
func (c *ChatLinks) shellDesigns(cmd string, roots []string) []string {
	if cmd == "" {
		return nil
	}
	cmd = quietRedirect.ReplaceAllString(cmd, " ")
	var out []string
	// A subcommand is a word like an id too: only the ids of designs that exist count, and that is the caller's check.
	for _, m := range c.cli.FindAllStringSubmatch(cmd, -1) {
		for _, w := range cliWords.FindAllString(m[1], -1) {
			arg := strings.Trim(w, `"'`)
			switch {
			case filepath.IsAbs(arg):
				out = appendNew(out, designUnder(roots, arg))
			case IsLibraryID(arg):
				out = appendNew(out, arg)
			}
		}
	}
	if writesShell.MatchString(cmd) {
		for _, m := range absolutePathRe.FindAllStringSubmatch(cmd, -1) {
			out = appendNew(out, designUnder(roots, m[1]))
		}
	}
	return out
}

// designUnder is the design whose folder holds an absolute path, "" for none.
func designUnder(roots []string, p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	p = filepath.Clean(p)
	for _, r := range roots {
		if ref := folderUnder(r, p); IsLibraryID(ref) {
			return ref
		}
	}
	return ""
}

// appendNew appends the values not in xs yet, "" aside.
func appendNew(xs []string, vs ...string) []string {
	for _, v := range vs {
		if v != "" && !contains(xs, v) {
			xs = append(xs, v)
		}
	}
	return xs
}

// ChatsOf are, by design, the conversations whose transcripts worked on it (links, from Designs; transcriptOf maps a
// conversation to its transcript), the latest activity first.
func ChatsOf(links map[string][]string, transcriptOf map[string]string, lastActivity map[string]int64) map[string][]string {
	out := map[string][]string{}
	for conv, p := range transcriptOf {
		for _, d := range links[p] {
			out[d] = append(out[d], conv)
		}
	}
	for d := range out {
		ids := out[d]
		sort.SliceStable(ids, func(i, j int) bool {
			if lastActivity[ids[i]] != lastActivity[ids[j]] {
				return lastActivity[ids[i]] > lastActivity[ids[j]]
			}
			return ids[i] < ids[j]
		})
	}
	return out
}
