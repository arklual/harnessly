package machines

import (
	"context"
	"math"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The load of the machines (docs/hub/machines-resources.md, «Load»): an agent that offers it sends one reading a minute
// to a hub that subscribed; the newest reading of each machine is kept in memory for the list, and every reading is
// folded into a five-minute bucket that is kept for a week.

const (
	// sampleMinGap is the least time between two readings of a machine that are both taken; an agent that talks faster is
	// not believed.
	sampleMinGap = 10 * time.Second
	// liveFor is how long a reading counts as the machine's load now.
	liveFor = 3 * time.Minute
)

type liveSample struct {
	cpu, ram, disk *float64
	chats          *int
	at             time.Time
}

// LoadView is the machine's load now: percentages (null when the machine could not read one) and when it was read.
type LoadView struct {
	CPUPct  *float64 `json:"cpuPct"`
	RAMPct  *float64 `json:"ramPct"`
	DiskPct *float64 `json:"diskPct"`
	At      string   `json:"at"`
}

// pct keeps a percentage in 0..100, and drops what is no number.
func pct(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	c := math.Min(100, math.Max(0, *v))
	return &c
}

// onSample records one reading of a machine's load.
func (s *Service) onSample(ctx context.Context, m store.Machine, e hubproto.MetricsSample) {
	if !s.onCached(ctx, m.SpaceID) {
		return
	}
	now := s.now()
	ls := liveSample{cpu: pct(e.CPUPct), ram: pct(e.RAMPct), disk: pct(e.DiskPct), chats: e.ChatsNow, at: now}
	if ls.chats != nil && *ls.chats < 0 {
		ls.chats = nil
	}
	s.res.mu.Lock()
	prev, had := s.res.latest[m.ID]
	if had && now.Sub(prev.at) < sampleMinGap {
		s.res.mu.Unlock()
		return
	}
	s.res.latest[m.ID] = ls
	s.res.mu.Unlock()
	// A bucket averages the three together, so a reading that lacks one of them only updates the live view.
	if ls.cpu == nil || ls.ram == nil || ls.disk == nil {
		return
	}
	chats := 0
	if ls.chats != nil {
		chats = *ls.chats
	}
	if err := s.db.S().AddMetricSample(ctx, m.ID, now.UnixMilli(), *ls.cpu, *ls.ram, *ls.disk, chats); err != nil {
		s.log.Warn("load sample not saved", "machine", m.ID, "error", err)
	}
}

// load is the newest reading of a connected machine, or nil when there is none fresh.
func (s *Service) load(machineID string, now time.Time) *LoadView {
	s.res.mu.Lock()
	ls, ok := s.res.latest[machineID]
	s.res.mu.Unlock()
	if !ok || now.Sub(ls.at) > liveFor || s.reg.Get(machineID) == nil {
		return nil
	}
	return &LoadView{CPUPct: roundPtr(ls.cpu), RAMPct: roundPtr(ls.ram), DiskPct: roundPtr(ls.disk), At: ls.at.UTC().Format(time.RFC3339)}
}

func roundPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := round1(*v)
	return &r
}
