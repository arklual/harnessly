package chatwork

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Chat is what the observer needs to know of a chat.
type Chat struct {
	ID         string
	Parent     string
	Cwd        string
	CreatedAt  int64 // epoch ms
	Transcript string
}

// Observer registers the work of the chats as their feeds go: a file a tool wrote registers its worktree at once; a
// folder a shell command named, when the command changed git (a commit, a new worktree or branch, a push…); and when a
// turn ends every worktree of the chat is looked at again, together with the chat's own folder if the chat committed
// there. All of it runs on one goroutine off a bounded queue: the feed never waits for git.
type Observer struct {
	Reg  *Registry
	Chat func(id string) (Chat, bool)
	Home string
	Now  func() time.Time

	jobs     chan job
	dropOnce sync.Once

	placesMu sync.Mutex
	places   map[string]placeAt // a folder's worktree, looked up shortly before
}

type placeAt struct {
	p   place
	ok  bool
	got time.Time
}

type job struct {
	conv    string
	strong  []string // files a tool wrote
	weak    []string // folders a command named
	changes bool     // the command changed git: its folders are work
	output  string   // the output of a command that may have made a merge request
	turnEnd bool
	scan    bool
	frame   []byte // a frame of the hub, read on the observer's goroutine
	done    chan struct{}
}

const (
	queueSize = 1024
	placeTTL  = 2 * time.Minute
	maxPlaces = 2000
)

// NewObserver makes an observer; Run starts it.
func NewObserver(reg *Registry, chat func(id string) (Chat, bool), home string) *Observer {
	return &Observer{Reg: reg, Chat: chat, Home: home, Now: time.Now, jobs: make(chan job, queueSize), places: map[string]placeAt{}}
}

// Run handles the queue until done closes.
func (o *Observer) Run(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case j := <-o.jobs:
			o.handle(j)
			if j.done != nil {
				close(j.done)
			}
		}
	}
}

func (o *Observer) push(j job) bool {
	select {
	case o.jobs <- j:
		return true
	default:
		o.dropOnce.Do(func() { jsonlog.Log("warn", "chatwork: the queue is full, a step was not looked at") })
		return false
	}
}

// writeTools are the tools whose summary names the file they wrote.
var writeTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// shellTools are the tools whose summary is a shell command.
var shellTools = map[string]bool{"Bash": true, "exec": true, "command": true}

// mrCommand matches a command whose output may name a merge request the chat made or pushed to.
var mrCommand = regexp.MustCompile(`\bglab\s+mr\s+(create|update)\b|\bgit\b.*\bpush\b|merge_requests`)

// Step takes a tool call of a chat's feed: its tool, its summary, whether it finished and its output. It never waits.
func (o *Observer) Step(conv, tool, summary string, finished bool, output string) {
	if j, ok := o.stepJob(conv, tool, summary, finished, output); ok {
		o.push(j)
	}
}

func (o *Observer) stepJob(conv, tool, summary string, finished bool, output string) (job, bool) {
	switch {
	case writeTools[tool]:
		if p := summaryPath(summary); p != "" {
			return job{conv: conv, strong: []string{p}}, true
		}
	case tool == "apply_patch":
		if ps := absPaths(summary); len(ps) > 0 {
			return job{conv: conv, strong: ps}, true
		}
	case shellTools[tool] && finished:
		cmd := strings.TrimPrefix(summary, "$ ")
		j := job{conv: conv, changes: changesGit(cmd)}
		chat, _ := o.Chat(conv)
		j.weak = commandPaths(cmd, chat.Cwd, o.Home)
		if mrCommand.MatchString(cmd) {
			j.output = output
		}
		if len(j.weak) > 0 || j.output != "" {
			return j, true
		}
	}
	return job{}, false
}

// TurnEnded looks again at every worktree of the chat when its turn is over.
func (o *Observer) TurnEnded(conv string) { o.push(job{conv: conv, turnEnd: true}) }

// Scan looks through a chat's transcript once (see scan.go) and waits until it is done or the time is out; false when
// it did not finish in time (it goes on in the background).
func (o *Observer) Scan(conv string, wait time.Duration) bool {
	if o.Reg.Scanned(conv) {
		return true
	}
	done := make(chan struct{})
	if !o.push(job{conv: conv, scan: true, done: done}) {
		return false
	}
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}

// Frame takes a frame of the runtime's hub (wire.Hub.Tap): the tool steps and the ends of turns of the chats. It is
// called under the hub's lock, so it only looks for the words of such a frame and queues it; the frame is read on the
// observer's goroutine.
func (o *Observer) Frame(ch string, frame []byte) {
	conv, ok := strings.CutPrefix(ch, "conv:")
	if !ok {
		return
	}
	if bytes.Contains(frame, []byte(`"t":"tool"`)) || bytes.Contains(frame, []byte(`"type":"turn_finished"`)) ||
		bytes.Contains(frame, []byte(`"type":"turn_failed"`)) {
		o.push(job{conv: conv, frame: frame})
	}
}

