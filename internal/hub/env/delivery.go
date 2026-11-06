package env

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Delivery is how far the screen's environment got: Machines are the working machines of the screen's tenants (and
// team), Reported those that said what they hold, Current those that hold the list the hub has for them now. LastAt is
// the latest report.
type Delivery struct {
	Machines int     `json:"machines"`
	Reported int     `json:"reported"`
	Current  int     `json:"current"`
	LastAt   *string `json:"lastAt"`
}

// ItemDelivery is what the machines said about one item: how many put it in, how many keep it without being able to
// load it yet, how many failed it, and the first reason one of them gave.
type ItemDelivery struct {
	OK      int     `json:"ok"`
	Pending int     `json:"pending"`
	Error   int     `json:"error"`
	Message *string `json:"message"`
}

// maxDeliveryMachines: past this many working machines the screen's delivery is not computed (null): it is read from every
// machine's state, and the screen shows no per-machine statuses anyway.
var maxDeliveryMachines = 200

// delivery reads what the machines of the screen hold. Only the reports of machines whose list is the current one are
// counted per item: an older list says nothing about the items the screen shows now. Nil when the screen reaches more
// than maxDeliveryMachines machines.
func (q *req) delivery(tenants []store.Space, team string) (*Delivery, map[string]*ItemDelivery, error) {
	total := 0
	for _, sp := range tenants {
		n, err := q.st.CountWorkingMachines(q.ctx, sp.ID)
		if err != nil {
			return nil, nil, err
		}
		total += n
	}
	if total > maxDeliveryMachines {
		return nil, nil, nil
	}
	d := &Delivery{}
	items := map[string]*ItemDelivery{}
	var last int64
	for _, sp := range tenants {
		ms, err := q.st.MachinesBySpace(q.ctx, sp.ID)
		if err != nil {
			return nil, nil, err
		}
		states, err := q.st.EnvStatesBySpace(q.ctx, sp.ID) // one query for the tenant, not one per machine
		if err != nil {
			return nil, nil, err
		}
		memo := map[string]wantList{}
		for _, m := range ms {
			if m.RevokedAt != nil || m.DisabledAt != nil || (team != "" && m.TeamID != team) {
				continue
			}
			d.Machines++
			want := q.s.machineListMemo(q.w, sp.ID, m, memo)
			st, found := states[m.ID]
			if !found || st.At.IsZero() {
				// The hub sends no first empty list, so a machine that should hold nothing and never got anything holds
				// what it should.
				if want.empty() {
					d.Current++
				}
				continue
			}
			d.Reported++
			if ms := st.At.UnixMilli(); ms > last {
				last = ms
			}
			if !want.holds(st.Hash) {
				continue
			}
			d.Current++
			var applied []hubproto.EnvApplied
			if err := json.Unmarshal([]byte(st.Applied), &applied); err != nil {
				continue
			}
			for _, a := range applied {
				it := items[a.ItemID]
				if it == nil {
					it = &ItemDelivery{}
					items[a.ItemID] = it
				}
				switch a.Status {
				case hubproto.EnvOK:
					it.OK++
				case hubproto.EnvPending:
					it.Pending++
				default:
					it.Error++
				}
				if a.Status != hubproto.EnvOK && a.Message != "" && it.Message == nil {
					it.Message = strp(a.Message)
				}
			}
		}
	}
	d.LastAt = msStr(last)
	return d, items, nil
}

// wantList is the list the hub computes for a machine. A machine that has not said hello to this hub process yet has
// not told whether it has the feature environment.setup, so both lists (with and without the setup kinds) count as its
// own: what it reported last is one of them.
type wantList struct {
	items  []hubproto.EnvItem
	hashes []string
}

func (w wantList) empty() bool { return len(w.items) == 0 }

// holds says whether a reported hash is the machine's current list.
func (w wantList) holds(hash string) bool { return slices.Contains(w.hashes, hash) }

// machineList is the list a machine of a tenant is to hold now.
func (s *Service) machineList(w *world, spaceID string, m store.Machine) wantList {
	return s.machineListMemo(w, spaceID, m, nil)
}

// machineListMemo is machineList that computes the list once per (team, features) in memo: the machines of a team hold
// the same list.
func (s *Service) machineListMemo(w *world, spaceID string, m store.Machine, memo map[string]wantList) wantList {
	setup, known, claude := false, true, false
	if s.push != nil {
		setup, known = s.push.SetupOf(m.ID)
		claude = s.push.ClaudeOf(m.ID)
	}
	owner := ""
	if len(w.groups) > 0 {
		owner = m.CreatedBy // with groups the list depends on the owner's choices
	}
	key := fmt.Sprintf("%s/%s/%s/%t/%t/%t", spaceID, m.TeamID, owner, setup, known, claude)
	if v, ok := memo[key]; ok {
		return v
	}
	items := MachineItems(w, spaceID, m.TeamID, owner, setup, claude)
	out := wantList{items: items, hashes: []string{hubproto.EnvHash(items)}}
	if !known {
		full := MachineItems(w, spaceID, m.TeamID, owner, true, true)
		out.hashes = append(out.hashes, hubproto.EnvHash(full))
		if len(items) == 0 {
			out.items = full
		}
	}
	if memo != nil {
		memo[key] = out
	}
	return out
}
