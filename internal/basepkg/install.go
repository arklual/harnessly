package basepkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Status of an item on a machine.
type Status string

const (
	OK          Status = "ok"
	Missing     Status = "missing"      // not in place and nobody here installs it (the person runs the command)
	Installing  Status = "installing"   // being installed now
	NoAccess    Status = "no_access"    // the install failed on the network
	WaitingDisk Status = "waiting_disk" // not enough free disk for it
	Deferred    Status = "deferred"     // a heavy item waits while the machine is busy
	Skipped     Status = "skipped"      // the person said they do without it
	Failed      Status = "error"
)

// ItemState is how an item stands on the machine.
type ItemState struct {
	ID      string `json:"id"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
	// Host is the address the install could not reach (no_access).
	Host string `json:"host,omitempty"`
}

// State is what the service saw and did last, in StateFile of its own folder.
type State struct {
	Hash     string      `json:"hash"`
	At       int64       `json:"at"`
	Manager  Manager     `json:"manager,omitempty"`
	Running  bool        `json:"running,omitempty"`
	Items    []ItemState `json:"items"`
	ProxySet bool        `json:"proxySet,omitempty"`
}

// The files of the inbox: the folder the runtime (the person's side) writes and the root service only reads.
const (
	StateFile   = "state.json"
	SkipFile    = "skip.json"     // ["chrome", …]: the items the person does without
	ProxyFile   = "proxy"         // one address: http, https or socks5, without a login
	RequestFile = "request"       // its change asks for an install now; the content is not read
	maxInbox    = 4 << 10         // the service never reads more of a file of the inbox
	freeAfter   = uint64(2) << 30 // an item is installed only when this much stays free after it
)

// Inbox is what the person's side asked for. It is read with care: the folder is not root's.
type Inbox struct {
	Skip  map[string]bool
	Proxy string
}

// readSmall reads a regular file of the inbox, at most maxInbox bytes. It is opened once, never through a link, and
// checked on the open handle: the folder is the unprivileged side's, so it may change between a look and a read.
func readSmall(path string) ([]byte, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxInbox {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(f, maxInbox+1))
	if err != nil || len(b) > maxInbox {
		return nil, false
	}
	return b, true
}

// ReadInbox reads the inbox; anything it does not understand counts as not asked.
func ReadInbox(dir string) Inbox {
	in := Inbox{Skip: map[string]bool{}}
	if b, ok := readSmall(filepath.Join(dir, SkipFile)); ok {
		var ids []string
		if json.Unmarshal(b, &ids) == nil {
			for _, id := range ids {
				if _, known := ByID(id); known {
					in.Skip[id] = true
				}
			}
		}
	}
	if b, ok := readSmall(filepath.Join(dir, ProxyFile)); ok {
		if p, err := CheckProxy(strings.TrimSpace(string(b))); err == nil {
			in.Proxy = p
		}
	}
	return in
}

// CheckProxy accepts a proxy address for the package manager: http, https or socks5, a host, no login in it.
func CheckProxy(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("пустой адрес")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("нужен адрес вида http://proxy:3128")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", errors.New("прокси бывает http, https или socks5")
	}
	if u.User != nil {
		return "", errors.New("логин и пароль прокси — только через сейф, не в адресе")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || strings.ContainsAny(raw, " \t\r\n") {
		return "", errors.New("в адресе прокси только схема, хост и порт")
	}
	return u.Scheme + "://" + u.Host, nil
}

// Installer installs the system part of the list. Every outside effect goes through its fields, so tests replace them.
type Installer struct {
	Manager Manager
	// Run runs a command with the extra environment; out is its output (stdout and stderr).
	Run      func(ctx context.Context, env []string, args []string) (out string, err error)
	LookPath func(string) (string, error)
	// Free is the free disk where packages go, in bytes.
	Free func() (uint64, error)
	// Busy says the machine is loaded (the load average above the cores × 2).
	Busy func() bool
	Now  func() time.Time
	// Save is called with the state as it goes, so the card sees progress.
	Save func(State)
}

func (in *Installer) present(it Item) bool {
	for _, b := range it.Bins {
		if _, err := in.LookPath(b); err == nil {
			return true
		}
	}
	return false
}

// netFail is an install output that says the network failed, and the host when it names one.
var (
	netWords = []string{"could not resolve", "temporary failure resolving", "failed to fetch", "connection timed out",
		"network is unreachable", "connection refused", "could not connect", "curl error", "cannot download",
		"failed to download", "no route to host", "unable to connect"}
	hostRe = regexp.MustCompile(`(?i)(?:https?|ftp)://([a-z0-9.-]+)|resolv(?:e|ing) '?([a-z0-9.-]+\.[a-z]{2,})'?`)
)

