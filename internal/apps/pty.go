package apps

import (
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// A terminal of an app (terminal.pty, HRN-852): a shell on a pty of its own, in the chat's folder, inside the same wall
// as the app's commands (run.go): the person's keys and the runtime's state are not there, nor any secret of its
// environment. The person's page carries its bytes between the view and the runtime; the runtime never reads them.

const (
	// MaxAppTerminals is how many terminals an app keeps open in a chat; MaxTerminals how many all apps keep.
	MaxAppTerminals = 4
	MaxTerminals    = 16
	// MaxTerminalCols and MaxTerminalRows bound a terminal's size.
	MaxTerminalCols = 1000
	MaxTerminalRows = 500
)

// ErrTooManyTerminals: the app (or all apps) holds as many terminals as it may.
var ErrTooManyTerminals = &BridgeError{Code: CodeRateLimited, Message: "too many terminals open: close one first"}

// Terminal is a shell started on its pty.
type Terminal struct {
	// Pty is the terminal's side the runtime reads and writes.
	Pty  *os.File
	cmd  *exec.Cmd
	done chan struct{}
	code int
	once sync.Once
}

// shellArgv is the shell a terminal runs: bash when the machine has it, else sh, interactive either way.
func shellArgv() []string {
	if _, err := exec.LookPath("bash"); err == nil {
		return []string{"bash", "-i"}
	}
	return []string{"sh", "-i"}
}

// Shell starts a shell on a terminal of cols×rows in dir.
func (s *Sandbox) Shell(dir string, cols, rows int) (*Terminal, error) {
	if cols < 1 || rows < 1 || cols > MaxTerminalCols || rows > MaxTerminalRows {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "cols is from 1 to 1000, rows from 1 to 500"}
	}
	cmd, err := s.command(dir, shellArgv(), true)
	if err != nil {
		return nil, err
	}
	attrs := cmd.SysProcAttr
	f, err := pty.StartWithAttrs(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}, attrs)
	if err != nil {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: "the terminal did not start", Detail: err.Error()}
	}
	t := &Terminal{Pty: f, cmd: cmd, done: make(chan struct{}), code: -1}
	go func() {
		err := cmd.Wait()
		var exit *exec.ExitError
		switch {
		case err == nil:
			t.code = 0
		case errors.As(err, &exit):
			t.code = exit.ExitCode()
		}
		close(t.done)
	}()
	return t, nil
}

// Resize sets the terminal's size.
func (t *Terminal) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > MaxTerminalCols || rows > MaxTerminalRows {
		return &BridgeError{Code: CodeInvalidParams, Message: "cols is from 1 to 1000, rows from 1 to 500"}
	}
	return pty.Setsize(t.Pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Done is closed when the shell has ended; Code is then how.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// Code is how the shell ended: its exit code, -1 for a signal.
func (t *Terminal) Code() int {
	<-t.done
	return t.code
}

// Close ends the shell and everything it started, and the pty.
func (t *Terminal) Close() {
	t.once.Do(func() {
		kill(t.cmd)
		<-t.done
		_ = t.Pty.Close()
	})
}

// TerminalSlots counts the open terminals of the apps: each app in each chat, and all of them.
type TerminalSlots struct {
	mu  sync.Mutex
	of  map[string]int
	all int
}

// Take holds a place for a terminal of app in chat; release gives it back (once).
func (l *TerminalSlots) Take(app, chat string) (release func(), err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := app + "\x00" + chat
	if l.of == nil {
		l.of = map[string]int{}
	}
	if l.of[key] >= MaxAppTerminals || l.all >= MaxTerminals {
		return nil, ErrTooManyTerminals
	}
	l.of[key]++
	l.all++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.of[key]--
			l.all--
			if l.of[key] == 0 {
				delete(l.of, key)
			}
		})
	}, nil
}
