package apps

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
)

// The tools a view of an app offers from its page (tools.page), and what it publishes for the agent to read
// (context.publish). A view registers them with hl.tools.register or the standard navigator.modelContext.registerTool;
// the page that shows the view reports them here while the view is up, and the agent of the chat has each as a tool of
// the host's server under its own name (windowtools.go); page_tools and page_call stay for older instructions. A call waits here until the page takes it, hands it to the view's
// window and brings the answer back. The tools are memory only: a tool is as alive as the view that offers it. The calls
// are commands of a journal on the disk (pagecommands.go).

const (
	// PageToolsTTL is how long a report holds without being renewed: the page renews it every few seconds while the view
	// is up, so a closed view's tools are gone soon after.
	PageToolsTTL = 15 * time.Second
	// PageCallTTL is how long a call waits for the view's answer.
	PageCallTTL           = 60 * time.Second
	MaxPageTools          = 32
	MaxPageToolSchema     = 16 << 10
	MaxPageToolArgs       = 64 << 10
	MaxPageToolResult     = 256 << 10
	MaxPageContext        = 64 << 10
	MaxPageCallsPerChat   = 8
	maxPageToolDescLength = 1000
)

var (
	// ErrPageToolArgs: a report or a call past the limits, or a tool of no valid name.
	ErrPageToolArgs = errors.New("apps: bad page tool")
	// ErrPageToolGone: no view of the app offers that tool now.
	ErrPageToolGone = errors.New("apps: the view that offers this tool is not open")
	// ErrPageCallBusy: too many calls of the chat wait for the page.
	ErrPageCallBusy = errors.New("apps: too many page calls waiting in this chat")
	// ErrPageCallGone: no such waiting call.
	ErrPageCallGone = errors.New("apps: no such page call")
	// ErrPageCallTimeout: the page did not answer in time (the view was closed, or the tool hung).
	ErrPageCallTimeout = errors.New("apps: the view did not answer")
)

var pageToolNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// PageTool is a tool a view offers, as the view described it.
type PageTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	// Confirm: the page asks the person before it runs the tool; a refusal comes back as the call's error.
	Confirm bool `json:"confirm,omitempty"`
}

// PageReport is what the page reports for one app of a chat.
type PageReport struct {
	App     string          `json:"app"`
	Tools   []PageTool      `json:"tools"`
	Context json.RawMessage `json:"context,omitempty"`
	// Hooks: a view of the app hears the agent's tool calls before they run (hooks.agent).
	Hooks bool `json:"hooks,omitempty"`
}

