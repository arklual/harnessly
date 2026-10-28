// Package install puts Claude Code or Codex on the machine as a job of five steps the hub's UI shows live: the system
// is checked, the provider's servers are reached, the CLI is downloaded, installed, and started once to see it runs.
// Claude Code comes from Anthropic's native installer into ~/.local/bin; Codex from npm into ~/.local.
package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/preflight"
)

// Step ids, in order (docs/hub/api.md).
const (
	StepSystem   = "system"
	StepProvider = "provider"
	StepDownload = "download"
	StepInstall  = "install"
	StepVerify   = "verify"
)

var stepOrder = []string{StepSystem, StepProvider, StepDownload, StepInstall, StepVerify}

// Timeouts of the steps that run programs; the whole install of Codex through npm over a slow proxy is the long one.
const (
	downloadTimeout = 4 * time.Minute
	installTimeout  = 10 * time.Minute
	verifyTimeout   = 30 * time.Second
	keepJobs        = 20
	outputTail      = 300
)

// vendor is how the provider's company is named in a message.
var vendor = map[string]string{hubproto.ProviderClaude: "Anthropic", hubproto.ProviderCodex: "OpenAI"}

// Deps are what an install needs of the rest of the agent.
type Deps struct {
	// Shell runs scripts in the login shell, EnvDir is where the env files are (a proxy among them).
	Shell  agents.LoginRunner
	EnvDir string
	// Home is the account's home folder; JobsDir holds each job's scratch folder.
	Home    string
	JobsDir string
	// Preflight is a fresh report of the system; Check the provider check through the proxy the agent has.
	Preflight func(ctx context.Context) hubproto.PreflightReport
	Check     func(ctx context.Context, provider string) hubproto.ProviderCheck
	// Emit gets a copy of the job after every change; Done is told when a job ended, for the agent to refresh statuses.
	Emit func(hubproto.Job)
	Done func(provider string, ok bool)
}

// Manager runs install jobs, one per provider at a time.
type Manager struct {
	d   Deps
	ctx context.Context

	mu      sync.Mutex
	jobs    map[string]*hubproto.Job
	order   []string
	running map[string]string // provider → job id
	wg      sync.WaitGroup
}

// NewManager is a manager whose jobs stop when ctx ends.
func NewManager(ctx context.Context, d Deps) *Manager {
	return &Manager{d: d, ctx: ctx, jobs: map[string]*hubproto.Job{}, running: map[string]string{}}
}

// ErrBusy: an install of that provider is already running.
var ErrBusy = errors.New("an install of this agent is already running")

// Start begins the job for the hub's jobId. fromProvider marks system as done and starts at the provider step.
func (m *Manager) Start(jobID, provider string, fromProvider bool) error {
	if provider != hubproto.ProviderClaude && provider != hubproto.ProviderCodex {
		return fmt.Errorf("no installer for %q", provider)
	}
	m.mu.Lock()
	if _, busy := m.running[provider]; busy {
		m.mu.Unlock()
		return ErrBusy
	}
	if _, dup := m.jobs[jobID]; dup {
		m.mu.Unlock()
		return fmt.Errorf("job %s exists", jobID)
	}
	p := provider
	job := &hubproto.Job{ID: jobID, Kind: hubproto.JobAgentInstall, Provider: &p, State: hubproto.JobRunning}
	for _, id := range stepOrder {
		job.Steps = append(job.Steps, hubproto.JobStep{ID: id, State: hubproto.StepWait})
	}
	m.jobs[jobID] = job
	m.order = append(m.order, jobID)
	for len(m.order) > keepJobs {
		delete(m.jobs, m.order[0])
		m.order = m.order[1:]
	}
	m.running[provider] = jobID
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(jobID, provider, fromProvider)
	}()
	return nil
}

// Get is a copy of a job.
func (m *Manager) Get(id string) (hubproto.Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return hubproto.Job{}, false
	}
	return snapshot(j), true
}

// Wait blocks until the running jobs end.
func (m *Manager) Wait() { m.wg.Wait() }

func snapshot(j *hubproto.Job) hubproto.Job {
	c := *j
	c.Steps = append([]hubproto.JobStep(nil), j.Steps...)
	if j.Error != nil {
		e := *j.Error
		c.Error = &e
	}
	return c
}

// update changes a job under the lock and sends the result out.
func (m *Manager) update(id string, f func(j *hubproto.Job)) {
	m.mu.Lock()
	j := m.jobs[id]
	f(j)
	snap := snapshot(j)
	m.mu.Unlock()
	if m.d.Emit != nil {
		m.d.Emit(snap)
	}
}

