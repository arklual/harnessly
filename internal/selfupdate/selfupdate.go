// Package selfupdate keeps the programs of a machine connected to a hub the same as the ones the hub serves
// (<hub>/dl/manifest.json, the file install.sh downloads from). It compares the SHA-256 of the installed files with the
// manifest, downloads what differs, checks the download against the manifest and puts it in place with a rename, so a
// running program is never cut. Then the caller restarts what it replaced.
//
// The files are compared, not version numbers: the hub's binaries may all be "dev" builds, and a machine must still get
// the hub's latest ones.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultInterval is the time between checks.
	DefaultInterval = time.Hour
	// FirstDelay is the wait before the first check after a start: an update right after one would restart the
	// program before the page has had a look at it.
	FirstDelay   = 2 * time.Minute
	manifestMax  = 1 << 20
	binaryMax    = 512 << 20
	timeout      = 10 * time.Minute
	manifestWait = 20 * time.Second
)

// File is one installed program: its name in the manifest ("agent", "runtime") and where it is.
type File struct {
	Name string
	Path string
	// Optional: a hub that does not serve it is no error, and a machine without it gets it.
	Optional bool
}

// Status is what the page is told.
type Status struct {
	// Mode is "auto" when this machine can replace its programs itself, "manual" when it needs Command.
	Mode string `json:"mode"`
	// Checked says whether a check has been made since the start.
	Checked bool `json:"checked"`
	// UpToDate: the installed programs are the hub's (meaningful once Checked).
	UpToDate bool `json:"upToDate"`
	// Applying: an update is being downloaded and put in place.
	Applying bool `json:"applying"`
	// Version is what the hub calls its programs (may be "dev").
	Version string `json:"version,omitempty"`
	// CheckedAt is the time of the last check, RFC 3339.
	CheckedAt string `json:"checkedAt,omitempty"`
	// Error is the last check's or update's failure, for the person.
	Error string `json:"error,omitempty"`
	// Command updates this machine from its shell (run where it was installed).
	Command string `json:"command"`
}

// Updater checks and applies. Set HubURL, Files, GOOS and GOARCH.
type Updater struct {
	HubURL string
	Files  []File
	GOOS   string
	GOARCH string
	// Auto makes Run apply an update by itself; otherwise it only checks.
	Auto bool
	// Sudo: the install command needs root (a system install).
	Sudo bool
	// Client makes the requests; nil means one that honours the proxy environment.
	Client *http.Client
	// Interval is the time between checks; zero means DefaultInterval.
	Interval time.Duration
	// Applied runs after the files were replaced, with the names of those replaced (the caller restarts them).
	Applied func(names []string)
	// Log gets one line per failure; may be nil.
	Log func(msg string, err error)

	mu       sync.Mutex
	st       Status
	applying bool
}

