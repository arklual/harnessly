package agent

import (
	"encoding/json"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The load of the machine for the hub: every minute one `metrics.sample` {cpuPct, ramPct, diskPct, chatsNow, at}. It goes
// only to a link that named the event in link.subscribe, so a hub that does not know it is never sent it, and it is
// offered in `hello` as the "metrics" feature. The sampler reads /proc (or sysctl and vm_stat) and one statfs; the chats
// at work come from the runtime next door and are left out when it does not answer.

// sampler makes the samples. Its clock, reader and chat count are injected for tests.
type sampler struct {
	sys   sysReader
	path  string
	chats func() *int
	now   func() time.Time
}

// warm takes the CPU baseline: a percentage needs two readings.
func (s *sampler) warm() { _, _ = s.sys.CPUPct() }

// sample reads the machine once. False when nothing at all could be read (the system is not one the agent knows).
func (s *sampler) sample() (hubproto.MetricsSample, bool) {
	out := hubproto.MetricsSample{At: s.now().UnixMilli()}
	ok := false
	if v, err := s.sys.CPUPct(); err == nil {
		out.CPUPct, ok = round1(v), true
	}
	if total, avail, err := s.sys.Memory(); err == nil && total > 0 {
		out.RAMPct, ok = round1(pct(float64(total-avail), float64(total))), true
	}
	if total, used, err := s.sys.Disk(s.path); err == nil && total > 0 {
		out.DiskPct, ok = round1(pct(float64(used), float64(total))), true
	}
	if s.chats != nil {
		out.ChatsNow = s.chats()
	}
	return out, ok
}

func round1(v float64) *float64 {
	r := float64(int64(v*10+0.5)) / 10
	return &r
}

// hardware is what hello says of the machine; nil when nothing is known.
func (s *sampler) hardware(goos, goarch string) *hubproto.Hardware {
	f := s.sys.Hardware(s.path)
	hw := &hubproto.Hardware{Cores: f.Cores, RAMBytes: f.RAMBytes, DiskBytes: f.DiskBytes, Model: f.Model, Laptop: f.Laptop,
		OS: goos, Arch: goarch}
	if hw.Cores == 0 && hw.RAMBytes == 0 && hw.DiskBytes == 0 && hw.Model == "" {
		return nil
	}
	return hw
}

// subscribed says the link named the event: unlike wants, a link without a list does not count.
func (l *link) subscribed(event string) bool {
	set := l.events.Load()
	if set == nil {
		return false
	}
	_, ok := (*set)[event]
	return ok
}

// emitSubscribed sends an event to the hub links that asked for it by name.
func (h *hub) emitSubscribed(event, key string, value any) bool {
	frame, err := marshal(map[string]any{"event": event, key: value})
	if err != nil {
		Log("error", "event not encoded", F{"event", event}, F{"error", err.Error()})
		return false
	}
	sent := false
	for _, l := range h.hubLinks() {
		if l.subscribed(event) {
			l.sendEvent(event, frame)
			sent = true
		}
	}
	return sent
}

// startMetrics runs the sampler every minute while a link that asked for it is up.
func (h *hub) startMetrics() {
	if h.metrics == nil {
		return
	}
	h.metrics.warm()
	h.goBG(func() {
		every(h.ctx, hubproto.MetricsEvery*time.Second, false, func() {
			wanted := false
			for _, l := range h.hubLinks() {
				wanted = wanted || l.subscribed(hubproto.EventMetricsSample)
			}
			if !wanted {
				// Keep the baseline fresh so the first sample after a subscribe does not span an hour.
				h.metrics.warm()
				return
			}
			if s, ok := h.metrics.sample(); ok {
				h.emitSubscribed(hubproto.EventMetricsSample, "sample", s)
			}
		})
	})
}

// chatsNow asks the runtime how many chats have an agent at work; nil when it does not say.
func (h *hub) chatsNow() *int {
	if h.a.cfg.RuntimePort == 0 || h.observe {
		return nil
	}
	raw, err := h.a.callRuntime(h.a.cfg.ConsoleAPI()+"/load", map[string]any{})
	if err != nil {
		return nil
	}
	var r struct {
		ChatsNow *int `json:"chatsNow"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	return r.ChatsNow
}