// fromFrame turns a queued frame into the job it stands for.
func (o *Observer) fromFrame(conv string, frame []byte) (job, bool) {
	var f struct {
		E struct {
			Type  string `json:"type"`
			Entry *struct {
				T       string `json:"t"`
				Tool    string `json:"tool"`
				Summary string `json:"summary"`
				OK      *bool  `json:"ok"`
				Output  *struct {
					Text string `json:"text"`
				} `json:"output"`
			} `json:"entry"`
		} `json:"e"`
	}
	if json.Unmarshal(frame, &f) != nil {
		return job{}, false
	}
	switch f.E.Type {
	case "turn_finished", "turn_failed":
		return job{conv: conv, turnEnd: true}, true
	case "step", "entry_update", "entry_replace":
		if e := f.E.Entry; e != nil && e.T == "tool" {
			out := ""
			if e.Output != nil {
				out = e.Output.Text
			}
			return o.stepJob(conv, e.Tool, e.Summary, e.OK != nil, out)
		}
	}
	return job{}, false
}

func (o *Observer) handle(j job) {
	if j.frame != nil {
		var ok bool
		if j, ok = o.fromFrame(j.conv, j.frame); !ok {
			return
		}
	}
	chat, ok := o.Chat(j.conv)
	if !ok {
		chat = Chat{ID: j.conv}
	}
	switch {
	case j.scan:
		o.scan(chat)
	case j.turnEnd:
		o.turnEnd(chat)
	default:
		o.step(chat, j)
	}
}

func (o *Observer) since(chat Chat) int64 {
	if chat.CreatedAt > 0 {
		return chat.CreatedAt
	}
	return o.Now().UnixMilli()
}

// locate is the package's locate, remembered for a while per folder: a chat writes many files of one folder.
func (o *Observer) locate(path string) (place, bool) {
	dir := existingDir(path)
	if dir == "" {
		return place{}, false
	}
	now := o.Now()
	o.placesMu.Lock()
	if c, ok := o.places[dir]; ok && now.Sub(c.got) < placeTTL {
		o.placesMu.Unlock()
		return c.p, c.ok
	}
	o.placesMu.Unlock()
	p, ok := locate(dir)
	o.placesMu.Lock()
	if len(o.places) >= maxPlaces {
		clear(o.places)
	}
	o.places[dir] = placeAt{p: p, ok: ok, got: now}
	o.placesMu.Unlock()
	return p, ok
}

// known is the chat's record of a worktree, if any.
func (o *Observer) known(conv, worktree string) (Entry, bool) {
	for _, e := range o.Reg.Of(conv) {
		if e.Worktree == worktree {
			return e, true
		}
	}
	return Entry{}, false
}

// register looks at a worktree and records it for the chat. A worktree named only by a command (byCommand) becomes
// work once the chat has committed there: a command run in a checkout is no change of it by itself.
func (o *Observer) register(chat Chat, p place, source string, byCommand bool) {
	e, ok := o.known(chat.ID, p.worktree)
	if !ok {
		e = Entry{Conv: chat.ID, Repo: p.repo, Worktree: p.worktree, Since: o.since(chat), Source: source}
	}
	e.Parent = chat.Parent
	look(&e)
	if !ok && byCommand && len(e.Commits) == 0 {
		return
	}
	e.At = o.Now().UnixMilli()
	o.Reg.Put(e)
}

func (o *Observer) step(chat Chat, j job) {
	for _, path := range j.strong {
		p, ok := o.locate(path)
		if !ok {
			continue
		}
		if e, known := o.known(chat.ID, p.worktree); known {
			if !e.Dirty { // a write makes the worktree dirty; the rest waits for the end of the turn
				e.Dirty = true
				e.At = o.Now().UnixMilli()
				o.Reg.Put(e)
			}
			continue
		}
		o.register(chat, p, hubproto.ChatWorkLive, false)
	}
	for _, path := range j.weak {
		p, ok := o.locate(path)
		if !ok {
			continue
		}
		if _, known := o.known(chat.ID, p.worktree); known || j.changes {
			o.register(chat, p, hubproto.ChatWorkLive, true)
		}
	}
	if j.output != "" {
		o.noteMR(chat.ID, j.output)
	}
}

// noteMR gives a merge request a command printed to the chat's record of the same project looked at last.
func (o *Observer) noteMR(conv, output string) {
	for _, e := range o.Reg.Of(conv) { // newest look first
		if mr := mrOfProject(output, e.Remote); mr != "" {
			if e.MR != mr {
				e.MR = mr
				e.At = o.Now().UnixMilli()
				o.Reg.Put(e)
			}
			return
		}
	}
}

func (o *Observer) turnEnd(chat Chat) {
	seen := map[string]bool{}
	for _, e := range o.Reg.Of(chat.ID) {
		seen[e.Worktree] = true
		e.Parent = chat.Parent
		look(&e)
		e.At = o.Now().UnixMilli()
		o.Reg.Put(e)
	}
	// The chat's own folder: work only when the chat committed there.
	if chat.Cwd == "" {
		return
	}
	p, ok := o.locate(chat.Cwd)
	if !ok || seen[p.worktree] {
		return
	}
	o.register(chat, p, hubproto.ChatWorkLive, true)
}

// Subtree are the chat and the chats delegated from it, at any depth, of the given list.
func Subtree(root string, chats []Chat) []string {
	out := []string{root}
	for i := 0; i < len(out) && i < 1000; i++ {
		for _, c := range chats {
			if c.Parent == out[i] && !slices.Contains(out, c.ID) {
				out = append(out, c.ID)
			}
		}
	}
	return out
}
