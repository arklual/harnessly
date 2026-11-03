package machines

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// What the list of machines shows beyond the basics: what the machine runs on, its hardware, its load now, its week and
// the operation that is running on it. Only for the machines of an organization that has the resources feature on; for
// the others these fields stay empty and the list is what it was.

// weekDays is the span of a machine's «за неделю».
const weekDays = 7

// CloudView is what a machine runs on, as its provider says: null for a machine the hub knows no server of.
type CloudView struct {
	Provider string     `json:"provider"`
	Plan     *string    `json:"plan"`
	CPU      *int       `json:"cpu"`
	RAMGB    *int       `json:"ramGB"`
	DiskGB   *int       `json:"diskGB"`
	Price    *priceView `json:"price"`
	// ExternalID is the provider's id of the server, null when the hub addresses it by its own id.
	ExternalID *string `json:"externalId"`
}

// WeekView is what a machine's calls of the last seven days amount to. Spend is the list cost of the priced calls
// (null when none was priced); Unpriced counts the calls that had no price.
type WeekView struct {
	Chats    int          `json:"chats"`
	Calls    int          `json:"calls"`
	Unpriced int          `json:"unpriced"`
	Spend    *rates.Money `json:"spend"`
	// SpendMicros is Spend in micro-dollars, for the list to convert into the organization's currency.
	SpendMicros int64 `json:"-"`
}

// enrich fills the resources side of the machines' rows (hs[i] is the row of ms[i]).
func (s *Service) enrich(ctx context.Context, st store.Store, ms []store.Machine, hs []HubMachine, now time.Time) error {
	if s.gate == nil {
		return nil
	}
	spaceOn := map[string]bool{}
	var ids []string
	for _, m := range ms {
		on, seen := spaceOn[m.SpaceID]
		if !seen {
			on = s.on(ctx, m.SpaceID)
			spaceOn[m.SpaceID] = on
		}
		if on {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	res, err := st.ResourcesByMachines(ctx, ids)
	if err != nil {
		return err
	}
	hw, err := st.HWByMachines(ctx, ids)
	if err != nil {
		return err
	}
	managed, err := st.ManagedByMachines(ctx, ids)
	if err != nil {
		return err
	}
	ops, err := st.ActiveOpsByMachines(ctx, ids)
	if err != nil {
		return err
	}
	weeks, err := st.MachineWeeks(ctx, ids, now.AddDate(0, 0, -weekDays).UnixMilli(), now.UnixMilli()+1)
	if err != nil {
		return err
	}
	opsBy := map[string][]store.MachineOp{}
	for _, o := range ops {
		opsBy[o.MachineID] = append(opsBy[o.MachineID], o)
	}
	for i, m := range ms {
		if !spaceOn[m.SpaceID] {
			continue
		}
		h := &hs[i]
		mv := managedView(managed[m.ID])
		h.Managed = &mv
		if r, ok := res[m.ID]; ok && r.Provider != "" {
			h.Cloud = cloudOf(r)
		}
		if v, ok := hw[m.ID]; ok {
			h.Hardware = hwOf(v)
		}
		h.Load = s.load(m.ID, now)
		if w, ok := weeks[m.ID]; ok {
			h.Week = &WeekView{Chats: w.Chats, Calls: w.Calls, Unpriced: w.Unpriced, SpendMicros: w.ListCostMicros}
		}
		h.ActiveOps = s.opViews(opsBy[m.ID])
	}
	return nil
}

func cloudOf(r store.MachineResources) *CloudView {
	c := &CloudView{Provider: r.Provider, CPU: r.CPU, RAMGB: r.RAMGB, DiskGB: r.DiskGB}
	var plan struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(r.Plan), &plan) == nil && plan.Name != "" {
		c.Plan = &plan.Name
	}
	if r.PriceAmount != nil && r.PriceCurrency != "" {
		c.Price = &priceView{Amount: *r.PriceAmount, Currency: r.PriceCurrency, Period: r.PricePeriod}
	}
	if r.ExternalID != "" {
		id := r.ExternalID
		c.ExternalID = &id
	}
	return c
}
