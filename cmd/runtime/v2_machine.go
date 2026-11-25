package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/storelink"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What the live v2 reaches the machine through: the conversations' tmux panes and processes, the uploads' files and
// the machine channel's record. None of it is on the actors' path: the actors ask for it off their goroutines, and
// the machine channel answers from what it holds.

func init() {
	// A writer that held a pane past its term lost it: the journal says which pane, for the hang behind it is a fault.
	typist.OnExpired = func(pane string, held time.Duration) {
		agent.Log("warn", "v2: a pane's lock was let go for a writer that held it too long", agent.F{K: "session", V: pane},
			agent.F{K: "heldMs", V: held.Milliseconds()})
	}
}

// v2Pane is an agent pane in tmux, typed into the way the Legacy queue types.
type v2Pane struct {
	tm      *tmux.Tmux
	session string
	// term and tall read a cut dialog with the page's terminal attached; nil term never does.
	term tmux.Terminal
	tall *tallCaptures
	// held is the hold of the pane's lock the keys are bound to (Bind); nil types with no hold.
	held context.Context
}

// Bind is the pane for one writer's hold: its keys end with the hold (typist.Binder).
func (p v2Pane) Bind(held context.Context) typist.Pane {
	p.held = held
	return p
}

func (p v2Pane) keys() tmux.Keys {
	if p.held == nil {
		return p.tm.With(context.Background())
	}
	return p.tm.With(p.held)
}

func (p v2Pane) CaptureStyled() (string, bool) { return p.tm.CaptureStyled(p.session) }
func (p v2Pane) Capture() (string, bool)       { return p.tm.Capture(p.session) }
func (p v2Pane) Paste(text string) error       { return p.keys().SendBracketedPasteOnly(p.session, text) }
func (p v2Pane) TypeLine(text string) error    { return p.keys().SendLiteralThenEnter(p.session, text) }
func (p v2Pane) TypeOnly(text string) error    { return p.keys().SendLiteral(p.session, text) }
func (p v2Pane) Keys(keys ...string) error     { return p.keys().SendKeySequence(p.session, keys...) }

// Responsive is asked by the typist before the first key of a try (typist.Prober).
func (p v2Pane) Responsive() error { return p.tm.Responsive(p.session) }

// A permission dialog that does not fit its window gets this much: at 197 columns of command, about 50 lines of it.
const (
	dialogCols = 200
	dialogRows = 60
)

func (p v2Pane) GrowForDialog() bool { return p.tm.GrowDetached(p.session, dialogCols, dialogRows) }

// tallEvery is how seldom the person's terminal changes size for a dialog that keeps changing (a countdown in it).
const tallEvery = 15 * time.Second

// tallCaptures keeps the last tall look of every session: a dialog that stands on the screen is read once, the terminal
// under the chat does not jump at each look.
type tallCaptures struct {
	mu   sync.Mutex
	last map[string]tallCapture
}

type tallCapture struct {
	short, tall string
	at          time.Time
}

func (p v2Pane) CaptureTall(short string) (string, bool) {
	if p.tall == nil {
		return "", false
	}
	p.tall.mu.Lock()
	defer p.tall.mu.Unlock()
	last, seen := p.tall.last[p.session]
	if seen && last.short == short {
		return last.tall, true
	}
	if seen && time.Since(last.at) < tallEvery {
		return "", false
	}
	tall, ok := p.tm.CaptureStyledTall(p.term, p.session, dialogCols, dialogRows)
	if !ok {
		return "", false
	}
	if p.tall.last == nil {
		p.tall.last = map[string]tallCapture{}
	}
	p.tall.last[p.session] = tallCapture{short: short, tall: tall, at: time.Now()}
	return tall, true
}

