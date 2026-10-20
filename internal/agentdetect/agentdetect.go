// Package agentdetect tells whether a coding agent (Claude Code, Codex) runs in a tab's terminal, which conversation
// it is in and whether that conversation can be resumed from its transcript: the check-agent answer, read the way the
// previous runtime read it, on top of internal/agents/claude and internal/agents/codex.
package agentdetect

import (
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/machine"
	procfs "github.com/ly-solutions/harnessly/internal/proc"
)

// Provider is an agent the runtime knows.
type Provider struct {
	ID          string
	DisplayName string
	PanelType   string
}

var (
	Claude = Provider{ID: claude.ProviderID, DisplayName: claude.DisplayName, PanelType: claude.PanelType}
	Codex  = Provider{ID: codex.ProviderID, DisplayName: codex.DisplayName, PanelType: codex.PanelType}
)

// Panes is what the detector asks the terminal multiplexer.
type Panes interface {
	HasSession(name string) bool
	PanePID(name string) int
}

// Procs reads the process table; the machine's by default.
type Procs struct {
	Children func(pid int) []int
	Args     func(pid int) (string, bool)
	Cwd      func(pid int) string
}

func machineProcs() Procs {
	return Procs{Children: machine.ChildPIDs, Args: machine.ProcessArgs, Cwd: machine.ProcessCwd}
}

// Detector looks at a tab's pane.
type Detector struct {
	Home  string
	Panes Panes
	Procs Procs
	Now   func() time.Time
}

func New(home string, panes Panes) *Detector {
	return &Detector{Home: home, Panes: panes, Procs: machineProcs(), Now: time.Now}
}

// Answer is what the page is told about a tab.
type Answer struct {
	Running   bool
	CheckedAt int64
	// SessionID is the conversation's id as the agent recorded it (normally a string), nil when not known.
	SessionID any
	Resumable bool
	Provider  *Provider
}

// JSON is the answer as the page reads it.
func (a Answer) JSON() map[string]any {
	out := map[string]any{"running": a.Running, "checkedAt": a.CheckedAt, "sessionId": a.SessionID}
	if a.Provider != nil {
		out["resumable"] = a.Resumable
		out["providerId"] = a.Provider.ID
		out["providerDisplayName"] = a.Provider.DisplayName
		out["providerPanelType"] = a.Provider.PanelType
	}
	return out
}

// ErrBadRecord: an agent's own record of a session cannot be read the way the previous runtime expected, which made
// it fail the request.
var ErrBadRecord = claude.ErrBadRecord

func (d *Detector) procs() procfs.Procs {
	return procfs.Procs{Children: d.Procs.Children, Args: d.Procs.Args, Cwd: d.Procs.Cwd}
}

// claudeDetector reads like the previous runtime's check: "claude" anywhere in a command line, pid files left alone
// (the answer is read-only), and nothing asked about the installation.
func (d *Detector) claudeDetector() *claude.Detector {
	return &claude.Detector{Dir: filepath.Join(d.Home, ".claude"), Procs: d.procs(), Match: agents.MatchSubstring}
}

func (d *Detector) codexDetector() *codex.Detector {
	return &codex.Detector{Dir: filepath.Join(d.Home, ".codex"), Procs: d.procs(), Match: agents.MatchSubstring, Now: d.Now}
}

func (d *Detector) answer(at int64, info agents.SessionInfo, p *Provider) Answer {
	a := Answer{Running: true, CheckedAt: at, Resumable: info.JSONLPath != "", Provider: p}
	if info.RawSessionID != nil {
		a.SessionID = info.RawSessionID
	}
	return a
}

// Check looks at the session's pane: Claude Code first, then Codex.
func (d *Detector) Check(name string) (Answer, error) {
	a := Answer{CheckedAt: d.Now().UnixMilli()}
	if !d.Panes.HasSession(name) {
		return a, nil
	}
	pane := d.Panes.PanePID(name)
	if pane <= 0 {
		return a, nil
	}
	children := d.Procs.Children(pane)
	if cd := d.claudeDetector(); cd.IsRunning(children) {
		info, err := cd.Detect(children)
		if err != nil {
			return a, err
		}
		return d.answer(a.CheckedAt, info, &Claude), nil
	}
	if xd := d.codexDetector(); xd.IsRunning(children) {
		return d.answer(a.CheckedAt, xd.Detect(children, false), &Codex), nil
	}
	return a, nil
}
