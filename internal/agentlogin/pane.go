package agentlogin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// A login that needs a terminal runs in a pane of the agent's own tmux server (-L): the person's tmux and the runtime's
// tabs are never touched, and the pane is as wide as a long sign-in address needs, so the address is one line.
const (
	paneCols = 300
	paneRows = 50
	// tmuxTimeout bounds one tmux command.
	tmuxTimeout = 5 * time.Second
)

// pane is one program running in a pane of the agent's tmux server.
type pane struct {
	bin    string // the tmux program
	socket string // the server's -L name
	name   string // the session
	script string // the launcher file to remove
}

func (p *pane) tmux(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, tmuxTimeout)
	defer cancel()
	// -f /dev/null: the server of a login reads no configuration file, whoever's account it runs in.
	cmd := exec.CommandContext(ctx, p.bin, append([]string{"-L", p.socket, "-f", "/dev/null"}, args...)...)
	// A tmux the agent was started from is not this server.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// shQuote single-quotes s for sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// startPane runs cmd (a login shell command from preflight.Checker.LoginCmd) in a new pane. The command is written to
// a launcher file first, so the pane's own shell sees a path and nothing to quote, whichever shell it is; the file
// holds the script and the environment names the login shell gets, and no secret. dir is where launchers are made.
func startPane(ctx context.Context, tmuxBin, socket, name, dir string, cmd *exec.Cmd) (*pane, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, "login-*.sh")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\ncd " + shQuote(cmd.Dir) + " || exit 1\nexec env -i")
	for _, kv := range cmd.Env {
		b.WriteString(" " + shQuote(kv))
	}
	for _, a := range append([]string{cmd.Path}, cmd.Args[1:]...) {
		b.WriteString(" " + shQuote(a))
	}
	b.WriteString("\n")
	_, werr := f.WriteString(b.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(f.Name())
		return nil, werr
	}
	p := &pane{bin: tmuxBin, socket: socket, name: name, script: f.Name()}
	// A session left by an agent that was killed mid-login is not ours any more.
	_, _ = p.tmux(ctx, "kill-session", "-t", name)
	// remain-on-exit keeps the screen of a program that has ended: its last words say why. It is set in the same
	// command line as the session, before the program gets a chance to end.
	_, err = p.tmux(ctx, "new-session", "-d", "-s", name, "-x", strconv.Itoa(paneCols), "-y", strconv.Itoa(paneRows),
		"-c", cmd.Dir, "/bin/sh "+shQuote(f.Name()), ";", "set-option", "-w", "-t", name, "remain-on-exit", "on")
	if err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

// screen is what the pane shows, wrapped lines joined.
func (p *pane) screen(ctx context.Context) (string, error) {
	return p.tmux(ctx, "capture-pane", "-p", "-J", "-t", p.name)
}

// exited says whether the program has ended, and with what exit code.
func (p *pane) exited(ctx context.Context) (bool, int, error) {
	out, err := p.tmux(ctx, "display-message", "-p", "-t", p.name, "#{pane_dead} #{pane_dead_status}")
	if err != nil {
		// A session that is gone has ended too.
		if p.gone(ctx) {
			return true, -1, nil
		}
		return false, 0, err
	}
	dead, status, _ := strings.Cut(strings.TrimSpace(out), " ")
	code, _ := strconv.Atoi(status)
	return dead == "1", code, nil
}

func (p *pane) gone(ctx context.Context) bool {
	_, err := p.tmux(ctx, "has-session", "-t", p.name)
	return err != nil
}

// typeLine types text and presses Enter. The text goes literally (-l): no key names are looked up in it.
func (p *pane) typeLine(ctx context.Context, text string) error {
	if _, err := p.tmux(ctx, "send-keys", "-t", p.name, "-l", "--", text); err != nil {
		return errors.New("tmux send-keys failed")
	}
	if _, err := p.tmux(ctx, "send-keys", "-t", p.name, "Enter"); err != nil {
		return errors.New("tmux send-keys failed")
	}
	return nil
}

// close ends the session (the server goes with the last one) and removes the launcher.
func (p *pane) close() {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxTimeout)
	defer cancel()
	_, _ = p.tmux(ctx, "kill-session", "-t", p.name)
	if p.script != "" {
		_ = os.Remove(p.script)
	}
}