// v2Tmux is convo.Machine over tmux. A pane that looks gone, or an agent that looks exited, is believed only when
// the look before agreed: one call that failed on a loaded machine must not stop a working agent.
type v2Tmux struct {
	tm *tmux.Tmux
	// term is the page's terminal, tall the dialogs read through it.
	term tmux.Terminal
	tall tallCaptures
	// guard says whether the terminal is off: then the pane takes no write while a shell is in its foreground.
	guard *chatguard.Guard
	// nativeOwns verifies the current generation, bound UUID and foreground process before native writes.
	nativeOwns func(session string) bool

	// probe answers the looks; nil means tm.
	probe probeTmux

	mu    sync.Mutex
	doubt map[string]doubt

	// snap is one list-panes of every server, shared by the looks of all conversations for snapEvery: a look per
	// conversation per second, each a tmux client of its own, kept ~100 clients waiting on a loaded server.
	snapMu sync.Mutex
	snap   paneSnap
}

// doubt is what the last look of a session saw; gen is the snapshot a shell was seen in.
type doubt struct {
	noSession, shell bool
	gen              uint64
	// sure: the shell was confirmed by a listing of its own, see freshPanes.
	sure bool
}

// freshPanes is a tmux whose ReadAllPanes may answer from what its servers' events last said; ReadAllPanesNow lists
// the panes at this moment, and is false when there is nothing newer to say.
type freshPanes interface {
	ReadAllPanesNow() (map[string]tmux.PaneInfo, bool)
}

// probeTmux is what a look asks tmux.
type probeTmux interface {
	ReadAllPanes() (map[string]tmux.PaneInfo, bool)
	HasSession(name string) bool
}

type paneSnap struct {
	gen   uint64 // counts the reads: two looks of one gen saw the same moment, not two
	at    time.Time
	panes map[string]tmux.PaneInfo
	ok    bool
}

// snapEvery is how old a shared snapshot may be: the fastest look (a working agent) is once a second.
const snapEvery = time.Second

func (m *v2Tmux) prober() probeTmux {
	if m.probe != nil {
		return m.probe
	}
	return m.tm
}

// panes is the shared snapshot, read again when older than snapEvery; the looks that ask while it is read wait for it
// instead of starting their own.
func (m *v2Tmux) panes() (map[string]tmux.PaneInfo, bool, uint64) {
	m.snapMu.Lock()
	defer m.snapMu.Unlock()
	if m.snap.at.IsZero() || time.Since(m.snap.at) >= snapEvery {
		panes, ok := m.prober().ReadAllPanes()
		m.snap = paneSnap{gen: m.snap.gen + 1, at: time.Now(), panes: panes, ok: ok}
	}
	return m.snap.panes, m.snap.ok, m.snap.gen
}

func (m *v2Tmux) Pane(session string) typist.Pane {
	if session == "" {
		return nil
	}
	// While the terminal is off every write is checked against what is in the pane's foreground: text that reaches a
	// bare shell runs as commands (see internal/chatguard).
	pane := chatguard.Guarded(v2Pane{tm: m.tm, session: session, term: m.term, tall: &m.tall}, m.guard, func() string { return m.tm.PaneCurrentCommand(session) })
	if m.nativeOwns != nil && m.nativeOwns(session) {
		return nativeOwnedPane{Pane: pane, alive: func() bool { return m.nativeOwns(session) }}
	}
	return pane
}

var _ convo.SessionForgetter = (*v2Tmux)(nil)

// ForgetSession drops what the looks kept of a session nobody looks at any more: its last probe and its last tall
// capture. The next look of it, if one ever comes, starts as a first look does.
func (m *v2Tmux) ForgetSession(session string) {
	m.mu.Lock()
	delete(m.doubt, session)
	m.mu.Unlock()
	m.ForgetScreen(session)
}

// ForgetScreen drops the tall capture kept of a session whose terminal ended.
func (m *v2Tmux) ForgetScreen(session string) {
	m.tall.mu.Lock()
	delete(m.tall.last, session)
	m.tall.mu.Unlock()
}

