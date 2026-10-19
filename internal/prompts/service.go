package prompts

import (
	"errors"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/screen"
	"harnessly/internal/tmux"
)

const (
	// settleDelay: after a stop the prompt box takes a moment to come back; typing earlier lands in the old turn.
	settleDelay = 700 * time.Millisecond
	retryDelay  = 1500 * time.Millisecond
	// blockedRetry: a menu or foreign text in the box, or an agent not resumed yet, is looked at again this late.
	blockedRetry = 5 * time.Second
	// watchdogEvery: a queued message never waits on a missed transition; tabs with a queue are looked at this often.
	watchdogEvery = 4 * time.Second
	// steerHandOver: Claude writes a steered message to its transcript and screen within this long after Enter.
	steerHandOver = 300 * time.Millisecond
	// mirrorKeySettle: the TUI redraws after a mirrored key within this long.
	mirrorKeySettle = 250 * time.Millisecond
	// composerIDMax is the longest composer message id kept.
	composerIDMax = 64
)

// Config wires a Service.
type Config struct {
	Panes     Panes
	Tabs      Tabs
	Agents    Agents
	Events    Events
	Broadcast Broadcaster
	// Clock is the real clock when nil.
	Clock Clock
	// QueueFile keeps the queue across restarts ("" in memory only).
	QueueFile string
	Editor    EditorConfig
	// Procs reads the process table (the machine's when its functions are nil).
	Procs Procs
	// ReadTranscript reads a transcript's tail as lines (ReadTranscriptTail when nil).
	ReadTranscript func(path string) ([]string, bool)
	// IsUploadImage says whether a path is a picture this machine stored for uploads; nil refuses every picture.
	IsUploadImage func(path string) bool
	// IsShell says whether a pane's foreground program is only its shell (tmux.SafeShells when nil).
	IsShell func(command string) bool
	// Log writes a log line with key/value pairs; nil logs nothing.
	Log func(level, msg string, kv ...any)
}

// Service is the prompt queue, its delivery and every route that types into an agent's pane.
type Service struct {
	cfg     Config
	clock   Clock
	queue   *Queue
	reg     *Registry
	enter   *EnterClock
	flights *flights
	editor  *editor

	mu       sync.Mutex
	timers   map[string]Timer
	watchdog Timer
	running  bool
}

// New builds a Service and loads the queue. Start begins delivering.
func New(cfg Config) *Service {
	if cfg.Clock == nil {
		cfg.Clock = SystemClock{}
	}
	if cfg.Log == nil {
		cfg.Log = func(string, string, ...any) {}
	}
	if cfg.ReadTranscript == nil {
		cfg.ReadTranscript = ReadTranscriptTail
	}
	if cfg.IsShell == nil {
		cfg.IsShell = func(c string) bool { return tmux.SafeShells[c] }
	}
	if cfg.IsUploadImage == nil {
		cfg.IsUploadImage = func(string) bool { return false }
	}
	procs := machineProcs()
	if cfg.Procs.Children != nil {
		procs.Children = cfg.Procs.Children
	}
	if cfg.Procs.Args != nil {
		procs.Args = cfg.Procs.Args
	}
	if cfg.Procs.Environ != nil {
		procs.Environ = cfg.Procs.Environ
	}
	s := &Service{cfg: cfg, clock: cfg.Clock, timers: map[string]Timer{}, flights: newFlights()}
	s.queue = NewQueue(cfg.QueueFile, cfg.Clock)
	s.queue.warn = s.warn
	s.reg = NewRegistry(cfg.Clock, cfg.Log)
	s.enter = NewEnterClock(cfg.Clock)
	s.editor = newEditor(cfg.Editor, cfg.Panes, procs, cfg.Clock, s.enter, s.warn)
	s.queue.OnChange(func(tabID string, items []Item) {
		if cfg.Broadcast != nil {
			cfg.Broadcast.Broadcast(QueueUpdate{Type: "prompt-queue:update", TabID: tabID, Items: items})
		}
	})
	return s
}