type manifest struct {
	Version string `json:"version"`
	Files   map[string]struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"files"`
}

type stale struct {
	File
	key    string
	sha256 string
	size   int64
}

// Command is the shell command that updates this machine by the hub's installer.
func (u *Updater) Command() string {
	sh := "sh"
	if u.Sudo {
		sh = "sudo sh"
	}
	return fmt.Sprintf("curl -fsSL %s/install | %s -s -- --update", strings.TrimRight(u.HubURL, "/"), sh)
}

// Writable says whether every file's folder lets this process put a new file there (and so replace the file).
func (u *Updater) Writable() bool {
	if len(u.Files) == 0 {
		return false
	}
	for _, f := range u.Files {
		tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".harnessly-write-check-*")
		if err != nil {
			return false
		}
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	return true
}

func (u *Updater) mode() string {
	if u.Writable() {
		return "auto"
	}
	return "manual"
}

// Status is the last answer.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.st
	s.Applying = u.applying
	s.Command = u.Command()
	if s.Mode == "" {
		s.Mode = u.mode()
	}
	return s
}

func (u *Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
}

func (u *Updater) get(ctx context.Context, path string, wait time.Duration) (*http.Response, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(u.HubURL, "/")+path, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "harnessly-runtime")
	res, err := u.client().Do(req)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		cancel()
		return nil, nil, fmt.Errorf("%s: HTTP %d", path, res.StatusCode)
	}
	return res, cancel, nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// check reads the manifest and lists the files that differ from it.
func (u *Updater) check(ctx context.Context) (string, []stale, error) {
	res, cancel, err := u.get(ctx, "/dl/manifest.json", manifestWait)
	if err != nil {
		return "", nil, err
	}
	defer cancel()
	defer res.Body.Close()
	var m manifest
	if err := json.NewDecoder(io.LimitReader(res.Body, manifestMax)).Decode(&m); err != nil {
		return "", nil, fmt.Errorf("manifest: %w", err)
	}
	var out []stale
	for _, f := range u.Files {
		key := f.Name + "-" + u.GOOS + "-" + u.GOARCH
		want, ok := m.Files[key]
		if !ok && f.Optional {
			continue
		}
		if !ok {
			return m.Version, nil, fmt.Errorf("the hub has no %s", key)
		}
		have, err := fileSHA(f.Path)
		if errors.Is(err, os.ErrNotExist) && f.Optional {
			have, err = "", nil
		}
		if err != nil {
			return m.Version, nil, err
		}
		if !strings.EqualFold(have, want.SHA256) {
			out = append(out, stale{File: f, key: key, sha256: strings.ToLower(want.SHA256), size: want.Size})
		}
	}
	return m.Version, out, nil
}

func (u *Updater) record(version string, upToDate bool, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.st.Mode = u.mode()
	u.st.Checked = true
	u.st.UpToDate = upToDate
	if version != "" {
		u.st.Version = version
	}
	u.st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	u.st.Error = ""
	if err != nil {
		u.st.Error = err.Error()
	}
}

// Check compares the installed files with the hub's and records the answer.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	version, out, err := u.check(ctx)
	u.record(version, err == nil && len(out) == 0, err)
	return u.Status(), err
}

// ErrBusy: an update is already going on.
var ErrBusy = errors.New("an update is already going on")

// ErrManual: this machine cannot replace its own programs; the command is needed.
var ErrManual = errors.New("this machine cannot replace its own programs: run the update command")

// Apply checks and replaces what differs; Applied runs when something was replaced.
func (u *Updater) Apply(ctx context.Context) error {
	u.mu.Lock()
	if u.applying {
		u.mu.Unlock()
		return ErrBusy
	}
	u.applying = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.applying = false
		u.mu.Unlock()
	}()
	if !u.Writable() {
		u.record("", false, ErrManual)
		return ErrManual
	}
	version, out, err := u.check(ctx)
	if err != nil {
		u.record(version, false, err)
		return err
	}
	if len(out) == 0 {
		u.record(version, true, nil)
		return nil
	}
	// Download and check everything first, then rename: a failed download leaves the machine as it was.
	news := make([]string, 0, len(out))
	cleanup := func() {
		for _, n := range news {
			_ = os.Remove(n)
		}
	}
	for _, s := range out {
		n, err := u.download(ctx, s)
		if err != nil {
			cleanup()
			u.record(version, false, err)
			return err
		}
		news = append(news, n)
	}
	names := make([]string, 0, len(out))
	for i, s := range out {
		if err := os.Rename(news[i], s.Path); err != nil {
			cleanup()
			u.record(version, false, err)
			return err
		}
		names = append(names, s.Name)
	}
	u.record(version, true, nil)
	if u.Applied != nil {
		u.Applied(names)
	}
	return nil
}

// download writes the hub's file next to the installed one and checks it against the manifest.
func (u *Updater) download(ctx context.Context, s stale) (string, error) {
	res, cancel, err := u.get(ctx, "/dl/"+s.key, timeout)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer res.Body.Close()
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), "."+filepath.Base(s.Path)+".new-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(res.Body, binaryMax+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > binaryMax {
		err = fmt.Errorf("%s is larger than %d bytes", s.key, binaryMax)
	}
	if err == nil && s.size > 0 && n != s.size {
		err = fmt.Errorf("%s: got %d bytes, the manifest says %d", s.key, n, s.size)
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != s.sha256 {
		err = fmt.Errorf("%s does not match the hub's checksum", s.key)
	}
	if err == nil {
		err = os.Chmod(name, 0o755)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Run checks after FirstDelay and then every Interval until ctx ends, applying what it finds when Auto. Start it with go.
func (u *Updater) Run(ctx context.Context) {
	every := u.Interval
	if every <= 0 {
		every = DefaultInterval
	}
	wait := FirstDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		var err error
		if u.Auto && u.Writable() {
			err = u.Apply(ctx)
		} else {
			_, err = u.Check(ctx)
		}
		if err != nil && ctx.Err() == nil && u.Log != nil {
			u.Log("self-update failed", err)
		}
	}
}