// Probe: what runs in the session. A look that says "no session" or "a shell" is reported only when the previous look
// said so too; the first is reported as the agent still running. Looks that read one snapshot are one look: a pane that
// was a shell a moment ago (the launch line is not typed yet) must not be read twice from the same snapshot and taken
// for an agent that exited, as it was when the agent's first hook came before the next snapshot.
//
// The look reads the shared snapshot; only a session that was there and is missing from it is asked about on its own
// (a pane between two programs may list without a command), so a conversation without a session costs nothing.
func (m *v2Tmux) Probe(session string) convo.Probe {
	m.mu.Lock()
	if m.doubt == nil {
		m.doubt = map[string]doubt{}
	}
	prev := m.doubt[session]
	m.mu.Unlock()
	cmd, has := "", true // tmux that did not answer (a loaded machine) said nothing: not gone, not a shell
	panes, ok, gen := m.panes()
	if ok {
		p, found := panes[session]
		cmd, has = p.Command, found
		if !found && !prev.noSession {
			has = m.prober().HasSession(session)
		}
	}
	shell := tmux.SafeShells[cmd]
	if shell && prev.shell && prev.gen == gen {
		// The same snapshot again is the same look: the first sight of the shell is not confirmed by it.
		return convo.Probe{Session: true, Foreground: true}
	}
	if shell && prev.shell && !prev.sure {
		// Two looks of servers followed by events may be one listing read twice: before the agent is taken for
		// exited, the panes are listed once more.
		if f, can := m.prober().(freshPanes); can {
			if now, listed := f.ReadAllPanesNow(); listed && !tmux.SafeShells[now[session].Command] {
				m.mu.Lock()
				m.doubt[session] = doubt{gen: gen}
				m.mu.Unlock()
				return convo.Probe{Session: true, Foreground: true}
			}
		}
	}
	m.mu.Lock()
	m.doubt[session] = doubt{noSession: !has, shell: shell, gen: gen, sure: shell && prev.shell}
	m.mu.Unlock()
	switch {
	case !has && !prev.noSession:
		return convo.Probe{Session: true, Foreground: true}
	case !has:
		return convo.Probe{}
	case shell && !prev.shell:
		return convo.Probe{Session: true, Foreground: true}
	case shell:
		return convo.Probe{Session: true, Shell: true}
	}
	return convo.Probe{Session: true, Foreground: true}
}

var _ convo.StartProber = (*v2Tmux)(nil)

// ProbeAtStart is the look a start takes, believed at once: the runtime came back to sessions that may all be gone
// (tmux went down with it, a reboot), and the first sight of a missing session that Probe reports as the agent still
// running would leave a chat that was at work waiting for an agent nobody brings back. What it saw is kept
// as Probe's previous look, so the next look of a missing session confirms it at once. A tmux that did not answer says
// nothing, as in Probe.
func (m *v2Tmux) ProbeAtStart(session string) convo.Probe {
	panes, ok, gen := m.panes()
	if !ok {
		return convo.Probe{Session: true, Foreground: true}
	}
	p, found := panes[session]
	has := found || m.prober().HasSession(session)
	shell := found && tmux.SafeShells[p.Command]
	m.mu.Lock()
	if m.doubt == nil {
		m.doubt = map[string]doubt{}
	}
	m.doubt[session] = doubt{noSession: !has, shell: shell, gen: gen}
	m.mu.Unlock()
	switch {
	case !has:
		return convo.Probe{}
	case shell:
		return convo.Probe{Session: true, Shell: true}
	}
	return convo.Probe{Session: true, Foreground: true}
}

// v2Uploads finds the files the page uploaded. A file the page put into the hub's store (wire.HubUpload) is not on any
// machine yet: the machine that runs the chat takes it from the hub into its own uploads when the message names it, so
// it does not matter which machine the hub placed the chat on.
type v2Uploads struct {
	up *wire.Uploads
	// hub is the hub's uploads collection; nil (or a nil answer) for a machine with no hub's store.
	hub func() hubBlobs
}

// hubBlobs reads contents of the hub's uploads collection by sha256 (filesync.Client).
type hubBlobs interface {
	GetBlob(ctx context.Context, sha string, w io.Writer) error
}

// hubUploadWait is how long taking one file from the hub may last.
const hubUploadWait = 30 * time.Second