func (s *Service) warn(msg string, kv ...any) { s.cfg.Log("warn", msg, kv...) }
func (s *Service) info(msg string, kv ...any) { s.cfg.Log("info", msg, kv...) }

// Queue is the prompt queue.
func (s *Service) Queue() *Queue { return s.queue }

// Registry is the dispatch registry shared by every route that types a message (the terminal's composer included).
func (s *Service) Registry() *Registry { return s.reg }

// EnterClock is when the runtime last pressed Enter in each pane; the status machine takes it at prompt-submit.
func (s *Service) EnterClock() *EnterClock { return s.enter }

// ClaimPrompt is the terminal's claim of a composer message (its id cut to 64 characters) typed into a session.
func (s *Service) ClaimPrompt(id, session, text string) bool {
	return s.reg.Claim(jscompat.Slice(id, composerIDMax), RouteComposer, session, text)
}

// PromptEnter notes an Enter the terminal passed on in a session.
func (s *Service) PromptEnter(session string) { s.enter.Note(session) }

// QueueSync is the message a page gets on connect and on request: every queue.
func (s *Service) QueueSync() QueueSync {
	return QueueSync{Type: "prompt-queue:sync", Queues: s.queue.All()}
}

// --- scheduling -------------------------------------------------------------------------------------------------

// Start delivers what waited through a restart (700 ms on) and starts the 4 s watchdog.
func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	for tabID := range s.queue.All() {
		s.Schedule(tabID, settleDelay)
	}
	s.armWatchdog()
}

func (s *Service) armWatchdog() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.watchdog = s.clock.AfterFunc(watchdogEvery, func() {
		for tabID := range s.queue.All() {
			s.mu.Lock()
			_, pending := s.timers[tabID]
			s.mu.Unlock()
			if !pending {
				s.Schedule(tabID, 0)
			}
		}
		s.armWatchdog()
	})
}

// Stop cancels every pending delivery and the watchdog, and writes the queue file.
func (s *Service) Stop() {
	s.mu.Lock()
	s.running = false
	for id, t := range s.timers {
		t.Stop()
		delete(s.timers, id)
	}
	if s.watchdog != nil {
		s.watchdog.Stop()
	}
	s.mu.Unlock()
	s.queue.Flush()
}

// Schedule delivers the head of a tab's queue after delay, replacing a pending delivery; nothing when the queue is empty.
func (s *Service) Schedule(tabID string, delay time.Duration) {
	if len(s.queue.List(tabID)) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.timers[tabID]; t != nil {
		t.Stop()
	}
	var t Timer
	t = s.clock.AfterFunc(delay, func() {
		s.mu.Lock()
		current := s.timers[tabID] == t
		if current {
			delete(s.timers, tabID)
		}
		s.mu.Unlock()
		if current {
			s.deliverHead(tabID)
		}
	})
	s.timers[tabID] = t
}

// PendingDelivery says whether a delivery of the tab's queue is scheduled (the watchdog leaves such a tab alone).
func (s *Service) PendingDelivery(tabID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, pending := s.timers[tabID]
	return pending
}

// TabSettled: the tab's agent went idle or ready-for-review; its queue goes out 700 ms later.
func (s *Service) TabSettled(tabID string) { s.Schedule(tabID, settleDelay) }

// LaunchEnded: the tab's launch ended (SessionStart); its queue goes out 700 ms later.
func (s *Service) LaunchEnded(tabID string) { s.Schedule(tabID, settleDelay) }

// TabRemoved drops a closed tab's queue (the pages are told it is empty) and its pending delivery.
func (s *Service) TabRemoved(tabID string) {
	s.mu.Lock()
	if t := s.timers[tabID]; t != nil {
		t.Stop()
		delete(s.timers, tabID)
	}
	s.mu.Unlock()
	s.queue.Drop(tabID)
}

