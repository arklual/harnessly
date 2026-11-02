package agent

import (
	"encoding/json"
	"sync"

	"github.com/ly-solutions/harnessly/internal/budget"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The hub's limits (docs/hub/limits.md): budget.set brings the whole list, the agent keeps it in a file of the data
// folder, and the runtime next door reads the file for every chat (internal/budget). The file survives a restart and
// a lost link; only a newer version replaces it.

// budgetMaxEntries bounds a list: it holds the people of one machine who are near a limit.
const budgetMaxEntries = 10000

// budgetKeeper is the list the agent holds.
type budgetKeeper struct {
	path string

	mu      sync.Mutex
	version int64
}

func newBudgetKeeper(dataDir string) *budgetKeeper {
	k := &budgetKeeper{path: budget.Path(dataDir)}
	if set, err := budget.Load(k.path); err == nil {
		k.version = set.Version
	} else {
		Log("warn", "the limits file is unreadable and is ignored until the hub sends a new list", F{"error", err.Error()})
	}
	return k
}

// Version is the version held, 0 for none.
func (k *budgetKeeper) Version() int64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.version
}

// Set keeps set if it is newer than the one held; it answers the version held. The list is on disk before it counts.
func (k *budgetKeeper) Set(set hubproto.BudgetSet) (held int64, applied bool, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if set.Version <= k.version {
		return k.version, false, nil
	}
	if err := budget.Save(k.path, set); err != nil {
		return k.version, false, err
	}
	k.version = set.Version
	return k.version, true, nil
}

func (h *hub) opBudget(req map[string]json.RawMessage) (any, func(), error) {
	version, _ := field[int64](req, "version")
	mode, _ := field[string](req, "mode")
	issuedAt, _ := field[int64](req, "issuedAt")
	entries, _ := field[[]hubproto.BudgetEntry](req, "entries")
	switch {
	case version <= 0:
		return nil, nil, invalid("version must be a positive number")
	case mode != hubproto.EnforceOff && mode != hubproto.EnforceWarn && mode != hubproto.EnforceStop:
		return nil, nil, invalid("mode must be off, warn or stop")
	case len(entries) > budgetMaxEntries:
		return nil, nil, invalid("too many entries")
	}
	held, applied, err := h.budget.Set(hubproto.BudgetSet{Version: version, Mode: mode, IssuedAt: issuedAt, Entries: entries})
	if err != nil {
		return nil, nil, hubproto.Errf(hubproto.CodeFailed, "the limits were not saved: %v", err)
	}
	if applied {
		h.audit.Record(hubproto.OpBudgetSet, true, map[string]any{"version": version, "mode": mode, "entries": len(entries)})
	}
	return hubproto.BudgetSetResult{Version: held}, nil, nil
}