func (u v2Uploads) Resolve(ids []string) ([]wire.AttachmentRef, []string, wire.RefuseCode) {
	refs, paths := make([]wire.AttachmentRef, 0, len(ids)), make([]string, 0, len(ids))
	for _, id := range ids {
		ref, path, ok := u.up.Get(id)
		if h, kept := wire.ParseHubUpload(id); kept {
			ref, path, ok = u.take(h)
		}
		if !ok {
			return nil, nil, wire.RefuseAttachmentMissing
		}
		refs, paths = append(refs, ref), append(paths, path)
	}
	return refs, paths, ""
}

// take is the machine's copy of a file kept in the hub: the one it has already, else the bytes from the hub, which must
// hash to the name they were asked by.
func (u v2Uploads) take(h wire.HubUpload) (wire.AttachmentRef, string, bool) {
	id := h.LocalID()
	if ref, path, ok := u.up.Get(id); ok {
		return ref, path, true
	}
	var blobs hubBlobs
	if u.hub != nil {
		blobs = u.hub()
	}
	if blobs == nil {
		return wire.AttachmentRef{}, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), hubUploadWait)
	defer cancel()
	var buf bytes.Buffer
	max := wire.DefaultLimits.MaxFileBytes
	if max <= 0 {
		max = filestore.MaxFileSize
	}
	if err := blobs.GetBlob(ctx, h.Sha, &cappedBuffer{buf: &buf, left: max}); err != nil {
		agent.Log("warn", "v2: an attachment is not in the hub's store", agent.F{K: "sha", V: h.Sha}, agent.F{K: "error", V: err.Error()})
		return wire.AttachmentRef{}, "", false
	}
	if sum := sha256.Sum256(buf.Bytes()); hex.EncodeToString(sum[:]) != h.Sha {
		agent.Log("warn", "v2: an attachment from the hub's store does not hash to its name", agent.F{K: "sha", V: h.Sha})
		return wire.AttachmentRef{}, "", false
	}
	typ := h.Mime
	if typ == "" {
		typ = http.DetectContentType(buf.Bytes())
	}
	ref, code := u.up.PutAs(id, h.Name, typ, int64(buf.Len()), bytes.NewReader(buf.Bytes()))
	if code != "" {
		return wire.AttachmentRef{}, "", false
	}
	return u.up.Get(ref.ID)
}

// cappedBuffer refuses what is past its room, so a file bigger than any upload is not read to its end.
type cappedBuffer struct {
	buf  *bytes.Buffer
	left int64
}

func (l *cappedBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, errors.New("the file is bigger than an upload may be")
	}
	l.left -= int64(len(p))
	return l.buf.Write(p)
}

// hubUploads is the hub's uploads collection of the runtime's link to the hub's store, read when it is used: the link
// is opened after the conversations' runtime. nil with no hub.
func hubUploads() hubBlobs {
	l := linked.Load()
	if l == nil || l.Kind() != storelink.KindHub {
		return nil
	}
	return l.Client(filestore.KindUploads)
}

// Keep stores a text the runtime made a file of the message (a long message, see attach.IsLongText).
func (u v2Uploads) Keep(name string, body []byte) (wire.AttachmentRef, string, bool) {
	ref, code := u.up.Put(name, "text/markdown", int64(len(body)), bytes.NewReader(body))
	if code != "" {
		return wire.AttachmentRef{}, "", false
	}
	_, path, ok := u.up.Get(ref.ID)
	return ref, path, ok
}

// v2Log writes the conversations' journal lines.
type v2Log struct{}

func (v2Log) Info(msg string, kv ...any) { v2LogAt("info", msg, kv) }
func (v2Log) Warn(msg string, kv ...any) { v2LogAt("warn", msg, kv) }

func v2LogAt(level, msg string, kv []any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		v := kv[i+1]
		if err, ok := v.(error); ok {
			v = err.Error()
		}
		fs = append(fs, agent.F{K: k, V: v})
	}
	agent.Log(level, "v2: "+msg, fs...)
}

// --- the machine channel ---

