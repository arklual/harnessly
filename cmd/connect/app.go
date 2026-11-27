package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/connect"
)

// app is Connect as the tray and the console run it: the link, its state, the journal.
type app struct {
	mu      sync.Mutex
	dev     *connect.Device
	cancel  context.CancelFunc
	done    chan struct{}
	online  bool
	why     string
	onState func()
	log     *log.Logger
}

func newApp() *app {
	a := &app{log: log.New(openJournal(), "", log.LstdFlags)}
	go a.updater(context.Background())
	return a
}

func (a *app) state() (paired bool, name, url string, running, online bool, why string, commands int) {
	c, err := load()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dev != nil {
		commands = a.dev.Running()
	}
	return err == nil, c.Name, c.URL, a.cancel != nil, a.online, a.why, commands
}

func (a *app) changed() {
	if a.onState != nil {
		a.onState()
	}
}

// start links the computer, unless it already is.
func (a *app) start() error {
	c, err := load()
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &connect.Device{Cfg: c, Version: version, Logf: a.log.Printf}
	d.Status = func(online bool, why string) {
		a.mu.Lock()
		a.online, a.why = online, why
		a.mu.Unlock()
		a.changed()
	}
	done := make(chan struct{})
	a.dev, a.cancel, a.done, a.online, a.why = d, cancel, done, false, "подключается…"
	a.mu.Unlock()
	go func() {
		defer close(done)
		err := d.Run(ctx)
		a.mu.Lock()
		if a.dev == d {
			a.dev, a.cancel, a.online = nil, nil, false
			if err != nil && !errors.Is(err, context.Canceled) {
				a.why = err.Error()
			} else {
				a.why = "отключено"
			}
		}
		a.mu.Unlock()
		a.changed()
	}()
	a.changed()
	return nil
}

// stop is «Отключить»: every command of the agents here is killed, then the link goes.
func (a *app) stop() {
	a.mu.Lock()
	d, cancel, done := a.dev, a.cancel, a.done
	a.mu.Unlock()
	if d == nil {
		return
	}
	n := d.KillAll()
	a.log.Printf("отключено вручную, убито команд: %d", n)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// pair registers this computer with a runtime by its one-time code and keeps the key.
func pair(base, code, name string) (connect.DeviceConfig, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return connect.DeviceConfig{}, err
	}
	body, _ := json.Marshal(map[string]any{"code": strings.TrimSpace(code), "name": name, "os": runtime.GOOS, "arch": runtime.GOARCH, "pub": []byte(pub)})
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Post(base+"/connect/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return connect.DeviceConfig{}, fmt.Errorf("нет связи с %s: %w", base, err)
	}
	defer resp.Body.Close()
	var out struct{ ID, Name, Error string }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		if out.Error == "" {
			out.Error = resp.Status
		}
		return connect.DeviceConfig{}, errors.New(out.Error)
	}
	cfg := connect.DeviceConfig{URL: base, ID: out.ID, Name: out.Name, Key: priv}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
		return cfg, err
	}
	return cfg, os.WriteFile(configPath(), raw, 0o600)
}

// The journal: every command and every change to a file the agents made, in the person's cache folder; 50 MB, then
// it starts over and keeps the previous one as .1.
const journalMax = 50 << 20

func journalPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "harnessly-connect", "journal.log")
}

type journal struct {
	mu sync.Mutex
	f  *os.File
	n  int64
}

func openJournal() io.Writer {
	p := journalPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return os.Stderr
	}
	st, _ := f.Stat()
	j := &journal{f: f}
	if st != nil {
		j.n = st.Size()
	}
	return io.MultiWriter(j, os.Stderr)
}

func (j *journal) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.n+int64(len(p)) > journalMax {
		name := j.f.Name()
		j.f.Close()
		_ = os.Rename(name, name+".1")
		f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return len(p), nil
		}
		j.f, j.n = f, 0
	}
	n, err := j.f.Write(p)
	j.n += int64(n)
	return n, err
}
