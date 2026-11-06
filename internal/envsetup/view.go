package envsetup

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The page's look at the setup (the «Настройка машины» card of the start page): what the list asks of the machine, how
// each item stands, the run in progress or the last one. It is read from the link folder only; nothing here runs.

// ViewItem is one setup item as the card shows it.
type ViewItem struct {
	ItemID  string `json:"itemId"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, error, pending, running; pending also before the first run
	Message string `json:"message,omitempty"`
	// Secret is the safe's name a pending item waits for, when it waits for one.
	Secret string `json:"secret,omitempty"`
}

// View is the card's data.
type View struct {
	// Allowed: the policy lets the list set this machine up. Off, the card says nothing.
	Allowed bool       `json:"allowed"`
	Hash    string     `json:"hash"`
	Items   []ViewItem `json:"items"`
	// Done: the list's current version was worked on and every item is ok.
	Done bool `json:"done"`
	// Run is the run in progress, else the latest one; nil before any.
	Run *hubproto.EnvRun `json:"run,omitempty"`
}

// View reads the folder.
func (r *Runner) View() View {
	v := View{Allowed: r.allowed(), Items: []ViewItem{}}
	h, ok := r.readHeld()
	if !ok {
		return v
	}
	v.Hash = h.Hash
	st := r.readState()
	byID := map[string]hubproto.EnvApplied{}
	if st.Hash == h.Hash {
		for _, a := range st.Items {
			byID[a.ItemID] = a
		}
	}
	runs := r.runs()
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt > runs[j].StartedAt })
	var live *hubproto.EnvRun
	for i := range runs {
		if runs[i].Mode == hubproto.RunSandbox || runs[i].Mode == hubproto.RunCheck {
			continue
		}
		if v.Run == nil {
			v.Run = &runs[i]
		}
		if runs[i].Status == hubproto.EnvRunning {
			live = &runs[i]
			v.Run = live
			break
		}
	}
	steps := map[string]hubproto.EnvStep{}
	if live != nil && live.Hash == h.Hash {
		for _, s := range live.Steps {
			steps[s.ItemID] = s
		}
	}
	all := true
	for _, it := range setupItems(h.Items) {
		vi := ViewItem{ItemID: it.ItemID, Kind: it.Kind, Name: it.Name, Status: hubproto.EnvPending}
		if a, ok := byID[it.ItemID]; ok {
			vi.Status, vi.Message = a.Status, a.Message
		}
		if s, ok := steps[it.ItemID]; ok && s.Status == hubproto.EnvRunning {
			vi.Status, vi.Message = hubproto.EnvRunning, s.Message
		}
		if vi.Status == hubproto.EnvPending {
			vi.Secret = waitedSecret(it, r.Secrets)
		}
		if vi.Status != hubproto.EnvOK {
			all = false
		}
		v.Items = append(v.Items, vi)
	}
	v.Done = all && st.Hash == h.Hash && live == nil
	return v
}

// waitedSecret is the safe's name an item cannot go on without, when the safe lacks it.
func waitedSecret(it hubproto.EnvItem, sec Secrets) string {
	if sec == nil {
		return ""
	}
	var src source
	if len(it.Source) == 0 || json.Unmarshal(it.Source, &src) != nil {
		return ""
	}
	var names []string
	switch it.Kind {
	case hubproto.EnvKindAccess:
		n := src.Secret
		if n == "" {
			n = hubproto.AccessSecret(src.Service)
		}
		names = []string{n}
	case hubproto.EnvKindScript:
		names = src.Secrets
	}
	for _, n := range names {
		if !sec.Has(n) {
			return n
		}
	}
	return ""
}

// ErrSetupOff is Ask's answer when the policy does not let the list set the machine up.
var ErrSetupOff = errors.New("setup is off on this machine")

// Ask puts a run of the held list in the queue, the way a request of the hub is put: the person pressed «Повторить» on
// the card. It is an apply run (the hub keeps an apply run it did not ask for), so what is done stays done and what is
// not is done again now. A run already waiting is answered with its id.
func (r *Runner) Ask(by string) (string, error) {
	if !r.allowed() {
		return "", ErrSetupOff
	}
	h, ok := r.readHeld()
	if !ok {
		return "", errors.New("no list on this machine yet")
	}
	for _, f := range r.requestFiles() {
		if req, ok := readRequest(f.path); ok && req.Mode == hubproto.RunApply {
			return req.RunID, nil
		}
	}
	now := r.now().UnixMilli()
	id := fmt.Sprintf("apply-%s-%d", short(h.Hash), now)
	req := hubproto.EnvRunRequest{RunID: id, Mode: hubproto.RunApply, By: by, At: now}
	if err := writeJSON(filepath.Join(r.LinkDir, hubproto.EnvRequestsDir, id+".json"), req); err != nil {
		return "", err
	}
	return id, nil
}