// liveMachine is the machine channel of a runtime on its own: always online, its agents' state refreshed behind, its
// load read every 2 s for the sockets that asked. Snapshot never waits for a check: it answers what it holds.
type liveMachine struct {
	dataDir string
	busy    func() int
	check   func() preflight.Result
	models  func(wire.Provider) []wire.AgentModel
	health  *machines.HealthMonitor
	// offers are the profiles a provider's CLI can start a chat on; nil is a machine without profiles.
	offers func(wire.Provider) []wire.ProfileOffer
	// catalog says where a provider's model list came from (GitHub #124); nil: the machine does not say.
	catalog func(wire.Provider) *wire.ModelCatalog
	// codexConfig is Codex's config.toml, whose approval_policy is a Codex chat's default when the runtime skips nothing.
	codexConfig string
	// claudeSettings is Claude Code's settings.json, whose permissions.defaultMode "bypassPermissions" makes a Claude
	// chat's default bypass.
	claudeSettings string
	// skip is the runtime's dangerouslySkipPermissions, the permission mode a chat that names none starts in; nil says
	// nothing of it.
	skip func() bool

	mu      sync.Mutex
	machine wire.Machine
	subs    map[int]func(wire.Event)
	next    int
	// appsTimer holds the one pending apps push: writes of the installed list that come in a burst are told once.
	appsTimer *time.Timer
}

// appsTellDelay is how long a burst of writes of the installed list is gathered into one push.
const appsTellDelay = 120 * time.Millisecond

// appsChanged tells every socket of the machine channel that the installed apps changed, so the pages read them again
// It never blocks the writer: the status goes out from a timer, once for a burst.
func (m *liveMachine) appsChanged() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appsTimer != nil {
		return
	}
	m.appsTimer = time.AfterFunc(appsTellDelay, func() {
		m.mu.Lock()
		m.appsTimer = nil
		ev, subs := m.statusLocked()
		m.mu.Unlock()
		// The machine as it is, with the moment the apps changed: a page of an older build gets it without that moment
		// and takes it for the status it holds (the cap appssync, wire/caps.go).
		ev.AppsAt = time.Now().UnixMilli()
		for _, fn := range subs {
			fn(ev)
		}
	})
}

func newLiveMachine(id, version string, features []wire.Feature, dataDir string, busy func() int, check func() preflight.Result,
	models func(wire.Provider) []wire.AgentModel) *liveMachine {
	host, _ := os.Hostname()
	return &liveMachine{dataDir: dataDir, busy: busy, check: check, models: models, subs: map[int]func(wire.Event){},
		health: machines.NewHealthMonitor(machines.HealthDeps{Path: dataDir}),
		machine: wire.Machine{ID: id, Name: host, Host: host, Reach: wire.ReachOnline, Agents: []wire.MachineAgent{},
			Runtime: &wire.RuntimeInfo{Version: version, Proto: wire.Proto, Features: features}}}
}

var _ wire.MachineSource = (*liveMachine)(nil)

func (m *liveMachine) Snapshot(_ context.Context, load bool) (wire.MachineSnapshot, error) {
	m.mu.Lock()
	snap := m.machine
	snap.Agents = append([]wire.MachineAgent{}, m.machine.Agents...)
	m.mu.Unlock()
	now := time.Now().UnixMilli()
	snap.LastSeenAt = &now
	if load {
		l := m.readLoad()
		snap.Load = &l
	}
	return wire.MachineSnapshot{Machine: snap}, nil
}

func (m *liveMachine) Subscribe(load bool, fn func(wire.Event)) (cancel func()) {
	m.mu.Lock()
	id := m.next
	m.next++
	m.subs[id] = fn
	m.mu.Unlock()
	stop := make(chan struct{})
	if load {
		safego.Go("machine load", func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					fn(wire.MachineLoadEv{Load: m.readLoad()})
				}
			}
		})
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			m.mu.Lock()
			delete(m.subs, id)
			m.mu.Unlock()
		})
	}
}

// readLoad is the machine's load now: the CPU busy share (machines.CPUMeter), memory, the data folder's disk, the agents at work.
func (m *liveMachine) readLoad() wire.MachineLoad {
	h := machines.ReadHostStats()
	l := wire.MachineLoad{At: time.Now().UnixMilli(), MemTotal: int64(h.TotalMem), MemUsed: int64(h.TotalMem - h.FreeMem)}
	if h.CPUs > 0 || h.CPU != nil {
		l.CPU = h.CPUShare()
	}
	if m.busy != nil {
		l.BusyAgents = m.busy()
	}
	var st syscall.Statfs_t
	if syscall.Statfs(filepath.Clean(m.dataDir), &st) == nil {
		l.DiskTotal, l.DiskFree = diskSizes(uint64(st.Blocks), uint64(st.Bavail), uint64(st.Bsize))
	}
	return l
}

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER: a size past it the page cannot read as a whole number.
const maxSafeInteger = 1<<53 - 1