// deliverHead brings the tab's agent to take the queue head: a launch under way is waited for, a gone agent resumed (or
// its launch given up when it has no session), a running one gets the message when it waits for a prompt. Retries
// continue while the queue holds messages; the watchdog restarts any chain that stopped.
func (s *Service) deliverHead(tabID string) {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.Session == "" {
		return
	}
	running := s.cfg.Agents.Running(tab)
	hasSession := tab.HasAgentSession || s.cfg.Agents.RecoverSession(tab)
	switch QueueAgentAction(tab.Launching, tab.LaunchFailed, running, hasSession) {
	case ActionLaunching:
		return
	case ActionUnrecoverable:
		s.cfg.Agents.GiveUpLaunch(tabID)
		return
	case ActionResume:
		switch r := s.cfg.Agents.Resume(tab); r {
		case ResumeNoSession:
			s.cfg.Agents.GiveUpLaunch(tabID)
		case ResumeResumed, ResumeRunning:
			s.Schedule(tabID, retryDelay)
		default:
			// A shell still starting (a session the machine recreated after a reboot) is busy only for a moment.
			s.info("agent of a queued message not resumed yet, trying again", "tabId", tabID, "result", r)
			s.Schedule(tabID, blockedRetry)
		}
		return
	}
	steps := NewSteps(s.clock)
	steps.Mark("agent_check")
	if fresh, ok := s.cfg.Tabs.Tab(tabID); ok {
		tab = fresh
	}
	outcome := s.Pump(tabID, MayDeliverQueue(tab.CliState, false), s.deliveryDriver(tab))
	switch outcome {
	case PumpSent:
		steps.Mark("enter")
		s.info("queued prompt sent", "tabId", tabID, "steps", steps)
		return
	case PumpEmpty, PumpNotReady:
		return
	case PumpFailed:
		s.warn("queued prompt did not reach the agent; it stays in the queue", "tabId", tabID)
	}
	if outcome == PumpBlocked {
		s.Schedule(tabID, blockedRetry)
	} else {
		s.Schedule(tabID, retryDelay)
	}
}

// PumpOutcome is one step of sending a tab's queue.
type PumpOutcome string

const (
	PumpEmpty    PumpOutcome = "empty"
	PumpNotReady PumpOutcome = "not-ready"
	PumpSent     PumpOutcome = "sent"
	PumpWait     PumpOutcome = "wait"
	PumpBlocked  PumpOutcome = "blocked"
	PumpFailed   PumpOutcome = "failed"
	PumpInFlight PumpOutcome = "in-flight"
)

// Pump sends the head of the tab's queue when ready: under the tab's single flight the head leaves the queue before it
// is typed and comes back to the head unless it went in, so it is never lost or sent twice. One message per call.
func (s *Service) Pump(tabID string, ready bool, d DeliveryDriver) PumpOutcome {
	items := s.queue.List(tabID)
	if len(items) == 0 {
		return PumpEmpty
	}
	if !ready {
		return PumpNotReady
	}
	head := items[0]
	out := PumpEmpty
	if !s.flights.run(tabID, func() {
		item, ok := s.queue.Take(tabID, head.ID)
		if !ok {
			return
		}
		res := Deliver(item, d, s.reg, false, tabID)
		if res != OutcomeSent {
			s.queue.Restore(tabID, item)
		}
		out = PumpOutcome(res)
	}) {
		return PumpInFlight
	}
	return out
}

// --- adding, steering, send now -----------------------------------------------------------------------------------

// Delivery of a new message.
const (
	DeliverySteered = "steered"
	DeliveryQueued  = "queued"
)

// TakeNewPrompt hands a message just queued to the agent: a working agent gets it in its running turn now (steer), one
// that is not working from the queue at once. A message the screen refuses stays queued.
func (s *Service) TakeNewPrompt(tabID, id string) string {
	if tab, ok := s.cfg.Tabs.Tab(tabID); !ok || tab.CliState != StateBusy {
		s.Schedule(tabID, 0)
		return DeliveryQueued
	}
	if r := s.Steer(tabID, id); r.OK {
		return DeliverySteered
	}
	return DeliveryQueued
}

