package prompts

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
)

// The slash-command runner types a line into an agent's pane and reads what the TUI did with it: confirms "Switch
// model?" / "Change effort level?" for /model x and /effort x, reports a menu or panel that opened, otherwise "sent".

const (
	// CommandLineMax is the longest command line, in UTF-16 units.
	CommandLineMax     = 2000
	commandPoll        = 75 * time.Millisecond
	commandPollLimit   = 2500 * time.Millisecond
	commandSettle      = 600 * time.Millisecond
	commandAfterAnswer = 1500 * time.Millisecond
)

var (
	commandLineRE = regexp.MustCompile(`^/([A-Za-z0-9][A-Za-z0-9._:-]{0,127})(?:[ \x{a0}]+([\s\S]*))?$`)
	// C0/C1 controls, DEL, line and paragraph separators and bidi overrides: none belongs in a typed command.
	forbiddenChars = regexp.MustCompile(`[\x{0}-\x{1f}\x{7f}-\x{9f}\x{2028}\x{2029}\x{202a}-\x{202e}\x{2066}-\x{2069}]`)
	// SessionNameRE is what a tmux session name from a page may look like.
	SessionNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
	// ModelIDRE is a model id or alias (it is typed into a shell, so it stays this strict): aliases, full ids, the [1m]
	// context suffix.
	ModelIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}(\[1m\])?$`)
	// ClaudeEfforts are the levels `claude --effort` and /effort accept.
	ClaudeEfforts = []string{"low", "medium", "high", "xhigh", "max", "ultracode"}
)

// RunLine is a parsed command line.
type RunLine struct {
	Name, Args string
}

// ParseRunLine checks a command line: 2–2000 characters, no control characters, `/name` and optional arguments; the
// error text is shown to the person.
func ParseRunLine(line string) (RunLine, string) {
	if n := jscompat.Len(line); n < 2 || n > CommandLineMax {
		return RunLine{}, "Команда должна быть от 2 до 2000 символов"
	}
	if forbiddenChars.MatchString(line) {
		return RunLine{}, "Команда должна быть одной строкой без управляющих символов"
	}
	m := commandLineRE.FindStringSubmatch(line)
	if m == nil || !catalog.IsCommandName(m[1]) {
		return RunLine{}, "Команда начинается с «/» и имени"
	}
	return RunLine{Name: m[1], Args: jscompat.Trim(m[2])}, ""
}

// ClassifyCommand is the built-in behind a name or alias; anything else (skills, plugins, typos) is typed as a prompt and
// the CLI answers it.
func ClassifyCommand(provider, name string) (catalog.Item, bool) {
	return catalog.Builtin(provider, name)
}

// Applied is a model or effort a command changed.
type Applied struct {
	Model  *string `json:"model,omitempty"`
	Effort *string `json:"effort,omitempty"`
}

// LaunchChangeOf: Claude's `/model <id>` and `/effort <level>` (one argument) change the launch the conversation resumes
// with.
func LaunchChangeOf(provider string, l RunLine) (Applied, bool) {
	if provider != ProviderClaude || l.Args == "" || strings.ContainsFunc(l.Args, jscompat.IsSpace) {
		return Applied{}, false
	}
	name, value := strings.ToLower(l.Name), strings.ToLower(l.Args)
	switch {
	case name == "model" && ModelIDRE.MatchString(value):
		return Applied{Model: &value}, true
	case name == "effort" && slices.Contains(ClaudeEfforts, value):
		return Applied{Effort: &value}, true
	}
	return Applied{}, false
}

// Command runner errors (the `error` field of a refusal).
const (
	RunInvalid       = "invalid"
	RunNotFound      = "not-found"
	RunBlocked       = "blocked"
	RunNotRunning    = "not-running"
	RunMenuOpen      = "menu-open"
	RunBusy          = "busy"
	RunInputNotEmpty = "input-not-empty"
	RunNoScreen      = "no-screen"
	RunDeclined      = "declined"
)

// RunError is a refusal of the command runner.
type RunError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// RunBody is a 200 answer of the command runner; Outcome is sent, confirmed, menu or panel.
type RunBody struct {
	Outcome string       `json:"outcome"`
	Expect  string       `json:"expect,omitempty"`
	Applied *Applied     `json:"applied,omitempty"`
	Asked   *bool        `json:"asked,omitempty"`
	Menu    *screen.Menu `json:"menu,omitempty"`
	Screen  *string      `json:"screen,omitempty"`
	Command string       `json:"command"`
}

// RunResult is the status and body of a command run: a *RunBody on 200, a *RunError otherwise.
type RunResult struct {
	Status int
	Body   any
}

func runFail(status int, code, message string) RunResult {
	return RunResult{Status: status, Body: &RunError{Error: code, Message: message}}
}

// CommandDriver is the pane a command runs in.
type CommandDriver interface {
	HasSession(session string) bool
	// CurrentCommand is the pane's foreground program ("" unknown).
	CurrentCommand(session string) string
	IsShell(command string) bool
	// CaptureStyled is the visible screen with SGR attributes; false when it cannot be read.
	CaptureStyled(session string) (string, bool)
	// SendLine types the line literally and presses Enter 50 ms later.
	SendLine(session, line string) error
	SendKey(session, key string) error
	// CliState is the hook-driven state of the tab behind the session; false when the runtime does not track it.
	CliState(session string) (string, bool)
}

// RunInput is one command run. ExpectModel (switch-model) refuses anything but /model <id>.
type RunInput struct {
	Session, Provider, Line string
	ExpectModel             bool
}

var (
	modelSetRE  = regexp.MustCompile(`Set model to\b`)
	effortSetRE = regexp.MustCompile(`Set effort level to\b|Ultracode on\b`)
)

func boolPtr(b bool) *bool { return &b }

// RunCommandLine refuses blocked commands and panes without a running agent, refuses while the TUI shows a menu, works
// (prompt-like commands are queued by the CLI and pass) or has typed text; types the line, then polls the screen every
// 75 ms for 2.5 s (1.5 s more once a confirmation was answered).
func RunCommandLine(in RunInput, d CommandDriver, clock Clock) RunResult {
	line, bad := ParseRunLine(in.Line)
	if bad != "" {
		return runFail(400, RunInvalid, bad)
	}
	command := "/" + line.Name
	if !SessionNameRE.MatchString(in.Session) {
		return runFail(400, RunInvalid, "Неверная сессия")
	}
	item, known := ClassifyCommand(in.Provider, line.Name)
	if known && item.Execution == "blocked" {
		note := "команда заблокирована"
		if item.Note != nil {
			note = *item.Note
		}
		return runFail(422, RunBlocked, command+" недоступна в вебе: "+note)
	}
	change, changes := LaunchChangeOf(in.Provider, line)
	if in.ExpectModel && change.Model == nil {
		return runFail(400, RunInvalid, "Неизвестная модель")
	}
	if !d.HasSession(in.Session) {
		return runFail(404, RunNotFound, "Сессия не найдена")
	}
	if proc := d.CurrentCommand(in.Session); proc == "" || d.IsShell(proc) {
		return runFail(409, RunNotRunning, "Агент в этом диалоге не запущен — перезапустите диалог")
	}
	before, ok := d.CaptureStyled(in.Session)
	if !ok {
		return runFail(500, RunNoScreen, "Не получилось прочитать экран агента")
	}
	start := screen.Read(before)
	if start.Menu != nil {
		return runFail(409, RunMenuOpen, "В терминале открыто меню — ответьте на него или закройте")
	}
	queueable := !known || (item.Execution == "send" && item.Output == "reply")
	state, _ := d.CliState(in.Session)
	if (start.Busy || state == StateBusy) && !queueable {
		return runFail(409, RunBusy, "Агент сейчас работает — дождитесь конца шага или остановите его")
	}
	if start.Input != "" {
		return runFail(409, RunInputNotEmpty, "В поле ввода терминала уже есть текст — отправьте или сотрите его")
	}

	typed := command
	if line.Args != "" {
		typed += " " + line.Args
	}
	if err := d.SendLine(in.Session, typed); err != nil {
		return runFail(500, RunNoScreen, "Не получилось набрать команду")
	}

	exec := item.Execution
	expectsMenu := known && (exec == "menu" || exec == "terminal" || ((exec == "model" || exec == "effort") && line.Args == ""))
	applied := effortSetRE
	if change.Model != nil {
		applied = modelSetRE
	}
	count := func(re *regexp.Regexp, s string) int { return len(re.FindAllStringIndex(s, -1)) }
	menuBody := func(m *screen.Menu) RunResult {
		return RunResult{Status: 200, Body: &RunBody{Outcome: "menu", Menu: m, Command: command}}
	}

	startedAt := clock.Now()
	var answeredAt *time.Time
	for {
		limit := commandPollLimit
		if answeredAt != nil {
			limit += commandAfterAnswer
		}
		if clock.Now().Sub(startedAt) >= limit {
			break
		}
		clock.Sleep(commandPoll)
		styled, ok := d.CaptureStyled(in.Session)
		if !ok {
			continue
		}
		s := screen.Read(styled)
		elapsed := clock.Now().Sub(startedAt)
		confirm := screen.IsLaunchConfirm(s.Plain)

		if changes && confirm && s.Menu != nil && answeredAt == nil {
			// Picking the model in the web is the consent to re-read the history without cache; "1. Yes" is focused.
			if s.Menu.YesFocused() {
				if err := d.SendKey(in.Session, "Enter"); err != nil {
					return menuBody(s.Menu)
				}
				now := clock.Now()
				answeredAt = &now
				continue
			}
			return menuBody(s.Menu)
		}
		if changes && !confirm && count(applied, s.Plain) > count(applied, start.Plain) {
			return RunResult{Status: 200, Body: &RunBody{Outcome: "confirmed", Applied: &change, Asked: boolPtr(answeredAt != nil), Command: command}}
		}
		if changes && answeredAt != nil && screen.CountKept(s.Plain) > screen.CountKept(start.Plain) {
			return runFail(409, RunDeclined, "Агент оставил прежнее значение")
		}
		if answeredAt != nil {
			continue
		}
		if s.Menu != nil {
			return menuBody(s.Menu)
		}
		if s.Panel && screen.CountPanelHints(s.Plain) > screen.CountPanelHints(start.Plain) {
			snap := screen.PanelSnapshot(s.Plain)
			return RunResult{Status: 200, Body: &RunBody{Outcome: "panel", Screen: &snap, Command: command}}
		}
		if !expectsMenu && !changes && s.InputLine && s.Input == "" && elapsed >= commandSettle {
			break
		}
	}
	if changes && answeredAt != nil {
		return RunResult{Status: 200, Body: &RunBody{Outcome: "confirmed", Applied: &change, Asked: boolPtr(true), Command: command}}
	}
	expect := "reply"
	if known {
		expect = item.Output
	}
	return RunResult{Status: 200, Body: &RunBody{Outcome: "sent", Expect: expect, Command: command}}
}