// diskSizes are the disk's total and free bytes from statfs, or 0, 0 («unknown») when the filesystem reports what no
// disk has: a container's overlay can give block counts whose product overflows or passes the page's safe integers,
// and such a frame closed a healthy chat socket (GitHub #119).
func diskSizes(blocks, avail, bsize uint64) (total, free int64) {
	if bsize == 0 || blocks == 0 || avail > blocks || blocks > maxSafeInteger/bsize {
		return 0, 0
	}
	return int64(blocks * bsize), int64(avail * bsize)
}

// agentsNow is the agent CLIs as the preflight finds them, with the models each takes. Both change the model of a
// running conversation: Claude by /model in its box, Codex by a resume on its session with the new launch.
func agentsNow(r preflight.Result, models func(wire.Provider) []wire.AgentModel) []wire.MachineAgent {
	list := func(p wire.Provider) []wire.AgentModel {
		if models == nil {
			return []wire.AgentModel{}
		}
		return models(p)
	}
	claude := wire.MachineAgent{Provider: wire.ProviderClaude, Version: r.Claude.Version, Status: wire.InstallReady,
		Models: list(wire.ProviderClaude), CanSwitchModel: true}
	switch {
	case !r.Claude.Installed:
		claude.Status = wire.InstallMissing
	case !r.Claude.LoggedIn:
		claude.Status = wire.InstallLoginRequired
	}
	codex := wire.MachineAgent{Provider: wire.ProviderCodex, Version: r.Codex.Version, Status: wire.InstallReady,
		Models: list(wire.ProviderCodex), CanSwitchModel: true}
	switch {
	case !r.Codex.Installed:
		codex.Status = wire.InstallMissing
	case !r.Codex.LoggedIn:
		codex.Status = wire.InstallLoginRequired
	}
	result := []wire.MachineAgent{claude, codex}
	if nativeAntigravityEnabled() {
		native := wire.MachineAgent{Provider: wire.ProviderAntigravity, Version: r.Antigravity.Version, Status: wire.InstallReady, Models: list(wire.ProviderAntigravity), CanSwitchModel: agyBridged()}
		if !r.Antigravity.Installed {
			native.Status = wire.InstallMissing
		} else if !r.Antigravity.LoggedIn {
			native.Status = wire.InstallLoginRequired
		}
		result = append(result, native)
	}
	return result
}