func (m *Manager) setStep(id, step, state string, detail string) {
	m.update(id, func(j *hubproto.Job) {
		for i := range j.Steps {
			if j.Steps[i].ID == step {
				j.Steps[i].State = state
				j.Steps[i].Detail = nil
				if detail != "" {
					d := detail
					j.Steps[i].Detail = &d
				}
			}
		}
	})
}

func (m *Manager) fail(id, step, code, message, detail string, check *hubproto.ProviderCheck) {
	m.update(id, func(j *hubproto.Job) {
		for i := range j.Steps {
			if j.Steps[i].ID == step {
				j.Steps[i].State = hubproto.StepFail
				if detail != "" {
					d := detail
					j.Steps[i].Detail = &d
				}
			}
		}
		j.State = hubproto.JobFailed
		j.Error = &hubproto.JobError{Code: code, Message: message}
		j.Check = check
	})
}

func (m *Manager) run(id, provider string, fromProvider bool) {
	ok := false
	defer func() {
		m.mu.Lock()
		delete(m.running, provider)
		m.mu.Unlock()
		if m.d.Done != nil {
			m.d.Done(provider, ok)
		}
	}()
	name := agents.DisplayName(provider)

	if fromProvider {
		m.setStep(id, StepSystem, hubproto.StepOK, "")
	} else {
		m.setStep(id, StepSystem, hubproto.StepRun, "")
		if msg := m.systemProblem(provider); msg != "" {
			m.fail(id, StepSystem, StepSystem, msg, msg, nil)
			return
		}
		m.setStep(id, StepSystem, hubproto.StepOK, "")
	}

	m.setStep(id, StepProvider, hubproto.StepRun, "")
	check := m.d.Check(m.ctx, provider)
	if check.Verdict != hubproto.VerdictOK {
		msg := "Серверы " + vendor[provider] + " не отвечают"
		if check.Verdict == hubproto.VerdictRejected {
			msg = "Серверы " + vendor[provider] + " отказали в доступе"
		}
		detail := check.Host
		if check.Detail != nil {
			detail += ": " + *check.Detail
		}
		m.fail(id, StepProvider, StepProvider, msg, detail, &check)
		return
	}
	m.setStep(id, StepProvider, hubproto.StepOK, "")

	dir := filepath.Join(m.d.JobsDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.fail(id, StepDownload, StepDownload, "Не удалось подготовить папку для загрузки", err.Error(), nil)
		return
	}
	defer os.RemoveAll(dir)
	env := "HL_JOB_DIR=" + dir
	prefix := agents.EnvSourcePrefix(m.d.EnvDir, provider)

	m.setStep(id, StepDownload, hubproto.StepRun, "")
	if msg := m.script(downloadTimeout, prefix+downloadScript[provider], env); msg != "" {
		m.fail(id, StepDownload, StepDownload, "Не удалось скачать "+name, msg, nil)
		return
	}
	m.setStep(id, StepDownload, hubproto.StepOK, "")

	m.setStep(id, StepInstall, hubproto.StepRun, "")
	if msg := m.script(installTimeout, prefix+installScript[provider], env); msg != "" {
		m.fail(id, StepInstall, StepInstall, "Не удалось установить "+name, msg, nil)
		return
	}
	m.setStep(id, StepInstall, hubproto.StepOK, "")

	m.setStep(id, StepVerify, hubproto.StepRun, "")
	version, msg := m.verify(provider)
	if msg != "" {
		m.fail(id, StepVerify, StepVerify, name+" установлен, но не запускается", msg, nil)
		return
	}
	m.setStep(id, StepVerify, hubproto.StepOK, version)
	m.update(id, func(j *hubproto.Job) { j.State = hubproto.JobDone })
	ok = true
}

// systemProblem is why the machine cannot take this agent, "" when it can.
func (m *Manager) systemProblem(provider string) string {
	rep := m.d.Preflight(m.ctx)
	for _, c := range rep.Checks {
		switch {
		case c.Level == hubproto.LevelFail:
			return c.Message
		case provider == hubproto.ProviderCodex && c.ID == "node" && c.Level != hubproto.LevelOK:
			return c.Message
		}
	}
	return ""
}

// script runs one step's script in the login shell and returns what to tell the person if it failed, "" if it passed.
func (m *Manager) script(timeout time.Duration, script string, env ...string) string {
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()
	res, err := m.d.Shell.RunLogin(ctx, script, env...)
	if err != nil {
		return err.Error()
	}
	if res.Exit != 0 {
		if t := tail(res.Stdout); t != "" {
			return t
		}
		return fmt.Sprintf("the step ended with code %d", res.Exit)
	}
	return ""
}

