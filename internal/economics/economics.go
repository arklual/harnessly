package economics

import (
	"errors"
	"math"
	"sort"
	"time"

	"harnessly/internal/history"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/pricing"
	"harnessly/internal/subagents"
)

func breakdownOf(u *Usage) *jsvalue.Object {
	cacheWrite := u.CacheWrite5m + u.CacheWrite1h
	var reasoning any
	if u.ReasoningSeen {
		reasoning = u.Reasoning
	}
	return jsvalue.NewObject("input", u.Input, "output", u.Output, "cacheRead", u.CacheRead, "cacheWrite", cacheWrite,
		"reasoning", reasoning, "total", u.Input+u.Output+u.CacheRead+cacheWrite)
}

func sumModels(states []*State) *Models {
	out := &Models{}
	for _, st := range states {
		st.Models.Each(func(name string, u *Usage) {
			t := out.get(name)
			t.Input += u.Input
			t.Output += u.Output
			t.CacheRead += u.CacheRead
			t.CacheWrite5m += u.CacheWrite5m
			t.CacheWrite1h += u.CacheWrite1h
			t.Reasoning += u.Reasoning
			t.ReasoningSeen = t.ReasoningSeen || u.ReasoningSeen
		})
	}
	return out
}

func sumBreakdown(models *Models) *jsvalue.Object {
	total := &Usage{}
	models.Each(func(_ string, u *Usage) {
		total.Input += u.Input
		total.Output += u.Output
		total.CacheRead += u.CacheRead
		total.CacheWrite5m += u.CacheWrite5m
		total.CacheWrite1h += u.CacheWrite1h
		total.Reasoning += u.Reasoning
		total.ReasoningSeen = total.ReasoningSeen || u.ReasoningSeen
	})
	return breakdownOf(total)
}

// priceTableCost is the list price of every model's tokens; nil while a model the table does not know has any.
func priceTableCost(provider string, models *Models) (usd *float64, unpriced []string) {
	sum := 0.0
	unpriced = []string{}
	models.Each(func(name string, u *Usage) {
		var cost float64
		var ok bool
		if provider == "claude" {
			cost, ok = pricing.ClaudeCost(name, u.Input, u.Output, u.CacheWrite5m, u.CacheWrite1h, u.CacheRead)
		} else {
			cost, ok = pricing.OpenAICost(name, u.Input+u.CacheRead, u.CacheRead, u.Output)
		}
		if ok {
			sum += cost
		} else {
			unpriced = append(unpriced, name)
		}
	})
	if len(unpriced) > 0 {
		return nil, unpriced
	}
	return &sum, unpriced
}

// Source is the conversation's agent session as its tab holds it; SessionID and JSONLPath keep the stored value
// (Undefined when the tab has none).
type Source struct {
	Provider  string
	SessionID any
	JSONLPath any
}

func emptyResult(src Source, status string, now float64) *jsvalue.Object {
	return jsvalue.NewObject("provider", src.Provider, "sessionId", src.SessionID, "status", status, "tokens", nil, "subagents", nil,
		"models", nil, "apiEquivalentUsd", nil, "unpricedModels", []any{}, "time", nil, "limitShare", nil, "computedAt", now)
}