// refresh asks the preflight (a login shell, seconds on a loaded machine) and tells the sockets what changed.
func (m *liveMachine) refresh() {
	if m.check == nil {
		return
	}
	agents := m.withCatalog(m.withOffers(agentsNow(m.check(), m.models)))
	if m.skip != nil {
		skip := m.skip()
		for i := range agents {
			agents[i].DefaultPermission = defaultPermissionOf(agents[i].Provider, skip, m.codexConfig, m.claudeSettings)
		}
	}
	m.mu.Lock()
	changed := !reflect.DeepEqual(agents, m.machine.Agents)
	m.machine.Agents = agents
	ev, subs := m.statusLocked()
	m.mu.Unlock()
	if changed {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

// withCatalog adds where each agent's model list came from.
func (m *liveMachine) withCatalog(agents []wire.MachineAgent) []wire.MachineAgent {
	if m.catalog == nil {
		return agents
	}
	for i := range agents {
		agents[i].ModelCatalog = m.catalog(agents[i].Provider)
	}
	return agents
}

// withOffers adds the profiles each agent CLI offers.
func (m *liveMachine) withOffers(agents []wire.MachineAgent) []wire.MachineAgent {
	if m.offers == nil {
		return agents
	}
	for i := range agents {
		agents[i].Profiles = m.offers(agents[i].Provider)
	}
	return agents
}

// republishProfiles tells the sockets that a key was saved or removed: the agents as held, with the profiles anew.
func (m *liveMachine) republishProfiles() {
	m.mu.Lock()
	agents := m.withOffers(append([]wire.MachineAgent{}, m.machine.Agents...))
	changed := !reflect.DeepEqual(agents, m.machine.Agents)
	m.machine.Agents = agents
	ev, subs := m.statusLocked()
	m.mu.Unlock()
	if changed {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

// statusLocked is the machine_status of the record and the sockets to tell it; m.mu is held.
func (m *liveMachine) statusLocked() (wire.MachineStatusEv, []func(wire.Event)) {
	ev := wire.MachineStatusEv{Reach: m.machine.Reach, Runtime: m.machine.Runtime,
		Agents: append([]wire.MachineAgent{}, m.machine.Agents...), Health: m.machine.Health}
	subs := make([]func(wire.Event), 0, len(m.subs))
	for _, fn := range m.subs {
		subs = append(subs, fn)
	}
	return ev, subs
}

// judgeHealth samples the health and tells the sockets when it changed. Every socket of the machine channel hears it,
// with load or without: a page shows the health of the machines it does not have open too.
func (m *liveMachine) judgeHealth() {
	if m.health == nil {
		return
	}
	h := m.health.Sample()
	m.mu.Lock()
	if m.machine.Health != nil && reflect.DeepEqual(*m.machine.Health, h) {
		m.mu.Unlock()
		return
	}
	m.machine.Health = &h
	ev, subs := m.statusLocked()
	m.mu.Unlock()
	for _, fn := range subs {
		fn(ev)
	}
}

// modelsOf is the models the last look found for a provider.
func (m *liveMachine) modelsOf(p wire.Provider) []wire.AgentModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.machine.Agents {
		if a.Provider == p {
			return a.Models
		}
	}
	return nil
}

// run refreshes the agents' state every 30 s and the health every 5 s until ctx ends; the first looks are at once.
// The preflight takes seconds on a loaded machine: the health is judged beside it, never behind it.
func (m *liveMachine) run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	safego.Go("machine health", func() {
		defer wg.Done()
		m.judgeHealth()
		t := time.NewTicker(machines.HealthEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.judgeHealth()
			}
		}
	})
	defer wg.Wait()
	m.refresh()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.refresh()
		}
	}
}

// codexNeverAsksRE is a top-level `approval_policy = "never"` of Codex's config.toml.
var codexNeverAsksRE = regexp.MustCompile(`(?m)^\s*approval_policy\s*=\s*["']never["']`)

// defaultPermissionOf is the permission mode a chat that names none starts in: bypass when the runtime skips
// permissions; a Claude chat asks unless its settings.json makes bypassPermissions the defaultMode (another defaultMode
// does not decide: the launch names ask); a Codex chat runs on its config.toml, which asks unless its approval_policy
// is "never" (read before its first table).
func defaultPermissionOf(p wire.Provider, skip bool, codexConfig, claudeSettings string) wire.PermissionMode {
	if p == wire.ProviderAntigravity {
		return wire.PermissionAsk
	}
	if skip {
		return wire.PermissionBypass
	}
	switch {
	case p == wire.ProviderClaude && claudeSettings != "":
		raw, err := os.ReadFile(claudeSettings)
		if err != nil {
			return wire.PermissionAsk
		}
		var settings struct {
			Permissions struct {
				DefaultMode string `json:"defaultMode"`
			} `json:"permissions"`
		}
		if json.Unmarshal(raw, &settings) == nil && settings.Permissions.DefaultMode == "bypassPermissions" {
			return wire.PermissionBypass
		}
	case p == wire.ProviderCodex && codexConfig != "":
		raw, err := os.ReadFile(codexConfig)
		if err != nil {
			return wire.PermissionAsk
		}
		head, _, _ := strings.Cut(string(raw), "\n[")
		if codexNeverAsksRE.MatchString(head) {
			return wire.PermissionBypass
		}
	}
	return wire.PermissionAsk
}