// tail is the last of a script's output, one line, for a message.
func tail(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(stripControl(l)); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	s := strings.Join(lines, " / ")
	if r := []rune(s); len(r) > outputTail {
		s = "…" + string(r[len(r)-outputTail:])
	}
	return s
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// verify starts the CLI once through the login shell, as a tab would, and reads its version. A CLI that the installer
// put in ~/.local/bin but a login shell does not find gets that folder added to the account's profile, once.
func (m *Manager) verify(provider string) (version, problem string) {
	ctx, cancel := context.WithTimeout(m.ctx, verifyTimeout)
	defer cancel()
	prefix := agents.EnvSourcePrefix(m.d.EnvDir, provider)
	probe := func() (out string, found bool, err error) {
		res, err := m.d.Shell.RunLogin(ctx, prefix+"command -v "+provider+" >/dev/null 2>&1 || exit 127\n"+provider+" --version 2>&1 | head -n 1")
		if err != nil {
			return "", false, err
		}
		return strings.TrimSpace(res.Stdout), res.Exit != 127, nil
	}
	out, found, err := probe()
	if err != nil {
		return "", err.Error()
	}
	if !found {
		if st, err := os.Stat(filepath.Join(m.d.Home, ".local", "bin", provider)); err != nil || st.IsDir() {
			return "", provider + " не найден после установки"
		}
		if err := addLocalBinToProfile(m.d.Home); err != nil {
			return "", "не удалось добавить ~/.local/bin в PATH: " + err.Error()
		}
		if out, found, err = probe(); err != nil {
			return "", err.Error()
		}
		if !found {
			return "", provider + " установлен в ~/.local/bin, но оболочка входа его не видит: добавьте эту папку в PATH в профиле аккаунта"
		}
	}
	if v := preflight.Version(out); v != nil {
		return *v, ""
	}
	if out == "" {
		return "", provider + " не напечатал версию"
	}
	return tail(out), ""
}

const profileMarker = "# added by harnessly: the agent CLIs install into ~/.local/bin"

// addLocalBinToProfile appends the PATH line to ~/.profile, and to ~/.zprofile when the account uses zsh, unless it is
// there.
func addLocalBinToProfile(home string) error {
	if home == "" {
		return errors.New("no home folder")
	}
	files := []string{".profile"}
	for _, zsh := range []string{".zprofile", ".zshrc"} {
		if _, err := os.Stat(filepath.Join(home, zsh)); err == nil {
			files = append(files, ".zprofile")
			break
		}
	}
	for _, name := range files {
		path := filepath.Join(home, name)
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if strings.Contains(string(raw), profileMarker) {
			continue
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := f.WriteString("\n" + profileMarker + "\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
	}
	return nil
}

// The scripts. Each prints what went wrong on its output (2>&1) and exits non-zero; $HL_JOB_DIR is the job's folder.
var downloadScript = map[string]string{
	hubproto.ProviderClaude: `set -e
if command -v curl >/dev/null 2>&1; then
  curl -fsSL --connect-timeout 20 --max-time 180 https://claude.ai/install.sh -o "$HL_JOB_DIR/install.sh" 2>&1
elif command -v wget >/dev/null 2>&1; then
  wget -q -T 60 -O "$HL_JOB_DIR/install.sh" https://claude.ai/install.sh 2>&1
else
  echo "нужен curl или wget"; exit 127
fi
test -s "$HL_JOB_DIR/install.sh" || { echo "установщик пришёл пустым"; exit 1; }
`,
	hubproto.ProviderCodex: `set -e
command -v npm >/dev/null 2>&1 || { echo "нужен npm (Node.js 20+)"; exit 127; }
cd "$HL_JOB_DIR"
npm pack @openai/codex@latest --silent --loglevel=error 2>&1
ls "$HL_JOB_DIR"/*.tgz >/dev/null 2>&1 || { echo "npm не скачал пакет"; exit 1; }
`,
}

var installScript = map[string]string{
	hubproto.ProviderClaude: `command -v bash >/dev/null 2>&1 || { echo "нужен bash"; exit 127; }
bash "$HL_JOB_DIR/install.sh" 2>&1
`,
	hubproto.ProviderCodex: `mkdir -p "$HOME/.local"
npm install -g --prefix "$HOME/.local" --no-fund --no-audit --loglevel=error "$HL_JOB_DIR"/*.tgz 2>&1
`,
}