// Summarize is the economics of a conversation from its transcripts' counters; statuslineCost is the cost the
// statusline last reported for the session (nil for none).
func Summarize(src Source, main *State, subs []*State, statuslineCost any, now float64) *jsvalue.Object {
	all := append([]*State{main}, subs...)
	allModels := sumModels(all)
	usd, unpriced := priceTableCost(src.Provider, allModels)
	var api any
	// Claude Code prices its own session, subagents included, even for models the table does not know yet.
	var claudeCodeCost any = statuslineCost
	if main.ClaudeCodeCostUsd != nil {
		claudeCodeCost = *main.ClaudeCodeCostUsd
	}
	if src.Provider == "claude" && claudeCodeCost != nil {
		api = jsvalue.NewObject("value", claudeCodeCost, "source", "claude-code")
	} else if usd != nil && allModels.Len() > 0 {
		api = jsvalue.NewObject("value", *usd, "source", "price-table")
	}
	var timeView any
	first, last := math.Inf(1), math.Inf(-1)
	anyFirst, anyLast := false, false
	for _, st := range all {
		if st.FirstAt != nil {
			first, anyFirst = math.Min(first, *st.FirstAt), true
		}
		if st.LastAt != nil {
			last, anyLast = math.Max(last, *st.LastAt), true
		}
	}
	if anyFirst && anyLast {
		// Subagents run inside the main agent's turns: their time is already in it.
		timeView = jsvalue.NewObject("firstAt", first, "lastAt", last, "wallMs", last-first, "activeMs", main.ActiveMs, "turns", main.Turns)
	}
	var subView any
	if src.Provider == "claude" {
		subView = jsvalue.NewObject("count", len(subs), "tokens", sumBreakdown(sumModels(subs)))
	}
	type modelRow struct {
		name   string
		tokens *jsvalue.Object
	}
	var rows []modelRow
	allModels.Each(func(name string, u *Usage) { rows = append(rows, modelRow{name, breakdownOf(u)}) })
	total := func(o *jsvalue.Object) float64 { n, _ := o.Number("total"); return n }
	sort.SliceStable(rows, func(i, j int) bool { return total(rows[i].tokens) > total(rows[j].tokens) })
	models := []any{}
	for _, r := range rows {
		models = append(models, jsvalue.NewObject("model", r.name, "tokens", r.tokens))
	}
	return jsvalue.NewObject("provider", src.Provider, "sessionId", src.SessionID, "status", "ready", "tokens", sumBreakdown(allModels),
		"subagents", subView, "models", models, "apiEquivalentUsd", api, "unpricedModels", jsvalue.StringArray(unpriced), "time", timeView,
		"limitShare", nil, "computedAt", now)
}

// errBadPath: a transcript path that is not a string, which the previous runtime failed on (500).
var errBadPath = errors.New("economics: transcript path is not a string")

// Conversation reads a conversation's transcripts (its subagents' too, for Claude) and sums them up. While a large
// transcript is read for the first time the answer is "computing" after wait; the read goes on and a later request
// picks it up.
func Conversation(store *Store, stats *history.Stats, src Source, wait time.Duration, now func() float64) (*jsvalue.Object, error) {
	if !jsvalue.Truthy(src.JSONLPath) {
		return emptyResult(src, "no-transcript", now()), nil
	}
	path, ok := src.JSONLPath.(string)
	if !ok {
		// The previous runtime failed on it while listing Claude's subagents (500); for Codex it found no such file.
		if src.Provider == "claude" {
			return nil, errBadPath
		}
		return emptyResult(src, "no-transcript", now()), nil
	}
	type result struct {
		main *State
		subs []*State
		err  error
	}
	done := make(chan result, 1)
	go func() {
		var files []string
		if src.Provider == "claude" {
			files = subagents.Files(path)
		}
		main, err := store.Refresh(path, src.Provider)
		res := result{main: main, err: err}
		for _, f := range files {
			st, err := store.Refresh(f, "claude")
			if err != nil && res.err == nil {
				res.err = err
			}
			if st != nil {
				res.subs = append(res.subs, st)
			}
		}
		done <- res
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	var res result
	select {
	case res = <-done:
	case <-timer.C:
		return emptyResult(src, "computing", now()), nil
	}
	if res.err != nil {
		return nil, res.err
	}
	if res.main == nil {
		return emptyResult(src, "no-transcript", now()), nil
	}
	var cost any
	if src.Provider == "claude" && jsvalue.Truthy(src.SessionID) {
		if st := stats.Read(jscompat.String(src.SessionID)); st != nil {
			cost, _ = st.Get("cost")
		}
	}
	return Summarize(src, res.main, res.subs, cost, now()), nil
}