// PageCall is a call the page takes to the view.
type PageCall struct {
	ID        string          `json:"id"`
	App       string          `json:"app"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	// Hook: not a tool of the view but the agent's tool call, for the view's hook to stop or let through.
	Hook bool `json:"hook,omitempty"`
	// ExpiresAt is when the command's term runs out, in milliseconds; Attempt how many times it was handed out.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
	Attempt   int   `json:"attempt,omitempty"`
}

// PageAnswer is the view's answer: a result, or an error in words.
type PageAnswer struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type pageEntry struct {
	PageReport
	at time.Time
}

// openCommand is a command in memory: its row, and what wakes those who wait for its end.
type openCommand struct {
	PageCommand
	hook bool
	over chan struct{}
	// waiters are the callers waiting for the command's end now; left is when the last of them went, in ms.
	waiters int
	left    int64
}

// orphan says nobody waits for the command's end; the lock is held.
func (c *openCommand) orphan() bool { return c.waiters == 0 }

// ms0 is a duration in the journal's milliseconds.
func ms0(d time.Duration) int64 { return d.Milliseconds() }

// PageTools is the page tools of the machine, by chat and app.
type PageTools struct {
	mu    sync.Mutex
	chats map[string]map[string]pageEntry
	// cmds are the commands that are not over and those that ended lately, by id; keys the ids by chat and the
	// caller's idempotency key; seen when each page of a chat asked for commands last.
	cmds map[string]*openCommand
	keys map[string]string
	seen map[string]time.Time
	log  *commandLog
	made int
	// answered is when the last call of an app's view in a chat was answered, by chat and app.
	answered map[string]time.Time
	// Now, CallTTL and ToolWait (WindowToolWait when 0) are for tests; so are Resend and Reack (PageCallResend and
	// PageCallReack when 0).
	Now      func() time.Time
	CallTTL  time.Duration
	ToolWait time.Duration
	Resend   time.Duration
	Reack    time.Duration
}

// AgentAfter is how long after a view answers the agent's call of its tool the view's loud calls still count as the
// agent's: the page asks the person about them, and the runtime runs them only with the person's yes.
const AgentAfter = 30 * time.Second

func NewPageTools() *PageTools {
	return &PageTools{chats: map[string]map[string]pageEntry{}, cmds: map[string]*openCommand{}, keys: map[string]string{},
		seen: map[string]time.Time{}, answered: map[string]time.Time{}}
}

func (p *PageTools) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func checkReport(r PageReport) error {
	if !idRe.MatchString(r.App) || len(r.Tools) > MaxPageTools || len(r.Context) > MaxPageContext {
		return ErrPageToolArgs
	}
	if len(r.Context) > 0 && !json.Valid(r.Context) {
		return ErrPageToolArgs
	}
	seen := map[string]bool{}
	for _, t := range r.Tools {
		if !pageToolNameRe.MatchString(t.Name) || seen[t.Name] || len(t.Description) > maxPageToolDescLength || len(t.InputSchema) > MaxPageToolSchema {
			return ErrPageToolArgs
		}
		if len(t.InputSchema) > 0 && !json.Valid(t.InputSchema) {
			return ErrPageToolArgs
		}
		seen[t.Name] = true
	}
	return nil
}

// Report replaces what a chat's page offers for the apps it names; the caller has checked the apps are on in the chat
// and hold their rights (Allowed). An app the page shows no more is dropped when its report runs out (PageToolsTTL).
func (p *PageTools) Report(conv string, reports []PageReport) error {
	for _, r := range reports {
		if err := checkReport(r); err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	apps := p.chats[conv]
	if apps == nil {
		apps = map[string]pageEntry{}
		p.chats[conv] = apps
	}
	for _, r := range reports {
		apps[r.App] = pageEntry{PageReport: r, at: p.now()}
	}
	return nil
}

// Offered is what the views of a chat offer now, by app.
func (p *PageTools) Offered(conv string) []PageReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.offered(conv)
}

// offered is Offered with the lock held.
func (p *PageTools) offered(conv string) []PageReport {
	var out []PageReport
	for app, e := range p.chats[conv] {
		if p.now().Sub(e.at) > PageToolsTTL {
			delete(p.chats[conv], app)
			continue
		}
		out = append(out, e.PageReport)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

// Hooked says whether a view of the app in the chat hears the agent's tool calls now.
func (p *PageTools) Hooked(conv, app string) bool {
	for _, r := range p.Offered(conv) {
		if r.App == app && r.Hooks {
			return true
		}
	}
	return false
}

func (p *PageTools) offers(conv, app, tool string) bool {
	for _, r := range p.Offered(conv) {
		if r.App != app {
			continue
		}
		for _, t := range r.Tools {
			if t.Name == tool {
				return true
			}
		}
	}
	return false
}

// OpenCommands gives the page tools their journal on the disk (PageCommandsFile in the runtime's data folder) and takes
// up what a runtime before this one left: a command that is not over and still within its term goes on being delivered,
// one past its term is expired, and those that ended lately are remembered for the proxy's repeat of the call.
func (p *PageTools) OpenCommands(path string) error {
	log, err := openCommandLog(path)
	if err != nil {
		return err
	}
	now := p.now()
	rows, err := log.load(ms(now.Add(-PageCommandsKept)))
	if err != nil {
		_ = log.close()
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.log != nil {
		_ = p.log.close()
	}
	p.log = log
	for _, row := range rows {
		c := &openCommand{PageCommand: *row, over: make(chan struct{})}
		p.cmds[c.ID] = c
		if c.Key != "" {
			p.keys[c.Conv+"\x00"+c.Key] = c.ID
		}
		switch {
		case CommandOver(c.Status):
			close(c.over)
		case ms(now) >= c.Expires:
			p.end(c, CommandExpired, nil, "its term ran out while the runtime was away")
		default:
			// Its caller went with the runtime that was: it has PageCallOrphan to come back with its key.
			c.left = ms(now)
			logCommand(&c.PageCommand, "", "taken up after a restart of the runtime")
		}
	}
	return nil
}

// CloseCommands closes the journal's file.
func (p *PageTools) CloseCommands() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.log == nil {
		return nil
	}
	err := p.log.close()
	p.log = nil
	return err
}

// save writes a command's row; the lock is held. A journal that cannot be written does not stop the call: the command
// lives in memory as it did before there was a journal, and the machine's journal says so.
func (p *PageTools) save(c *openCommand) {
	if p.log == nil || c.hook {
		return
	}
	if err := p.log.save(&c.PageCommand); err != nil {
		journal.Add(journal.Entry{Level: journal.Warn, Source: JournalSource(c.App), Event: "app.command", Chat: c.Conv, App: c.App,
			Tool: c.Tool, Text: "command " + c.ID + ": the journal could not be written: " + err.Error()})
	}
}

// move changes a command's status that is not a final one; the lock is held.
func (p *PageTools) move(c *openCommand, status, note string) {
	from := c.Status
	c.Status = status
	p.save(c)
	if !c.hook {
		logCommand(&c.PageCommand, from, note)
	}
}

// end ends a command once; the lock is held. A command that is over is left as it is.
func (p *PageTools) end(c *openCommand, status string, result json.RawMessage, words string) bool {
	if CommandOver(c.Status) {
		return false
	}
	from := c.Status
	c.Status, c.Result, c.Error, c.Ended = status, result, words, ms(p.now())
	p.save(c)
	if !c.hook {
		logCommand(&c.PageCommand, from, words)
	}
	// However the call ends (an answer, a timeout, the agent gone), the view may still act on it a while.
	p.answered[c.Conv+"\x00"+c.App] = p.now()
	close(c.over)
	return true
}

// expire ends the commands of a chat whose term ran out; the lock is held.
func (p *PageTools) expire(conv string) {
	now := ms(p.now())
	for _, c := range p.cmds {
		if c.Conv != conv || CommandOver(c.Status) {
			continue
		}
		switch {
		case now >= c.Expires:
			p.end(c, CommandExpired, nil, "the window did not answer within its term")
		case c.orphan() && c.Status != CommandAcked && now-c.left >= ms0(PageCallOrphan):
			// A window that has the command answers for it; one that has not will not get it.
			p.end(c, CommandCanceled, nil, "the caller left and did not come back")
		}
	}
}

// forget drops the commands that ended long ago; the lock is held.
func (p *PageTools) forget() {
	before := ms(p.now().Add(-PageCommandsKept))
	for id, c := range p.cmds {
		if c.Ended > 0 && c.Ended < before {
			delete(p.cmds, id)
			if c.Key != "" && p.keys[c.Conv+"\x00"+c.Key] == id {
				delete(p.keys, c.Conv+"\x00"+c.Key)
			}
		}
	}
	if p.log != nil {
		p.log.prune(before)
	}
}

// report is what an app's window reports now; the lock is held.
func (p *PageTools) report(conv, app string) (PageReport, bool) {
	for _, r := range p.offered(conv) {
		if r.App == app {
			return r, true
		}
	}
	return PageReport{}, false
}

// screenOf is what names the screen of a window in a command's condition: its route, or its title when it has none.
func screenOf(r PageReport) string {
	c, ok := readWindowContext(r.Context)
	if !ok || c.Screen == nil {
		return ""
	}
	if c.Screen.Route != "" {
		return oneLine(c.Screen.Route, windowBriefDesc)
	}
	return oneLine(c.Screen.Title, windowBriefDesc)
}

func offerOf(r PageReport) string {
	names := make([]string, 0, len(r.Tools))
	for _, t := range r.Tools {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(strings.Join(names, "\x00")))
	return hex.EncodeToString(sum[:6])
}

// Call puts a call to a view's tool and waits for its answer, the context's end or PageCallTTL.
func (p *PageTools) Call(ctx context.Context, conv, app, tool string, args json.RawMessage) (PageAnswer, error) {
	if !p.offers(conv, app, tool) {
		return PageAnswer{}, ErrPageToolGone
	}
	ttl := p.CallTTL
	if ttl == 0 {
		ttl = PageCallTTL
	}
	return p.put(ctx, PageCall{App: app, Tool: tool, Arguments: args}, conv, ttl)
}

// Hook hands the agent's tool call to the view of the app that hears them and waits for its word, at most ttl. The
// view acts in the agent's turn while it decides, so its loud calls count as the agent's, as a tool call's do. A hook
// is not a command of the journal: it is asked once and its word is needed at once or not at all.
func (p *PageTools) Hook(ctx context.Context, conv, app string, call json.RawMessage, ttl time.Duration) (PageAnswer, error) {
	if !p.Hooked(conv, app) {
		return PageAnswer{}, ErrPageToolGone
	}
	return p.put(ctx, PageCall{App: app, Arguments: call, Hook: true}, conv, ttl)
}

// Resume finds the command of a call the caller makes again with the same idempotency key (its proxy lost the answer
// when the runtime restarted) and waits for its end as the first call did; found is false when the chat has no command
// of that key.
func (p *PageTools) Resume(ctx context.Context, conv string) (PageAnswer, PageCommand, error, bool) {
	k := callKeyOf(ctx)
	if k.key == "" {
		return PageAnswer{}, PageCommand{}, nil, false
	}
	p.mu.Lock()
	c := p.cmds[p.keys[conv+"\x00"+k.key]]
	p.mu.Unlock()
	if c == nil {
		return PageAnswer{}, PageCommand{}, nil, false
	}
	a, err := p.wait(ctx, c)
	p.mu.Lock()
	row := c.PageCommand
	p.mu.Unlock()
	return a, row, err, true
}

// put puts a command for the chat's page and waits for its end, the context's end or its term.
func (p *PageTools) put(ctx context.Context, pc PageCall, conv string, ttl time.Duration) (PageAnswer, error) {
	args := pc.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if len(args) > MaxPageToolArgs || !json.Valid(args) {
		return PageAnswer{}, ErrPageToolArgs
	}
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	now := p.now()
	c := &openCommand{hook: pc.Hook, over: make(chan struct{}), PageCommand: PageCommand{
		ID: hex.EncodeToString(raw), Conv: conv, App: pc.App, Tool: pc.Tool, Args: args, Status: CommandQueued,
		Created: ms(now), Expires: ms(now.Add(ttl)),
	}}
	p.mu.Lock()
	if !pc.Hook {
		c.Key = callKeyOf(ctx).key
		if r, ok := p.report(conv, pc.App); ok {
			c.Screen, c.Offer = screenOf(r), offerOf(r)
		}
	}
	p.expire(conv)
	n := 0
	for _, o := range p.cmds {
		if o.Conv == conv && !CommandOver(o.Status) {
			n++
		}
	}
	if n >= MaxPageCallsPerChat {
		p.mu.Unlock()
		return PageAnswer{}, ErrPageCallBusy
	}
	// Written before the page may take it.
	p.save(c)
	p.cmds[c.ID] = c
	if c.Key != "" {
		p.keys[conv+"\x00"+c.Key] = c.ID
	}
	if !c.hook {
		logCommand(&c.PageCommand, "", "")
	}
	if p.made++; p.made%64 == 0 {
		p.forget()
	}
	p.mu.Unlock()
	return p.wait(ctx, c)
}

// wait waits for a command's end and says how it ended. The caller that leaves cancels a command it gave no key for;
// one with a key is left to its term, for the caller's proxy asks again after a restart of the runtime.
func (p *PageTools) wait(ctx context.Context, c *openCommand) (PageAnswer, error) {
	p.mu.Lock()
	c.waiters++
	c.left = 0
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if c.waiters--; c.waiters == 0 {
			c.left = ms(p.now())
		}
		p.mu.Unlock()
	}()
	timer := time.NewTimer(time.UnixMilli(c.Expires).Sub(p.now()))
	defer timer.Stop()
	select {
	case <-c.over:
	case <-ctx.Done():
		p.mu.Lock()
		if c.Key == "" || c.hook {
			p.end(c, CommandCanceled, nil, "the caller left")
		}
		over := CommandOver(c.Status)
		p.mu.Unlock()
		if !over {
			return PageAnswer{}, ctx.Err()
		}
	case <-timer.C:
		p.mu.Lock()
		p.end(c, CommandExpired, nil, "the window did not answer within its term")
		p.mu.Unlock()
	}
	if c.hook {
		// A hook's call is forgotten with its end.
		p.mu.Lock()
		delete(p.cmds, c.ID)
		p.mu.Unlock()
	}
	p.mu.Lock()
	status, result, words := c.Status, c.Result, c.Error
	p.mu.Unlock()
	switch status {
	case CommandDone:
		return PageAnswer{Result: result}, nil
	case CommandFailed:
		return PageAnswer{Error: words}, nil
	case CommandRejected:
		return PageAnswer{}, ErrPageCallRejected{Why: words}
	case CommandExpired:
		return PageAnswer{}, ErrPageCallTimeout
	}
	if err := ctx.Err(); err != nil {
		return PageAnswer{}, err
	}
	return PageAnswer{}, context.Canceled
}

// Driving says whether the agent of a chat is using an app's view: a call of the view's tool waits, or was answered
// less than AgentAfter ago.
func (p *PageTools) Driving(conv, app string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.cmds {
		if c.Conv == conv && c.App == app && !CommandOver(c.Status) {
			return true
		}
	}
	at, ok := p.answered[conv+"\x00"+app]
	if ok && p.now().Sub(at) >= AgentAfter {
		delete(p.answered, conv+"\x00"+app)
		return false
	}
	return ok
}

// Take hands a page that does not acknowledge (an older page) the commands of a chat nobody took yet; each goes to
// such a page once.
func (p *PageTools) Take(conv string) []PageCall { return p.TakeBy(conv, "", false) }

// unmet says why a command's condition does not hold now; "" when it holds or cannot be told. Only what the window
// reports now counts: a window that has not reported since the runtime started is not taken for closed.
func (p *PageTools) unmet(c *openCommand) string {
	if c.hook {
		return ""
	}
	r, ok := p.report(c.Conv, c.App)
	if !ok {
		return ""
	}
	if !slices.ContainsFunc(r.Tools, func(t PageTool) bool { return t.Name == c.Tool }) {
		return "the window of " + c.App + " offers the tool " + c.Tool + " no more"
	}
	if now := screenOf(r); c.Screen != "" && now != "" && now != c.Screen {
		return "the window of " + c.App + " shows the screen " + now + " now, and the call was made for the screen " + c.Screen
	}
	return ""
}

// TakeBy hands a chat's page its commands. page names the page (a tab of the browser), acks says it acknowledges what
// it takes (Ack). A command goes out when nobody took it yet; and again, to a page that acknowledges, when the page that
// took it did not acknowledge it within PageCallResend or has not answered it within PageCallReack, and that page is
// this one or has been silent for PageGone. A command whose term ran out or whose condition holds no more is ended and
// not handed out.
func (p *PageTools) TakeBy(conv, page string, acks bool) []PageCall {
	resend, reack := p.Resend, p.Reack
	if resend == 0 {
		resend = PageCallResend
	}
	if reack == 0 {
		reack = PageCallReack
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if page != "" {
		p.seen[conv+"\x00"+page] = now
	}
	p.expire(conv)
	out := []PageCall{}
	for _, c := range p.cmds {
		if c.Conv != conv || CommandOver(c.Status) {
			continue
		}
		// A command whose caller is gone waits for the caller to come back: nobody would hear its answer, and the
		// agent may have been stopped.
		if c.orphan() {
			continue
		}
		note := ""
		switch c.Status {
		case CommandQueued:
		case CommandSent, CommandAcked:
			// An older page ran what it took, or lost it: there is no telling, so it is not given again to anyone.
			if !acks || c.Legacy || c.hook {
				continue
			}
			since, wait := c.Sent, resend
			if c.Status == CommandAcked {
				wait = reack
			}
			if now.Sub(time.UnixMilli(since)) < wait {
				continue
			}
			if c.Page != page {
				if last, ok := p.seen[conv+"\x00"+c.Page]; ok && now.Sub(last) < PageGone {
					continue
				}
			}
			note = "handed out again"
		}
		// The condition is checked until the window has the command: after that the window's own answer decides.
		if c.Status != CommandAcked {
			if why := p.unmet(c); why != "" {
				p.end(c, CommandRejected, nil, why)
				continue
			}
		}
		c.Sent, c.Page, c.Legacy = ms(now), page, !acks
		c.Deliveries++
		if c.Status == CommandQueued {
			p.move(c, CommandSent, note)
		} else {
			p.save(c)
			logCommand(&c.PageCommand, c.Status, note)
		}
		out = append(out, PageCall{ID: c.ID, App: c.App, Tool: c.Tool, Arguments: c.Args, Hook: c.hook, ExpiresAt: c.Expires, Attempt: c.Deliveries})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Ack is the page's word that it has a command and is about to run it. The status that comes back is the command's
// now; dead says it is over, and the page must not run it.
func (p *PageTools) Ack(conv, id, page string) (status string, dead bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cmds[id]
	if !ok || c.Conv != conv {
		return "", true, ErrPageCallGone
	}
	p.expire(conv)
	// Over, or its caller is gone: not to be run now. A caller that comes back has it handed out again.
	if CommandOver(c.Status) || c.orphan() {
		return c.Status, true, nil
	}
	c.Page = page
	if c.Status != CommandAcked {
		c.Acked = ms(p.now())
		p.move(c, CommandAcked, "")
	}
	return c.Status, false, nil
}

// Answer brings a view's answer to the command that waits for it; conv must be the command's chat. An answer to a
// command that is over is dropped: ErrPageCallDead, unless it only repeats the answer that ended it.
func (p *PageTools) Answer(conv, id string, a PageAnswer) error {
	if len(a.Result) > MaxPageToolResult || (len(a.Result) > 0 && !json.Valid(a.Result)) || len(a.Error) > 2000 {
		return ErrPageToolArgs
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cmds[id]
	if !ok || c.Conv != conv {
		return ErrPageCallGone
	}
	if !CommandOver(c.Status) && ms(p.now()) >= c.Expires {
		p.end(c, CommandExpired, nil, "the window did not answer within its term")
	}
	switch c.Status {
	case CommandDone, CommandFailed:
		return nil
	case CommandRejected, CommandExpired, CommandCanceled:
		logCommand(&c.PageCommand, c.Status, "an answer that came after the end was dropped")
		return ErrPageCallDead
	}
	if a.Error != "" {
		p.end(c, CommandFailed, nil, a.Error)
	} else {
		result := a.Result
		if len(result) == 0 {
			result = json.RawMessage("null")
		}
		p.end(c, CommandDone, result, "")
	}
	return nil
}

// Command is a command's row as it is now; false when the chat has none of that id.
func (p *PageTools) Command(conv, id string) (PageCommand, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cmds[id]
	if !ok || c.Conv != conv {
		return PageCommand{}, false
	}
	return c.PageCommand, true
}

// PageApp is the manifest of an app whose page may offer tools and context in a chat: a builtin app's (Sinopia runs in
// the host's page wherever a document is open, and is on in no chat's list), or an installed app on in the chat. nil
// for any other.
func PageApp(id string, enabled []string, reg *Registry) *Manifest {
	if m := builtinOf(id); m != nil {
		return m
	}
	if reg == nil || !slices.Contains(enabled, id) {
		return nil
	}
	in, err := reg.Get(id)
	if err != nil {
		return nil
	}
	return in.Manifest
}

// PageReportAllowed keeps of a report what the app may offer: tools need tools.page, the context context.publish.
func PageReportAllowed(m *Manifest, r PageReport) PageReport {
	if !m.Allows("tools.page") {
		r.Tools = nil
	}
	if !m.Allows("context.publish") {
		r.Context = nil
	}
	if !m.Allows("hooks.agent") {
		r.Hooks = false
	}
	return r
}
