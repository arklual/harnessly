// Package runlock is the single-instance lock of a machine's runtime: an exclusive flock on a guard file in the data
// folder, held for as long as the process lives, so the kernel drops it when the process dies however it dies. Beside it
// lies the file naming the holder and its port: it is what a newcomer's refusal quotes, and what a runtime older than
// the guard is recognised by (that one is asked on its port's health route, as it asked itself).
package runlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// guardSuffix names the guard file after the lock file. The guard is never removed: unlinking a file others may hold
// open would let two processes lock two different files of one name.
const guardSuffix = ".flock"

// Lock is a lock this process holds.
type Lock struct {
	file  string
	pid   int
	guard *os.File
}

// HeldError: a live runtime holds the lock. PID and Port are zero when the holder has not named itself yet.
type HeldError struct{ PID, Port int }

func (e *HeldError) Error() string {
	if e.PID == 0 {
		return "another runtime is already running (it holds the lock of this data folder)"
	}
	return fmt.Sprintf("another runtime is already running (pid=%d, port=%d)", e.PID, e.Port)
}

// Config says where the lock is and how a holder is recognised.
type Config struct {
	File string
	// App is the name a runtime's health route answers with ("app").
	App string
	// Health asks a port's health route for its app name; nil asks http://127.0.0.1:<port>/api/health.
	Health func(port int) (string, bool)
	Now    func() time.Time
}

type data struct {
	PID  *float64 `json:"pid"`
	Port *float64 `json:"port"`
}

// content is JSON.stringify({pid, port, startedAt}, null, 2) and a newline.
func content(pid, port int, startedAt time.Time) []byte {
	o := jsvalue.NewObject("pid", float64(pid), "port", float64(port), "startedAt", float64(startedAt.UnixMilli()))
	return append(jsvalue.Stringify(o, "  "), '\n')
}

// write puts body in place of file in one step, so a reader never sees half of it.
func write(file string, body []byte) error {
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// errBusy: another process holds the guard.
var errBusy = errors.New("lock held")

// lockGuard takes the exclusive flock on file without waiting. A nil file with a nil error is a filesystem that has no
// flock: the holder file's rules then stand alone, as they did before the guard.
func lockGuard(file string) (*os.File, error) {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	switch err {
	case nil:
		return f, nil
	case syscall.EWOULDBLOCK:
		f.Close()
		return nil, errBusy
	case syscall.ENOLCK, syscall.ENOSYS, syscall.ENOTSUP, syscall.EINVAL:
		f.Close()
		return nil, nil
	}
	f.Close()
	return nil, err
}

// holder is who the lock file names: zeros when it is missing or does not parse.
func holder(file string) (pid, port int) {
	raw, err := os.ReadFile(file)
	var d data
	if err != nil || json.Unmarshal(raw, &d) != nil {
		return 0, 0
	}
	if d.PID != nil {
		pid = int(*d.PID)
	}
	if d.Port != nil {
		port = int(*d.Port)
	}
	return pid, port
}

// alive is process.kill(pid, 0) succeeding: a process another user owns counts as gone, as it did.
func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func health(port int) (string, bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/health")
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var v struct {
		App any `json:"app"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	app, ok := v.App.(string)
	return app, ok
}

// Acquire takes the lock for this process on port. While another process holds the guard it is a *HeldError, whether
// or not that process answers on its port. With the guard taken, the holder file is only a leftover: of a process that
// is gone, or of a runtime older than the guard, which is a *HeldError while it answers on its port.
func Acquire(cfg Config, port int) (*Lock, error) {
	if cfg.Health == nil {
		cfg.Health = health
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pid := os.Getpid()
	if err := os.MkdirAll(filepath.Dir(cfg.File), 0o777); err != nil {
		return nil, err
	}
	guard, err := lockGuard(cfg.File + guardSuffix)
	heldPID, heldPort := holder(cfg.File)
	if errors.Is(err, errBusy) {
		if heldPID == pid {
			return &Lock{file: cfg.File, pid: pid}, nil
		}
		return nil, &HeldError{PID: heldPID, Port: heldPort}
	}
	if err != nil {
		return nil, err
	}
	l := &Lock{file: cfg.File, pid: pid, guard: guard}
	if heldPID != pid && alive(heldPID) {
		if app, ok := cfg.Health(heldPort); ok && app == cfg.App {
			l.unlock()
			return nil, &HeldError{PID: heldPID, Port: heldPort}
		}
	}
	if err := write(cfg.File, content(pid, port, cfg.Now())); err != nil {
		l.unlock()
		return nil, err
	}
	return l, nil
}

func (l *Lock) unlock() {
	if l.guard != nil {
		_ = l.guard.Close()
		l.guard = nil
	}
}

// Release removes the holder file while it is still this process's, then lets the guard go.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	defer l.unlock()
	if pid, _ := holder(l.file); pid == l.pid {
		_ = os.Remove(l.file)
	}
}