func netFail(out string) (bool, string) {
	low := strings.ToLower(out)
	hit := false
	for _, w := range netWords {
		if strings.Contains(low, w) {
			hit = true
			break
		}
	}
	if !hit {
		return false, ""
	}
	if m := hostRe.FindStringSubmatch(out); m != nil {
		return true, m[1] + m[2]
	}
	return true, ""
}

// lastLine is the last meaningful line of an output, short, for the card.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" {
			if len(l) > 200 {
				l = l[:200] + "…"
			}
			return l
		}
	}
	return ""
}

// Install brings the system part in place: each item on its own, a failure of one does not stop the others.
func (in *Installer) Install(ctx context.Context, box Inbox) State {
	st := State{Hash: Hash(), At: in.Now().UnixMilli(), Manager: in.Manager, Running: true, ProxySet: box.Proxy != ""}
	items := System()
	st.Items = make([]ItemState, len(items))
	for i, it := range items {
		st.Items[i] = ItemState{ID: it.ID, Status: Missing}
	}
	// The proxy comes from the unprivileged side, so the package manager goes direct first and through the proxy only
	// after the direct way failed on the network. The packages are checked by the manager's signatures either way.
	direct := []string{"DEBIAN_FRONTEND=noninteractive"}
	viaProxy := direct
	if box.Proxy != "" {
		viaProxy = append(append([]string{}, direct...), "http_proxy="+box.Proxy, "https_proxy="+box.Proxy, "HTTP_PROXY="+box.Proxy, "HTTPS_PROXY="+box.Proxy)
	}
	run := func(args []string) (string, error) {
		out, err := in.Run(ctx, direct, args)
		if err != nil && box.Proxy != "" {
			if bad, _ := netFail(out); bad {
				return in.Run(ctx, viaProxy, args)
			}
		}
		return out, err
	}
	refreshed := false
	for i, it := range items {
		set := func(s Status, msg, host string) {
			st.Items[i] = ItemState{ID: it.ID, Status: s, Message: msg, Host: host}
		}
		switch {
		case ctx.Err() != nil:
			continue
		case in.present(it):
			set(OK, "", "")
			continue
		case box.Skip[it.ID]:
			set(Skipped, "", "")
			continue
		case len(it.Pkgs[in.Manager]) == 0:
			set(Missing, "на этой системе не ставится само", "")
			continue
		}
		if free, err := in.Free(); err == nil && free < uint64(it.SizeMB)<<20+freeAfter {
			set(WaitingDisk, fmt.Sprintf("нужно %d МБ и ещё 2 ГиБ свободных", it.SizeMB), "")
			continue
		}
		if it.Heavy && in.Busy() {
			set(Deferred, "машина занята — поставится позже", "")
			continue
		}
		set(Installing, "", "")
		in.Save(st)
		if r := RefreshArgs(in.Manager); r != nil && !refreshed {
			refreshed = true
			if out, err := run(r); err != nil {
				if bad, host := netFail(out); bad {
					set(NoAccess, "нет доступа к сети пакетов — без этого "+it.Without, host)
					continue
				}
			}
		}
		out, err := run(InstallArgs(in.Manager, it.Pkgs[in.Manager]))
		switch {
		case err == nil && in.present(it):
			set(OK, "", "")
		case err == nil:
			set(Failed, "поставилось, но "+strings.Join(it.Bins, " или ")+" не найден", "")
		default:
			if bad, host := netFail(out); bad {
				what := "сети пакетов"
				if host != "" {
					what = host
				}
				set(NoAccess, "нет доступа к "+what+" — без этого "+it.Without, host)
			} else {
				set(Failed, lastLine(out), "")
			}
		}
		in.Save(st)
	}
	st.Running = false
	st.At = in.Now().UnixMilli()
	in.Save(st)
	return st
}

// WriteState writes the state into dir (root's own folder) with a rename, so a reader never sees half of it.
func WriteState(dir string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, StateFile))
}

// ReadState reads the service's state; ok is false when there is none (no service on this machine).
func ReadState(dir string) (State, bool) {
	b, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		return State{}, false
	}
	var st State
	if json.Unmarshal(b, &st) != nil {
		return State{}, false
	}
	return st, true
}
