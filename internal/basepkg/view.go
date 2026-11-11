package basepkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ViewItem is one line of the base package on the machine's card.
type ViewItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Without string `json:"without"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
	Host    string `json:"host,omitempty"`
	// Command installs it by hand (the machine has no service that does).
	Command string `json:"command,omitempty"`
	// Optional: the item only adds a feature; without it the base package is still done.
	Optional bool `json:"optional,omitempty"`
}

// View is GET <platform>/basepkg: how the base package stands here.
type View struct {
	// Service: a root service installs the system part on this machine; without it the person runs Command.
	Service bool       `json:"service"`
	Running bool       `json:"running"`
	Done    bool       `json:"done"` // nothing is left to do: every item but the optional ones is ok or skipped
	Proxy   string     `json:"proxy"`
	Command string     `json:"command,omitempty"` // everything missing in one line
	At      int64      `json:"at,omitempty"`
	Items   []ViewItem `json:"items"`
}

// Voice is the personal item's state, as the runtime's speech engine says.
type Voice struct {
	Off       bool // the runtime does not recognize speech here (an engine of the person's own, or not this machine)
	Ready     bool
	Preparing bool
	Error     string
}

// Look is what the view reads on this machine.
type Look struct {
	LookPath func(string) (string, error)
	Manager  Manager
	// State is the service's last state; HasService says there is a service at all.
	State      State
	HasService bool
	Inbox      Inbox
	Voice      Voice
}

func (l Look) present(it Item) bool {
	for _, b := range it.Bins {
		if _, err := l.LookPath(b); err == nil {
			return true
		}
	}
	return false
}

// MakeView puts together the card's lines. What is on PATH now wins over what the service wrote.
func MakeView(l Look) View {
	v := View{Service: l.HasService, Running: l.HasService && l.State.Running, Proxy: l.Inbox.Proxy, At: l.State.At, Items: []ViewItem{}}
	seen := map[string]ItemState{}
	if l.HasService && l.State.Hash == Hash() {
		for _, s := range l.State.Items {
			seen[s.ID] = s
		}
	}
	var missing []Item
	v.Done = true
	for _, it := range List {
		line := ViewItem{ID: it.ID, Title: it.Title, Without: it.Without, Optional: it.Optional}
		switch {
		case it.Personal:
			line.Status, line.Message, line.Host = voiceStatus(l, it)
		case l.present(it):
			line.Status = OK
		case l.Inbox.Skip[it.ID]:
			line.Status = Skipped
		case l.HasService:
			s, ok := seen[it.ID]
			if !ok || s.Status == OK {
				s = ItemState{Status: Installing, Message: "поставится при следующем запуске службы"}
				if !l.State.Running {
					s.Status = Missing
				}
			}
			line.Status, line.Message, line.Host = s.Status, s.Message, s.Host
		default:
			line.Status = Missing
			line.Command = Command(l.Manager, []Item{it})
			if line.Command != "" && !it.Optional {
				missing = append(missing, it)
			}
		}
		if line.Status != OK && line.Status != Skipped && !it.Optional {
			v.Done = false
		}
		v.Items = append(v.Items, line)
	}
	v.Command = Command(l.Manager, missing)
	return v
}

func voiceStatus(l Look, it Item) (Status, string, string) {
	switch {
	case l.Voice.Off || l.Voice.Ready:
		return OK, "", ""
	case l.Inbox.Skip[it.ID]:
		return Skipped, "", ""
	case l.Voice.Preparing:
		return Installing, "", ""
	case l.Voice.Error != "":
		low := strings.ToLower(l.Voice.Error)
		for _, h := range it.Hosts {
			if strings.Contains(low, h) || strings.Contains(low, "network") || strings.Contains(low, "dial") ||
				strings.Contains(low, "timeout") || strings.Contains(low, "скачать") {
				host := h
				if !strings.Contains(low, h) {
					host = it.Hosts[0]
				}
				return NoAccess, "нет доступа к " + host + " — без этого " + it.Without, host
			}
		}
		return Failed, l.Voice.Error, ""
	}
	return Missing, "", ""
}

// WriteSkip keeps the person's «Пропустить» (on) or takes it back, in the inbox.
func WriteSkip(dir, id string, on bool) error {
	if _, ok := ByID(id); !ok {
		return os.ErrNotExist
	}
	in := ReadInbox(dir)
	if on {
		in.Skip[id] = true
	} else {
		delete(in.Skip, id)
	}
	ids := make([]string, 0, len(in.Skip))
	for k := range in.Skip {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	b, _ := json.Marshal(ids)
	return writeInbox(dir, SkipFile, b)
}

// WriteProxy keeps the proxy address for the package manager ("" takes it away).
func WriteProxy(dir, raw string) error {
	if raw == "" {
		err := os.Remove(filepath.Join(dir, ProxyFile))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	p, err := CheckProxy(raw)
	if err != nil {
		return err
	}
	return writeInbox(dir, ProxyFile, []byte(p+"\n"))
}

// Ask asks the service for an install now: its path unit watches the request file.
func Ask(dir string, now time.Time) error {
	return writeInbox(dir, RequestFile, []byte(strconv.FormatInt(now.UnixMilli(), 10)+"\n"))
}

func writeInbox(dir, name string, b []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}
