package main

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// What the page did with its smart notice, for the team's product metrics:
//
//	POST <platform>/ui-events   {events: [{name, at, props}]}
//
// Only the events and properties named below pass, and only as words from their own lists or as numbers: no text of a
// notice, no chat, no title, no path ever leaves the machine. They go to the runtime's telemetry endpoint (a released
// runtime only; a checkout drops them).

const (
	uiEventsMax  = 40
	uiEventsRate = 600
)

// uiEventWords: each event's properties and the words each may be; nil takes a whole number from 0 to a day in ms.
var uiEventWords = map[string]map[string]map[string]bool{
	"harnessly_notice": {
		"action": words("shown", "opened", "answered", "dropped", "closed", "closed_all", "restored", "paged",
			"chat_opened"),
		"kind":  words("code", "permission", "question", "plan", "machine", "failed", "limit", "done"),
		"input": words("mouse", "trackpad", "touch", "keys"),
		"stack": words("1", "2-3", "4+"),
		"place": words("top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"),
		"ms":    nil,
		"count": nil,
	},
	"harnessly_notice_gesture": {
		"input":  words("mouse", "trackpad", "touch", "keys"),
		"ms":     nil,
		"frames": nil,
		"long":   nil,
	},
}

func words(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

// uiEventIn is one event as the page sends it.
type uiEventIn struct {
	Name  string         `json:"name"`
	At    int64          `json:"at"`
	Props map[string]any `json:"props"`
}

const uiNumberMax = 24 * 60 * 60 * 1000

// uiEvent keeps what the lists allow and drops the rest; false when the event itself is not one of them.
func uiEvent(in uiEventIn, now time.Time) (telemetry.Event, bool) {
	allowed, ok := uiEventWords[in.Name]
	if !ok {
		return telemetry.Event{}, false
	}
	props := jsvalue.NewObject()
	for _, key := range sortedKeys(allowed) {
		v, has := in.Props[key]
		if !has {
			continue
		}
		list := allowed[key]
		switch x := v.(type) {
		case string:
			if list != nil && list[x] {
				props.Set(key, x)
			}
		case float64:
			if list == nil && x >= 0 && x <= uiNumberMax {
				props.Set(key, int64(x))
			}
		}
	}
	if in.Name == "harnessly_notice" {
		if !props.Has("action") {
			return telemetry.Event{}, false
		}
	}
	at := now
	// The page's clock, when it is near the machine's: an event is never dated in the future or a day back.
	if t := time.UnixMilli(in.At); in.At > 0 && t.Before(now.Add(time.Minute)) && now.Sub(t) < 24*time.Hour {
		at = t
	}
	return telemetry.Event{Name: in.Name, At: at, Properties: props}, true
}

func sortedKeys(m map[string]map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func uiEventRoutes(d deps, queue func() *telemetry.Queue) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var mu sync.Mutex
	window, count := time.Time{}, 0
	take := func(n int, now time.Time) int {
		mu.Lock()
		defer mu.Unlock()
		if now.Sub(window) >= time.Minute {
			window, count = now, 0
		}
		if left := uiEventsRate - count; n > left {
			n = left
		}
		count += n
		return n
	}

	report := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Events []uiEventIn `json:"events"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		q := queue()
		if q == nil || !q.Enabled() {
			secretJSON(w, http.StatusOK, map[string]any{"sent": 0})
			return
		}
		events := b.Events
		if len(events) > uiEventsMax {
			events = events[:uiEventsMax]
		}
		now := time.Now()
		sent := 0
		for _, in := range events[:take(len(events), now)] {
			if e, ok := uiEvent(in, now); ok {
				q.Push(e)
				sent++
			}
		}
		secretJSON(w, http.StatusOK, map[string]any{"sent": sent})
	}

	return []gate.Route{
		{Match: is(platformAPI + "/ui-events"), Handler: http.HandlerFunc(report)},
		{Match: is("/agents" + platformAPI + "/ui-events"), Handler: http.HandlerFunc(report)},
	}
}
