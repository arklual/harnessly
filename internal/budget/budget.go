// Package budget is what a machine does with the limits a hub sends it (docs/hub/limits.md, «On the machine»). The
// agent keeps the last budget.set in a file of the data folder (Save); the runtime next door reads that file (Reader)
// and, per chat, decides what to do with it (Decide): nothing, a notice, or a stop.
package budget

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// FileName is the list's file in the data folder.
const FileName = "budget.json"

// Path is the list's file in a data folder.
func Path(dataDir string) string { return filepath.Join(dataDir, FileName) }

// Load reads the list; a missing file is an empty list with no error.
func Load(path string) (hubproto.BudgetSet, error) {
	var set hubproto.BudgetSet
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return set, nil
	}
	if err != nil {
		return set, err
	}
	err = json.Unmarshal(raw, &set)
	return set, err
}

// Save replaces the list atomically (a temporary file, then rename), readable by its owner only.
func Save(path string, set hubproto.BudgetSet) error {
	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Reader reads the list for the runtime and keeps it until the file changes. A file that is missing or unreadable is
// an empty list: the machine then does nothing, as without a hub.
type Reader struct {
	Path string

	mu   sync.Mutex
	info os.FileInfo // the file the list was read from
	set  hubproto.BudgetSet
}

// Get is the list as of now.
func (r *Reader) Get() hubproto.BudgetSet {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := os.Stat(r.Path)
	if err != nil {
		r.info, r.set = nil, hubproto.BudgetSet{}
		return r.set
	}
	// Save replaces the file by a rename, so a new list is a new file even when its size and time are the same.
	if r.info != nil && os.SameFile(r.info, st) && st.ModTime().Equal(r.info.ModTime()) && st.Size() == r.info.Size() {
		return r.set
	}
	set, err := Load(r.Path)
	if err != nil {
		return r.set // half written or damaged: keep what was read before
	}
	r.info, r.set = st, set
	return set
}

// Subject is who a decision is about. An empty Account or Model is unknown: only entries that do not name one apply.
type Subject struct {
	User    string // the person's email
	Account string // accountRef: claude:<uuid>, codex:<id>, key:sha256:<16 hex>
	Model   string // as the chat was launched or as the CLI names it
}

// Applies says whether an entry is about the subject: the same person, the account the entry names (or any), and a
// model the entry's pattern is a part of (or any). Case does not matter for the person and the model.
func Applies(e hubproto.BudgetEntry, s Subject) bool {
	if !strings.EqualFold(strings.TrimSpace(e.User), strings.TrimSpace(s.User)) {
		return false
	}
	if e.Account != "" && e.Account != s.Account {
		return false
	}
	if e.ModelPattern != "" && !strings.Contains(strings.ToLower(s.Model), strings.ToLower(e.ModelPattern)) {
		return false
	}
	return true
}

// Decision is what to do about one subject.
type Decision struct {
	// Entry is the binding entry; the zero Entry (Status "") when there is nothing to do.
	Entry hubproto.BudgetEntry
	// Block: the chat must not start or go on (a stop in stop mode).
	Block bool
}

// None says there is nothing to do.
func (d Decision) None() bool { return d.Entry.Status == "" }

// Key names the entry for «once per entry version»: the same rule in the same status and period is one notice, however
// its percentage moves.
func (d Decision) Key() string {
	e := d.Entry
	return e.RuleID + "|" + e.Status + "|" + time.UnixMilli(e.ResetsAt).UTC().Format(time.RFC3339)
}

// Decide picks the entry that binds the subject: a stop over a warn, a forbidden model first, then the highest share.
// Mode off, an unknown mode and an empty list decide nothing.
func Decide(set hubproto.BudgetSet, s Subject) Decision {
	if set.Mode != hubproto.EnforceWarn && set.Mode != hubproto.EnforceStop {
		return Decision{}
	}
	var best hubproto.BudgetEntry
	for _, e := range set.Entries {
		if (e.Status != hubproto.BudgetWarn && e.Status != hubproto.BudgetStop) || !Applies(e, s) {
			continue
		}
		if best.Status == "" || heavier(e, best) {
			best = e
		}
	}
	return Decision{Entry: best, Block: best.Status == hubproto.BudgetStop && set.Mode == hubproto.EnforceStop}
}

func heavier(a, b hubproto.BudgetEntry) bool {
	if (a.Status == hubproto.BudgetStop) != (b.Status == hubproto.BudgetStop) {
		return a.Status == hubproto.BudgetStop
	}
	if a.Forbid != b.Forbid {
		return a.Forbid
	}
	return a.Pct > b.Pct
}

// OnlyUser is the one person the list names, "" when it names none or several. A chat that does not know its person
// belongs to it: a machine that one person uses is the usual case.
func OnlyUser(set hubproto.BudgetSet) string {
	user := ""
	for _, e := range set.Entries {
		u := strings.ToLower(strings.TrimSpace(e.User))
		if user != "" && u != user {
			return ""
		}
		user = u
	}
	return user
}
