package usagefeed

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/usagereport"
)

// maxWait is the longest pause after a turn that still counts as the agent waiting for its person; a longer one is the
// person away, and the session resumed later.
const maxWait = 4 * 3600.0

// metrics adds up a session's numbers for the hub's KPI (analytics.md, «KPI») over its transcripts.
type metrics struct {
	turns, agentMs, waitMs, waits, intervals float64
	toolCalls, toolErrors                    float64
	input, cacheRead, cacheWrite, carry      float64
	firstTs, firstCtx                        float64
	// prs are the merge requests of the session: the pr-link ones and the ones its Bash and MCP calls created or pushed
	// to, by key (hubproto.MRKey); an address that is not a GitLab merge request stays as it is.
	prs map[string]bool
}

// add takes one transcript's facts.
func (m *metrics) add(fa *usagereport.Facts) {
	m.turns += float64(len(fa.Prompts))
	m.intervals += float64(len(fa.Turns))
	for _, t := range fa.Turns {
		m.agentMs += math.Max(0, t.Ms)
	}
	// A wait is from a turn's end to the person's next prompt.
	prompts := make([]float64, 0, len(fa.Prompts))
	for _, p := range fa.Prompts {
		prompts = append(prompts, p.Ts)
	}
	sort.Float64s(prompts)
	for _, t := range fa.Turns {
		i := sort.SearchFloat64s(prompts, t.Ts)
		for i < len(prompts) && prompts[i] <= t.Ts {
			i++
		}
		if i == len(prompts) {
			continue
		}
		if gap := prompts[i] - t.Ts; gap > 0 && gap <= maxWait {
			m.waits++
			m.waitMs += gap * 1000
		}
	}
	// Tool errors leave out the person's refusals: a block is not a failure of the tool.
	errs := -float64(len(fa.Denials))
	for _, t := range fa.Tools {
		errs += t.Error
	}
	m.toolCalls += float64(len(fa.Tools))
	m.toolErrors += math.Max(0, errs)

	prev := 0.0
	for i, r := range fa.Messages {
		write := r.W5m + r.W1h
		ctx := r.Input + r.CacheRead + write
		m.input += r.Input
		m.cacheRead += r.CacheRead
		m.cacheWrite += write
		// Carried context: the part of the previous call's context that this call paid for again, not from the cache.
		if i > 0 {
			m.carry += math.Min(prev, r.Input+write)
		}
		if ctx > 0 && r.Ts > 0 && (m.firstTs == 0 || r.Ts < m.firstTs) {
			m.firstTs, m.firstCtx = r.Ts, ctx
		}
		if ctx > 0 {
			prev = ctx
		}
	}
	keys, other := usagereport.PRKeys(fa.PRs)
	for _, pr := range append(append(keys, other...), fa.MRKeys...) {
		if m.prs == nil {
			m.prs = map[string]bool{}
		}
		m.prs[pr] = true
	}
}

// mrKeys are the session's merge request keys, sorted; what is not a key (a pr-link to another host's pull request) is
// counted in `mrs` but not told to the hub.
func (m *metrics) mrKeys() []string {
	var out []string
	for k := range m.prs {
		if hubproto.IsMRKey(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	if len(out) > maxMRKeys {
		out = out[:maxMRKeys]
	}
	return out
}

// maxMRKeys bounds the keys one session tells the hub.
const maxMRKeys = 200

// values are the hub's metrics keys; the fields the facts do not carry (skills, merged MRs, time to MR) are left out.
func (m *metrics) values() map[string]float64 {
	return map[string]float64{
		"turns": m.turns, "agentMs": math.Round(m.agentMs), "waitMs": math.Round(m.waitMs), "waits": m.waits,
		"activeIntervals": m.intervals, "toolCalls": m.toolCalls, "toolErrors": m.toolErrors,
		"inputTokens": math.Round(m.input), "cacheReadTokens": math.Round(m.cacheRead),
		"cacheWriteTokens": math.Round(m.cacheWrite), "carryTokens": math.Round(m.carry),
		"firstCtx": math.Round(m.firstCtx), "mrs": float64(len(m.prs)),
	}
}

// signature says whether the metrics (and the merge request keys, when there are any) changed since the session was
// last written. A session with no keys has the signature it had before keys existed, so only sessions that have some are
// written once more.
func signature(v map[string]float64, mrKeys []string) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%g;", k, v[k])
	}
	if len(mrKeys) > 0 {
		sum := sha256.Sum256([]byte(strings.Join(mrKeys, "\n")))
		fmt.Fprintf(&b, "mrk=%x;", sum[:8])
	}
	return b.String()
}
