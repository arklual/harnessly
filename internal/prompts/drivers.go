package prompts

import (
	"time"
)

// claudeDriver types a queued message into a Claude pane: pictures in one paste, long or multi-line text through the
// editor (else a paste), a short line typed with its Enter.
type claudeDriver struct {
	s       *Service
	session string
}

func (d claudeDriver) Capture() (string, bool) { return d.s.cfg.Panes.CaptureStyled(d.session) }

func (d claudeDriver) PasteImages(paths []string) bool {
	p := d.s.cfg.Panes
	return SendImagePaths(paths, true,
		func(body string) error { return p.SendBracketedPasteOnly(d.session, body) },
		func() (string, bool) { return p.Capture(d.session) },
		d.s.clock).Confirmed
}

func (d claudeDriver) PasteText(text string) error {
	_, err := d.s.editor.typePromptText(d.session, text, nil)
	return err
}

func (d claudeDriver) TypeLine(text string) error {
	if err := d.s.cfg.Panes.SendLiteralThenEnter(d.session, text); err != nil {
		return err
	}
	d.s.enter.Note(d.session)
	return nil
}

func (d claudeDriver) Enter() error {
	if err := d.s.cfg.Panes.SendRawKeys(d.session, "Enter"); err != nil {
		return err
	}
	d.s.enter.Note(d.session)
	return nil
}

func (d claudeDriver) Sleep(dur time.Duration) { d.s.clock.Sleep(dur) }

// codexDriver types a queued message into a Codex pane: one picture per paste, the text pasted and submitted with its
// box watched.
type codexDriver struct {
	s       *Service
	session string
}

func (d codexDriver) Capture() (string, bool) { return d.s.cfg.Panes.CaptureStyled(d.session) }

func (d codexDriver) PasteImages(paths []string) bool {
	p := d.s.cfg.Panes
	return SendImagePaths(paths, false,
		func(body string) error { return p.SendBracketedPasteOnly(d.session, body) },
		func() (string, bool) { return p.Capture(d.session) },
		d.s.clock).Confirmed
}

func (d codexDriver) PasteText(text string) error {
	return d.s.cfg.Panes.SendBracketedPasteOnly(d.session, text)
}

func (d codexDriver) Enter() error { return d.s.cfg.Panes.SendRawKeys(d.session, "Enter") }

func (d codexDriver) Sleep(dur time.Duration) { d.s.clock.Sleep(dur) }

func (d codexDriver) Paste(text string) error { return d.PasteText(text) }

func (d codexDriver) Submit(text string, typed bool) bool {
	if !SubmitCodex(text, typed, codexSubmitDriver{d}, d.s.clock) {
		return false
	}
	d.s.enter.Note(d.session)
	return true
}

// codexPlain is the Codex driver reading the plain screen, as the submit loop does.
func (d codexDriver) plain() (string, bool) { return d.s.cfg.Panes.Capture(d.session) }

func (s *Service) deliveryDriver(tab Tab) DeliveryDriver {
	if tab.Provider == ProviderCodex {
		return codexDriver{s, tab.Session}
	}
	return claudeDriver{s, tab.Session}
}

func (s *Service) codexDriver(session string) CodexDriver {
	return codexSubmitDriver{codexDriver{s, session}}
}

// codexSubmitDriver is the Codex driver for SubmitCodex: it reads the plain screen.
type codexSubmitDriver struct{ codexDriver }

func (d codexSubmitDriver) Capture() (string, bool) { return d.plain() }

// sendNowDriver reads the pane and the tab's transcript (looked up on every read: it may appear meanwhile).
type sendNowDriver struct {
	s              *Service
	tabID, session string
}

func (s *Service) sendNowDriver(tabID, session string) SendNowDriver {
	return sendNowDriver{s, tabID, session}
}

func (d sendNowDriver) Capture() (string, bool) { return d.s.cfg.Panes.CaptureStyled(d.session) }

func (d sendNowDriver) ReadTranscript() ([]string, bool) {
	tab, ok := d.s.cfg.Tabs.Tab(d.tabID)
	if !ok || tab.TranscriptPath == "" {
		return nil, false
	}
	return d.s.cfg.ReadTranscript(tab.TranscriptPath)
}

func (d sendNowDriver) SendKeys(keys ...string) error {
	return d.s.cfg.Panes.SendKeySequence(d.session, keys...)
}

// commandDriver runs commands through the service's panes and tabs.
type commandDriver struct{ s *Service }

func (d commandDriver) HasSession(session string) bool { return d.s.cfg.Panes.HasSession(session) }
func (d commandDriver) CurrentCommand(session string) string {
	return d.s.cfg.Panes.PaneCurrentCommand(session)
}
func (d commandDriver) IsShell(command string) bool { return d.s.cfg.IsShell(command) }
func (d commandDriver) CaptureStyled(session string) (string, bool) {
	return d.s.cfg.Panes.CaptureStyled(session)
}
func (d commandDriver) SendLine(session, line string) error {
	return d.s.cfg.Panes.SendLiteralThenEnter(session, line)
}
func (d commandDriver) SendKey(session, key string) error {
	return d.s.cfg.Panes.SendRawKeys(session, key)
}
func (d commandDriver) CliState(session string) (string, bool) {
	tab, ok := d.s.cfg.Tabs.TabOfSession(session)
	if !ok || tab.CliState == "" {
		return "", false
	}
	return tab.CliState, true
}
