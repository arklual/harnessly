package env

import (
	"encoding/json"

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

// delivery reads what the machines of the screen hold. Only the reports of machines whose list is the current one are
// counted per item: an older list says nothing about the items the screen shows now.
func (q *req) delivery(tenants []store.Space, team string) (Delivery, map[string]*ItemDelivery, error) {
	var d Delivery
	items := map[string]*ItemDelivery{}
	var last int64
	for _, sp := range tenants {
		ms, err := q.st.MachinesBySpace(q.ctx, sp.ID)
		if err != nil {
			return d, nil, err
		}
		for _, m := range ms {
			if m.RevokedAt != nil || m.DisabledAt != nil || (team != "" && m.TeamID != team) {
				continue
			}
			d.Machines++
			want := MachineItems(q.w, sp.ID, m.TeamID)
			st, err := q.st.EnvStateByMachine(q.ctx, m.ID)
			if store.IsNotFound(err) || (err == nil && st.At.IsZero()) {
				// The hub sends no first empty list, so a machine that should hold nothing and never got anything holds
				// what it should.
				if len(want) == 0 {
					d.Current++
				}
				continue
			}
			if err != nil {
				return d, nil, err
			}
			d.Reported++
			if ms := st.At.UnixMilli(); ms > last {
				last = ms
			}
			if st.Hash != hubproto.EnvHash(want) {
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
