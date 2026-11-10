package apps

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// The journal of an app (HRN-849): every action of a high or critical class (Loud), and every change of what the app
// may do — the person's consent, an update waiting for it, the switch. One JSON line per entry in
// <registry>/journal/<id>.jsonl, read by the app's inspector. It says what was done, never a value: no secret, no file's
// text, no message.

// JournalEntry is one line.
type JournalEntry struct {
	At int64 `json:"at"`
	// What is the method of the bridge or the event: consent, consent.waits, declined, off, on, secret.launch, …
	What  string `json:"what"`
	Class string `json:"class"`
	// Detail is a short note: the version, a file's path, a secret's name.
	Detail string `json:"detail,omitempty"`
	// Chat is the conversation it happened in, when there is one.
	Chat string `json:"chat,omitempty"`
	OK   bool   `json:"ok"`
}

const (
	// journalMax is how big a journal grows before its older half is dropped.
	journalMax = 512 << 10
	// journalDetailMax keeps a note short.
	journalDetailMax = 200
)

var journalMu sync.Mutex

func (r *Registry) journalFile(id string) string {
	return filepath.Join(r.Root, "journal", PluginName(id)+".jsonl")
}

// Journal adds a line to an app's journal; a journal that cannot be written loses the line, the action is not stopped
// for it.
func (r *Registry) Journal(id string, e JournalEntry) error {
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	if len(e.Detail) > journalDetailMax {
		e.Detail = e.Detail[:journalDetailMax]
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	journalMu.Lock()
	defer journalMu.Unlock()
	file := r.journalFile(id)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(file); err == nil && info.Size() > journalMax {
		if old, err := os.ReadFile(file); err == nil {
			half := old[len(old)/2:]
			if i := bytes.IndexByte(half, '\n'); i >= 0 {
				half = half[i+1:]
			}
			if err := writeFileAtomic(file, half); err != nil {
				return err
			}
		}
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

// ReadJournal is the last n lines of an app's journal, the newest first; none is an empty list.
func (r *Registry) ReadJournal(id string, n int) ([]JournalEntry, error) {
	out := []JournalEntry{}
	f, err := os.Open(r.journalFile(id))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e JournalEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	slices.Reverse(out)
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// ClassOfMethod is the class of what a bridge method does: its right's class, low for a method that needs none.
func ClassOfMethod(method string) string {
	if right := methodPermission[method]; right != "" {
		if p, _, ok := surfacePermission(right); ok {
			return p.Class
		}
	}
	return ClassLow
}
