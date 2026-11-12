package agent

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The subscription windows of the machine (Claude's and Codex's five-hour and seven-day ones) go to the hub once a
// minute, for the screens that tell how much of the week a person has used (hub/machines-limits.md). The numbers are
// the runtime's own answer to /machines; the hub keeps only the last value per window.

// limitsFeed starts the sampler while a link that asked for the windows is up.
func (h *hub) startLimits() {
	if !h.limitsServed() {
		return
	}
	h.goBG(func() {
		every(h.ctx, hubproto.LimitsEvery*time.Second, false, func() {
			wanted := false
			for _, l := range h.hubLinks() {
				wanted = wanted || l.subscribed(hubproto.EventLimitsWindows)
			}
			if !wanted {
				return
			}
			if w, ok := h.a.providerWindows(); ok {
				h.emitSubscribed(hubproto.EventLimitsWindows, "windows", w)
			}
		})
	})
}

// limitsServed says whether this agent can read the windows: it has a runtime next door that is its own to ask.
func (h *hub) limitsServed() bool { return h.a.cfg.RuntimePort != 0 && !h.observe }

type rawWindow struct {
	UsedPct  *float64 `json:"usedPct"`
	ResetsAt *float64 `json:"resetsAt"`
}

type rawProvider struct {
	FiveHour *rawWindow `json:"fiveHour"`
	SevenDay *rawWindow `json:"sevenDay"`
}

// providerWindows asks the runtime for its machine and takes the windows of each provider that has numbers.
func (a *Agent) providerWindows() (hubproto.LimitsWindows, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), machineCallTO)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.runtimeURL(a.cfg.RuntimeAPI+"/machines"), nil)
	if err != nil {
		return hubproto.LimitsWindows{}, false
	}
	req.Header.Set("cookie", a.runtimeCookie())
	res, err := a.client.Do(req)
	if err != nil {
		return hubproto.LimitsWindows{}, false
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil || res.StatusCode < 200 || res.StatusCode > 299 {
		return hubproto.LimitsWindows{}, false
	}
	return parseProviderWindows(raw, time.Now())
}

// parseProviderWindows reads the runtime's /machines answer. The runtime answers with its own machine; if it ever lists
// more, the fullest window of each provider wins, which is what a limit is about.
func parseProviderWindows(raw []byte, now time.Time) (hubproto.LimitsWindows, bool) {
	var ans struct {
		Machines []struct {
			Limits map[string]json.RawMessage `json:"limits"`
		} `json:"machines"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return hubproto.LimitsWindows{}, false
	}
	out := hubproto.LimitsWindows{Providers: map[string]hubproto.ProviderWindows{}, At: now.UnixMilli()}
	for _, m := range ans.Machines {
		for _, provider := range []string{"claude", "codex"} {
			var p rawProvider
			if json.Unmarshal(m.Limits[provider], &p) != nil {
				continue
			}
			cur := out.Providers[provider]
			cur.FiveHour = fuller(cur.FiveHour, window(p.FiveHour))
			cur.SevenDay = fuller(cur.SevenDay, window(p.SevenDay))
			if cur.FiveHour != nil || cur.SevenDay != nil {
				out.Providers[provider] = cur
			}
		}
	}
	return out, len(out.Providers) > 0
}

func window(w *rawWindow) *hubproto.LimitWindow {
	if w == nil || w.UsedPct == nil || math.IsNaN(*w.UsedPct) || math.IsInf(*w.UsedPct, 0) {
		return nil
	}
	out := &hubproto.LimitWindow{UsedPct: math.Max(0, math.Min(100, *w.UsedPct))}
	if w.ResetsAt != nil && !math.IsNaN(*w.ResetsAt) && !math.IsInf(*w.ResetsAt, 0) && *w.ResetsAt > 0 {
		out.ResetsAt = int64(*w.ResetsAt)
	}
	return out
}

func fuller(a, b *hubproto.LimitWindow) *hubproto.LimitWindow {
	if a == nil || (b != nil && b.UsedPct > a.UsedPct) {
		return b
	}
	return a
}
