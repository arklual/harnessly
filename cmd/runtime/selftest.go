package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// The self-test (HRN-794): what an agent's tab needs from this machine, tried on the machine itself, in a throwaway pane
// started exactly as a tab's pane is. Tests on stand-ins cannot see the machine: HRN-777 (tmux answered `nice` for a
// bare shell, and a restored agent was never resumed) lived only in one machine's process namespaces.
//
//	pane_shell    a new pane is seen as its bare shell (what tells a tab without its agent, and resumes one)
//	pane_program  a program typed into it is seen in the foreground, and the shell again after Ctrl-C (what tells a
//	              running agent, and keeps text from reaching a bare shell)
//	pane_keys     keys sent to a program in the pane reach it and its answer is read back from the screen (how a
//	              choice of a question and a message reach the agent)
//
// It runs a while after the runtime starts and every 30 minutes (selftest_keeper.go), on `harnessly selftest`, and with
// the machine checks of Settings.

const (
	// selftestShellWait is how long a new pane's login shell may take to read what is typed: its rc files run first,
	// and on a machine busy starting a hundred tabs (HRN-901) that took longer than a step's wait.
	selftestShellWait = 45 * time.Second
	selftestStepWait  = 8 * time.Second
	selftestPoll      = 200 * time.Millisecond
	selftestAfterBoot = 15 * time.Second
)

// selftestPanes is what the self-test asks of tmux; *tmux.Tmux is one, a test brings its own.
type selftestPanes interface {
	Names() tmux.Names
	CreateSession(name string, cols, rows int, cwd string) error
	KillSession(name string)
	Display(name, format string) (string, bool)
	PaneCurrentCommand(name string) string
	SendKeys(name, line string) error
	SendRawKeys(name, keys string) error
	Capture(name string) (string, bool)
}

type selftest struct {
	panes selftestPanes
	sleep func(time.Duration)
	now   func() time.Time
}

func newSelftest(t *tmux.Tmux) selftest {
	return selftest{panes: t, sleep: time.Sleep, now: time.Now}
}

// waitFor polls cond until it holds or the wait is over; false when it never held (or ctx ended).
func (s selftest) waitFor(ctx context.Context, wait time.Duration, cond func() bool) bool {
	until := s.now().Add(wait)
	for {
		if cond() {
			return true
		}
		if ctx.Err() != nil || !s.now().Before(until) {
			return false
		}
		s.sleep(selftestPoll)
	}
}

// selftestReady is what the shell prints when it reads input; selftestReadyTyped is the word as typed, which prints it.
const (
	selftestReady      = "hs-shell-ready"
	selftestReadyTyped = "hs-shell-''ready"
)

func selftestName(prefix string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return prefix + "selftest-" + hex.EncodeToString(b[:])
}

