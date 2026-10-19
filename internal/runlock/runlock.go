// Package runlock is the single-instance lock of a machine's runtime: a file in the data folder naming the process that
// holds it and its port. Whoever starts second asks that port's health route; a runtime that answers there keeps the
// lock and the newcomer gives up, so two runtimes never write one data folder. The file is the previous runtime's own,
// so either of them sees the other.
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

	"harnessly/internal/jsvalue"
)

// Lock is a lock this process holds.
type Lock struct {
	file string
	pid  int
}

// HeldError: a live runtime holds the lock.
type HeldError struct{ PID, Port int }

func (e *HeldError) Error() string {
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

func write(file string, body []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
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

// Acquire takes the lock for this process on port: a lock left by a process that is gone, or by one that is not a
// runtime, is taken over; one held by a runtime that answers on its port is a *HeldError.
func Acquire(cfg Config, port int) (*Lock, error) {
	if cfg.Health == nil {
		cfg.Health = health
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pid := os.Getpid()
	l := &Lock{file: cfg.File, pid: pid}
	if err := os.MkdirAll(filepath.Dir(cfg.File), 0o777); err != nil {
		return nil, err
	}
	body := content(pid, port, cfg.Now())
	err := write(cfg.File, body)
	if err == nil {
		return l, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	reclaim := func() (*Lock, error) {
		_ = os.Remove(cfg.File)
		return l, write(cfg.File, body)
	}
	raw, rerr := os.ReadFile(cfg.File)
	var existing data
	if rerr != nil || json.Unmarshal(raw, &existing) != nil {
		return reclaim()
	}
	holder := 0
	if existing.PID != nil {
		holder = int(*existing.PID)
	}
	if holder == pid {
		return l, nil
	}
	if !alive(holder) {
		return reclaim()
	}
	holderPort := 0
	if existing.Port != nil {
		holderPort = int(*existing.Port)
	}
	if app, ok := cfg.Health(holderPort); ok && app == cfg.App {
		return nil, &HeldError{PID: holder, Port: holderPort}
	}
	return reclaim()
}

// Release removes the lock while it is still this process's.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	raw, err := os.ReadFile(l.file)
	if err != nil {
		return
	}
	var d data
	if json.Unmarshal(raw, &d) == nil && d.PID != nil && int(*d.PID) == l.pid {
		_ = os.Remove(l.file)
	}
}