// SteerResult is how a steer ended.
type SteerResult struct {
	OK      bool
	Status  int
	Message string
}

// Steer sends a queued message now, into the running turn if there is one; a menu or foreign text still stops it and the
// message goes back to the head. A Claude message steered into a running Bash or MCP call is then handed over like
// "send now", so it does not wait for the call to end.
func (s *Service) Steer(tabID, id string) SteerResult {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.Session == "" {
		return SteerResult{Status: 404, Message: msgNoTab.String()}
	}
	var res SteerResult
	if !s.flights.run(tabID, func() {
		item, ok := s.queue.Take(tabID, id)
		if !ok {
			res = SteerResult{Status: 409, Message: msgNotQueued.String()}
			return
		}
		steeredAt := s.clock.Now()
		out := Deliver(item, s.deliveryDriver(tab), s.reg, true, tabID)
		if out == OutcomeSent {
			s.info("steered prompt sent", "tabId", tabID, "enterMs", s.clock.Now().Sub(steeredAt).Milliseconds())
			if tab.Provider != ProviderCodex {
				s.handOverSteered(tabID, tab.Session, item.Text, steeredAt)
			}
			res = SteerResult{OK: true}
			return
		}
		s.queue.Restore(tabID, item)
		status := 409
		if out == OutcomeFailed {
			status = 500
		}
		res = SteerResult{Status: status, Message: steerErrors[out].String()}
	}) {
		return SteerResult{Status: 409, Message: msgInFlight.String()}
	}
	return res
}

// handOverSteered: only while the tab still works, 300 ms after the Enter, the chord hands a running call over so the
// steered message reaches the agent now. The steer is done either way.
func (s *Service) handOverSteered(tabID, session, text string, steeredAt time.Time) {
	if tab, ok := s.cfg.Tabs.Tab(tabID); !ok || tab.CliState != StateBusy {
		return
	}
	s.clock.Sleep(steerHandOver)
	if r := SendQueuedNow(text, s.sendNowDriver(tabID, session), s.clock, max(1, steeredAt.UnixMilli())); !r.OK {
		s.warn("steered message was not handed to the agent", "tabId", tabID, "status", r.Status, "message", r.Message)
	}
}

// SendNow presses Claude's "send now" for a message waiting in its own queue; false when the tab has no session. The
// chord types nothing, so its id is the tab and the text: a second press within 30 s answers delivered.
func (s *Service) SendNow(tabID, text string) (SendNowResult, bool) {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.Session == "" {
		return SendNowResult{}, false
	}
	var res SendNowResult
	if !s.flights.run(tabID, func() {
		id := "send-now:" + tabID + ":" + sha1Hex(text, 16)
		if !s.reg.Claim(id, RouteSendNow, tabID, text) {
			res = SendNowResult{OK: true, Outcome: "delivered"}
			return
		}
		res = SendQueuedNow(text, s.sendNowDriver(tabID, tab.Session), s.clock, 0)
		if !(res.OK && res.Outcome == "sent") {
			s.reg.Release(id)
		}
	}) {
		return sendNowFailure(409, msgInFlight.String(), false), true
	}
	return res, true
}

// --- the composer's text ------------------------------------------------------------------------------------------

var (
	// ErrNoTab: the tab is unknown or has no tmux session.
	ErrNoTab = errors.New("no such tab")
	// ErrCodexNeedsSubmit: Codex takes the composer's text only submitted.
	ErrCodexNeedsSubmit = errors.New("codex text must be submitted")
)

// Ways the composer's text went in.
const (
	ViaEditor    = "editor"
	ViaPaste     = "paste"
	ViaCodex     = "codex"
	ViaDuplicate = "duplicate"
	ViaFailed    = "failed"
)