// run tries the three steps in one throwaway pane; a step that cannot start because the one before failed says so.
func (s selftest) run(ctx context.Context) []machineCheck {
	shell := machineCheck{ID: "pane_shell", Title: "Терминал вкладки: пустая оболочка видна"}
	program := machineCheck{ID: "pane_program", Title: "Терминал вкладки: запущенная программа видна"}
	keys := machineCheck{ID: "pane_keys", Title: "Терминал вкладки: ввод доходит до программы"}
	skipped := func(c *machineCheck) {
		c.Detail = "не проверено: предыдущий шаг не прошёл"
	}

	name := selftestName(s.panes.Names().Prefix)
	if err := s.panes.CreateSession(name, 80, 24, ""); err != nil {
		shell.Detail = "панель не создалась: " + err.Error()
		skipped(&program)
		skipped(&keys)
		return []machineCheck{shell, program, keys}
	}
	defer s.panes.KillSession(name)

	// 1. The bare shell. What tmux says on its own goes into the detail when it differs: that is HRN-777's sign.
	var seen string
	shell.OK = s.waitFor(ctx, selftestShellWait, func() bool {
		seen = s.panes.PaneCurrentCommand(name)
		return tmux.SafeShells[seen]
	})
	raw, _ := s.panes.Display(name, "#{pane_current_command}")
	switch {
	case !shell.OK && seen == "":
		shell.Detail = "tmux не сказал, что запущено в панели"
	case !shell.OK:
		shell.Detail = "в пустой панели видно «" + seen + "», а не оболочку"
	case raw != seen:
		shell.Detail = "tmux отвечает «" + raw + "», рантайм видит «" + seen + "» по /proc"
	}
	// The shell is in the foreground from its first instant, before its rc files have run; what is typed then waits
	// for them. A line it answers says it reads input now, so the program's step below times the program alone: on
	// 1.187.10 a slow rc under load used up that step's wait and the release was taken back (HRN-901). The mark is
	// split by quotes so that the echo of the typed line is not taken for the answer.
	if shell.OK {
		if err := s.panes.SendKeys(name, "echo "+selftestReadyTyped); err != nil {
			shell.OK, shell.Detail = false, "команда не ушла в панель: "+err.Error()
		} else if !s.waitFor(ctx, selftestShellWait, func() bool {
			screen, _ := s.panes.Capture(name)
			return strings.Contains(screen, selftestReady)
		}) {
			shell.OK, shell.Detail = false, "оболочка в панели не ответила на ввод"
		}
	}
	if !shell.OK {
		skipped(&program)
		skipped(&keys)
		return []machineCheck{shell, program, keys}
	}

	// 2. A program in the foreground, then the shell again.
	if err := s.panes.SendKeys(name, "sleep 30"); err != nil {
		program.Detail = "команда не ушла в панель: " + err.Error()
	} else if !s.waitFor(ctx, selftestStepWait, func() bool { seen = s.panes.PaneCurrentCommand(name); return seen == "sleep" }) {
		program.Detail = "вместо sleep видно «" + seen + "»"
	} else if err := s.panes.SendRawKeys(name, "C-c"); err != nil {
		program.Detail = "Ctrl-C не ушёл в панель: " + err.Error()
	} else if !s.waitFor(ctx, selftestStepWait, func() bool { seen = s.panes.PaneCurrentCommand(name); return tmux.SafeShells[seen] }) {
		program.Detail = "после Ctrl-C видно «" + seen + "», а не оболочку"
	} else {
		program.OK = true
	}
	if !program.OK {
		skipped(&keys)
		return []machineCheck{shell, program, keys}
	}

	// 3. A key typed into a program that waits for it, read back from the screen. `read` and `echo` mean the same in
	// sh, bash, zsh and fish.
	const mark = "hs-selftest:"
	if err := s.panes.SendKeys(name, "read hs_answer; echo "+mark+"$hs_answer"); err != nil {
		keys.Detail = "команда не ушла в панель: " + err.Error()
		return []machineCheck{shell, program, keys}
	}
	s.sleep(300 * time.Millisecond)
	var screen string
	if err := s.panes.SendRawKeys(name, "2"); err != nil {
		keys.Detail = "клавиша не ушла в панель: " + err.Error()
	} else if err := s.panes.SendRawKeys(name, "Enter"); err != nil {
		keys.Detail = "Enter не ушёл в панель: " + err.Error()
	} else if !s.waitFor(ctx, selftestStepWait, func() bool {
		screen, _ = s.panes.Capture(name)
		return strings.Contains(screen, mark+"2")
	}) {
		keys.Detail = "ответ программы не появился на экране панели"
	} else {
		keys.OK = true
	}
	return []machineCheck{shell, program, keys}
}

// failed is the checks that did not pass, as one line for a log.
func selftestFailed(checks []machineCheck) string {
	var bad []string
	for _, c := range checks {
		if !c.OK {
			bad = append(bad, c.ID+": "+c.Detail)
		}
	}
	return strings.Join(bad, "; ")
}

// selftestRoutes: GET /api/cli/selftest[?agent=1[&claude=PATH]] runs the self-test for `harnessly selftest` → {ok, checks}.
func selftestRoutes(d deps) []gate.Route {
	return []gate.Route{{
		Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/selftest") },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d.CLI == nil || !d.CLI.OK(r) {
				peerJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			if d.Tmux == nil {
				peerJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no tmux"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
			defer cancel()
			checks := newSelftest(d.Tmux).run(ctx)
			// ?agent=1 adds the agent's scenario on a real Claude (a model's turn: it costs a little), ?claude=PATH
			// tries that binary instead of the machine's.
			if q := r.URL.Query(); q.Get("agent") == "1" && selftestFailed(checks) == "" {
				a, err := newAgentSelftest(d.Tmux, d.DataDir, q.Get("claude"))
				if err != nil {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				selfKeeper.run.Lock()
				checks = append(checks, a.run(ctx)...)
				selfKeeper.run.Unlock()
			}
			peerJSON(w, http.StatusOK, map[string]any{"ok": selftestFailed(checks) == "", "checks": checks})
		}),
	}}
}
