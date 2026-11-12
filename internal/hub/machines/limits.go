package machines

import (
	"context"
	"math"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// providerNames and windowNames are the only keys the hub keeps; an agent's other keys are ignored.
var providerNames = []string{"claude", "codex"}

// onLimits keeps a machine's subscription windows. The hub keeps the last reading of each; the history of the numbers
// stays on the machine.
func (s *Service) onLimits(ctx context.Context, m store.Machine, e hubproto.LimitsWindows) {
	if !s.onCached(ctx, m.SpaceID) {
		return
	}
	now := s.now().UnixMilli()
	for _, provider := range providerNames {
		p, ok := e.Providers[provider]
		if !ok {
			continue
		}
		for win, w := range map[string]*hubproto.LimitWindow{"five_hour": p.FiveHour, "seven_day": p.SevenDay} {
			if w == nil || math.IsNaN(w.UsedPct) || math.IsInf(w.UsedPct, 0) {
				continue
			}
			reset := w.ResetsAt
			if reset < 0 {
				reset = 0
			}
			err := s.db.S().PutProviderWindow(ctx, store.ProviderWindow{
				MachineID: m.ID, Provider: provider, Window: win,
				UsedPct: math.Min(100, math.Max(0, w.UsedPct)), ResetsAt: reset, SampledAt: now,
			})
			if err != nil {
				s.log.Warn("provider window not saved", "machine", m.ID, "provider", provider, "window", win, "error", err)
			}
		}
	}
}