// TypePrompt puts the composer's text into a tab: a Claude tab through Claude's editor (or a paste), submitted unless
// submit is false; a Codex tab pasted and submitted with its box watched (ViaFailed when Codex kept it in the box). A
// messageID that went in within 30 s is not typed again (ViaDuplicate). A Claude text that could not be typed frees
// its id, so a retry may go.
func (s *Service) TypePrompt(tabID, text string, submit bool, messageID string, steps *Steps) (string, error) {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.Session == "" {
		return "", ErrNoTab
	}
	codex := tab.Provider == ProviderCodex
	if codex && !submit {
		return "", ErrCodexNeedsSubmit
	}
	if !s.reg.Claim(messageID, RoutePromptText, tabID, text) {
		return ViaDuplicate, nil
	}
	if codex {
		sent := SubmitCodex(text, false, s.codexDriver(tab.Session), s.clock)
		steps.Mark("enter")
		if !sent {
			return ViaFailed, nil
		}
		s.enter.Note(tab.Session)
		return ViaCodex, nil
	}
	var via string
	var err error
	if submit {
		via, err = s.editor.submitPromptText(tab.Session, text, steps)
	} else {
		via, err = s.editor.typePromptText(tab.Session, text, steps)
	}
	if err != nil {
		s.reg.Release(messageID)
		return "", err
	}
	return via, nil
}

// SubmitText types a text into a Claude pane the composer's way (its editor, or a paste) and submits it.
func (s *Service) SubmitText(session, text string) error {
	_, err := s.editor.submitPromptText(session, text, nil)
	return err
}

// Interrupt presses Escape in the tab and reports the interrupt (the status machine moves the tab to idle); false
// for an unknown tab, a tab without a session, or a key tmux did not take.
func (s *Service) Interrupt(tabID string) bool {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.Session == "" {
		return false
	}
	if s.cfg.Panes.SendRawKeys(tab.Session, "Escape") != nil {
		return false
	}
	s.cfg.Events.Interrupted(tabID, tab.Session)
	return true
}

// MinPaneCols and MinPaneRows: smaller panes wrap the prompt box and spinner, so their screen is not read for a menu.
const (
	MinPaneCols = 60
	MinPaneRows = 12
)

var menuCapable = map[string]bool{StateBusy: true, StateUnknown: true, StateIdle: true, StateReadyForReview: true}

// MarkNeedsInputFromScreen: after a command left a menu open, the agent waits for an answer whatever state the tab was
// in. True when the tab moved to needs-input.
func (s *Service) MarkNeedsInputFromScreen(tabID string) bool {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.Session == "" || !menuCapable[tab.CliState] {
		return false
	}
	d := s.cfg.Panes.PaneDetail(tab.Session)
	if d.Width < MinPaneCols || d.Height < MinPaneRows {
		return false
	}
	content, ok := s.cfg.Panes.Capture(tab.Session)
	if !ok || content == "" || screen.FindMenu(content) == nil {
		return false
	}
	return s.cfg.Events.MenuOnScreen(tabID)
}

// RunCommand runs a slash line in a session and does what follows a run: a confirmed model or effort becomes the
// conversation's launch, a plugin or skill reload makes the catalog stale, and a Claude tab is checked for being stuck.
func (s *Service) RunCommand(in RunInput) RunResult {
	defer func() {
		if in.Provider != ProviderClaude {
			return
		}
		if tab, ok := s.cfg.Tabs.TabOfSession(in.Session); ok {
			go s.cfg.Events.RecheckStuck(tab.ID)
		}
	}()
	r := RunCommandLine(in, commandDriver{s}, s.clock)
	body, ok := r.Body.(*RunBody)
	if r.Status != 200 || !ok {
		return r
	}
	if body.Outcome == "confirmed" && body.Applied != nil {
		s.cfg.Events.LaunchChosen(in.Session, *body.Applied)
	}
	if lower := strings.ToLower(body.Command); lower == "/reload-plugins" || lower == "/reload-skills" {
		s.cfg.Events.CommandsReloaded(in.Provider)
	}
	return r
}
