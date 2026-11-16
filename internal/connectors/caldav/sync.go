package caldav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// The source. The runtime asks every minute; the server is asked every syncEvery (a calendar's ctag and sync token
// first, the changed objects only when they moved), the whole window every fullEvery or when the token expired. The
// meetings of the window are kept in the cursor, so the minute-by-minute events (starting, ended, the day) need no
// request.

var (
	clock     = time.Now
	syncEvery = 5 * time.Minute
	icsEvery  = 15 * time.Minute
	fullEvery = 6 * time.Hour
	// back and ahead bound the window of kept meetings.
	back  = 24 * time.Hour
	ahead = 15 * 24 * time.Hour
	// freshAhead: a meeting new to the cursor without CREATED is told as created only this close.
	freshAhead = 7 * 24 * time.Hour
	endedFor   = 30 * time.Minute
)

// kept is an occurrence as the cursor keeps it: short keys, unix seconds.
type kept struct {
	K   string `json:"k"`
	O   string `json:"o"`
	U   string `json:"u,omitempty"`
	C   string `json:"c,omitempty"`
	S   int64  `json:"s"`
	E   int64  `json:"e"`
	A   bool   `json:"a,omitempty"`
	R   bool   `json:"r,omitempty"`
	T   string `json:"t,omitempty"`
	L   string `json:"l,omitempty"`
	V   string `json:"v,omitempty"` // call link
	St  string `json:"st,omitempty"`
	Me  string `json:"me,omitempty"`
	F   bool   `json:"f,omitempty"`
	Org string `json:"g,omitempty"`
	N   int    `json:"n,omitempty"` // attendees
	P   string `json:"p,omitempty"` // their names, no addresses
}

type calState struct {
	H string `json:"h"`
	N string `json:"n,omitempty"`
	C string `json:"c,omitempty"`
	T string `json:"t,omitempty"`
}

type objState struct {
	E   string `json:"e"`
	Cal string `json:"c,omitempty"`
}

type cursor struct {
	V      int                 `json:"v"`
	Synced int64               `json:"synced"`
	Full   int64               `json:"full"`
	Zone   string              `json:"zone,omitempty"`
	Cals   []calState          `json:"cals,omitempty"`
	ETag   string              `json:"etag,omitempty"`
	LastM  string              `json:"lastModified,omitempty"`
	Objs   map[string]objState `json:"objs"`
	Occ    []kept              `json:"occ"`
	// Fired are the time events already given ("starting:<key>:<start>"), kept a day: no second "starting" after a
	// restart.
	Fired map[string]int64 `json:"fired,omitempty"`
	Day   string           `json:"day,omitempty"`
}

func readCursor(c connectors.Cursor) (cursor, bool) {
	var cur cursor
	if c == "" || json.Unmarshal([]byte(c), &cur) != nil || cur.V != 1 {
		return cursor{V: 1, Objs: map[string]objState{}, Fired: map[string]int64{}}, false
	}
	if cur.Objs == nil {
		cur.Objs = map[string]objState{}
	}
	if cur.Fired == nil {
		cur.Fired = map[string]int64{}
	}
	return cur, true
}

func (c cursor) encode() connectors.Cursor {
	raw, _ := json.Marshal(c)
	return connectors.Cursor(raw)
}

func keep(o occurrence) kept {
	var names []string
	for _, a := range o.Attendees {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return kept{K: o.Key, O: o.Object, U: o.UID, C: o.Calendar, S: o.Start.Unix(), E: o.End.Unix(), A: o.AllDay, R: o.Recurring,
		T: clip(o.Title, 200), L: clip(o.Location, 200), V: clip(o.Call, 500), St: o.Status, Me: o.Me, F: o.Free,
		Org: clip(o.Organizer.Name, 120), N: len(o.Attendees), P: clip(strings.Join(names, ", "), 200)}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (k kept) cancelled() bool { return k.St == "CANCELLED" }

func (k kept) start(loc *time.Location) time.Time { return time.Unix(k.S, 0).In(loc) }
func (k kept) end(loc *time.Location) time.Time   { return time.Unix(k.E, 0).In(loc) }

func (calConn) Interval(connectors.Instance) time.Duration { return time.Minute }

// Poll keeps the window of meetings in step with the server when it is time, then gives the events of this minute.
func (calConn) Poll(ctx context.Context, in connectors.Instance, from connectors.Cursor) ([]connectors.Event, connectors.Cursor, error) {
	cur, learned := readCursor(from)
	now := clock()
	every := syncEvery
	if readOnly(in) {
		every = icsEvery
	}
	var events []connectors.Event
	a := &account{}
	settings(in, a)
	if cur.Zone != "" && !a.userZone {
		a.loc = zone(cur.Zone, a.loc)
	}
	if !learned || now.Sub(time.Unix(cur.Synced, 0)) >= every {
		next, changed, err := refresh(ctx, in, cur, learned, now)
		if err != nil {
			return nil, from, err
		}
		cur = next
		events = append(events, changed...)
		if cur.Zone != "" && !a.userZone {
			a.loc = zone(cur.Zone, a.loc)
		}
	}
	if !learned {
		// The first run only learns: what is in the calendar already is not news, nor is this morning's schedule.
		local := now.In(a.loc)
		if a.morning > 0 && local.Hour() >= a.morning {
			cur.Day = local.Format(time.DateOnly)
		}
		return nil, cur.encode(), nil
	}
	events = append(events, timed(&cur, a, now)...)
	return events, cur.encode(), nil
}

// refresh asks the server what changed and gives the calendar.changed events of it.
func refresh(ctx context.Context, in connectors.Instance, cur cursor, learned bool, now time.Time) (cursor, []connectors.Event, error) {
	a, _, err := login(ctx, in)
	if err != nil {
		return cur, nil, err
	}
	if cur.Zone == "" || a.userZone {
		cur.Zone = a.loc.String()
	}
	loc := a.loc
	from, to := now.Add(-back), now.Add(ahead)
	full := !learned || now.Sub(time.Unix(cur.Full, 0)) >= fullEvery
	old := byObject(cur.Occ)
	oldObjs := cur.Objs
	newObjs := map[string]objState{}
	for k, v := range oldObjs {
		newObjs[k] = v
	}
	occ := map[string][]kept{}
	for k, v := range old {
		occ[k] = v
	}
	touched := map[string]bool{} // objects whose ETag moved or which went away
	exists := map[string]bool{}  // objects still on the server
	created := map[string]bool{} // objects whose CREATED is after the last sync
	put := func(obj, etag, cal string, list []occurrence, createdAt time.Time) {
		touched[obj], exists[obj] = true, true
		newObjs[obj] = objState{E: etag, Cal: cal}
		var ks []kept
		for _, o := range list {
			ks = append(ks, keep(o))
		}
		occ[obj] = ks
		if !createdAt.IsZero() && createdAt.Unix() > cur.Synced-600 {
			created[obj] = true
		}
	}
	drop := func(obj string) {
		touched[obj] = true
		delete(newObjs, obj)
		delete(occ, obj)
	}

	if a.link != "" {
		etag, lastM := cur.ETag, cur.LastM
		if full {
			etag, lastM = "", "" // the window moved: the repeating meetings are expanded anew even if the file did not change
		}
		f, err := fetchICS(ctx, in.HTTP, a.link, etag, lastM)
		if err != nil {
			return cur, nil, err
		}
		if !f.notModified {
			if f.zone != "" && !a.userZone {
				loc = zone(f.zone, loc)
				cur.Zone = loc.String()
			}
			cur.ETag, cur.LastM = f.etag, f.lastModified
			byUID, order, err := parse(f.data)
			if err != nil {
				return cur, nil, connectors.Sayf("The calendar's file is not iCalendar")
			}
			seen := map[string]bool{}
			for _, uid := range order {
				obj := "uid:" + uid
				seen[obj] = true
				etag := fingerprint(byUID[uid])
				if st, ok := oldObjs[obj]; ok && st.E == etag && !full {
					continue
				}
				list, err := expandComps(byUID[uid], uid, source{Calendar: f.name}, from, to, loc, nil)
				if err != nil {
					continue
				}
				if len(list) == 0 && oldObjs[obj].E == "" {
					continue // not in the window and not known: nothing to keep
				}
				if st, ok := oldObjs[obj]; ok && st.E == etag {
					// The window moved (a full pass): same object, new times in the window, no change to tell.
					if len(list) == 0 {
						delete(newObjs, obj)
						delete(occ, obj)
						continue
					}
					newObjs[obj] = st
					var ks []kept
					for _, o := range list {
						ks = append(ks, keep(o))
					}
					occ[obj] = ks
					continue
				}
				put(obj, etag, "", list, createdOf(byUID[uid]))
			}
			for obj := range oldObjs {
				if !seen[obj] {
					drop(obj)
				}
			}
			cur.Full = now.Unix()
		}
	} else {
		states := map[string]calState{}
		for _, c := range cur.Cals {
			states[c.H] = c
		}
		var cals []calState
		chosen := map[string]bool{}
		for _, c := range a.cals {
			chosen[c.Href] = true
			st := states[c.Href]
			next := calState{H: c.Href, N: c.Name, C: c.CTag, T: c.SyncToken}
			if !full && st.H != "" && (c.CTag != "" || c.SyncToken != "") && st.C == c.CTag && st.T == c.SyncToken {
				cals = append(cals, next) // nothing moved in this calendar
				continue
			}
			ok := false
			if !full && st.T != "" {
				changed, gone, token, err := a.dav.changes(ctx, c.Href, st.T)
				if err == nil {
					var want []string
					for _, o := range changed {
						if oldObjs[o.Href].E != o.ETag || o.ETag == "" {
							want = append(want, o.Href)
						}
					}
					objs, err := a.dav.fetch(ctx, c.Href, want)
					if err == nil {
						for _, o := range objs {
							list, created := expandObject(o, c, from, to, loc, a.me)
							if len(list) == 0 && oldObjs[o.Href].E == "" {
								continue
							}
							put(o.Href, o.ETag, c.Href, list, created)
						}
						for _, h := range gone {
							if _, known := oldObjs[h]; known {
								drop(h)
							}
						}
						if token != "" {
							next.T = token
						}
						ok = true
					}
				}
			}
			if !ok {
				if err := fullPass(ctx, a, c, from, to, loc, oldObjs, newObjs, occ, put, drop, exists); err != nil {
					return cur, nil, say(err)
				}
			}
			cals = append(cals, next)
		}
		// A calendar no longer chosen: its meetings go, quietly.
		for obj, st := range oldObjs {
			if st.Cal != "" && !chosen[st.Cal] {
				delete(newObjs, obj)
				delete(occ, obj)
				delete(touched, obj)
			}
		}
		cur.Cals = cals
		if full {
			cur.Full = now.Unix()
		}
	}
	var events []connectors.Event
	if learned {
		events = diff(old, oldObjs, occ, newObjs, touched, exists, created, now, loc, cur.Synced)
	}
	cur.Objs = newObjs
	cur.Occ = flatten(occ, from)
	cur.Synced = now.Unix()
	return cur, events, nil
}

// fullPass reads a calendar's whole window (calendar-query) and makes the kept objects of it match.
func fullPass(ctx context.Context, a *account, c calendar, from, to time.Time, loc *time.Location,
	oldObjs, newObjs map[string]objState, occ map[string][]kept,
	put func(obj, etag, cal string, list []occurrence, created time.Time), drop func(obj string), exists map[string]bool) error {
	objs, err := a.dav.query(ctx, c.Href, from.UTC().Format("20060102T150405Z"), to.UTC().Format("20060102T150405Z"))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, o := range objs {
		seen[o.Href] = true
		list, created := expandObject(o, c, from, to, loc, a.me)
		if st, ok := oldObjs[o.Href]; ok && st.E == o.ETag && o.ETag != "" {
			exists[o.Href] = true
			if len(list) == 0 {
				delete(newObjs, o.Href)
				delete(occ, o.Href)
				continue
			}
			newObjs[o.Href] = st
			var ks []kept
			for _, x := range list {
				ks = append(ks, keep(x))
			}
			occ[o.Href] = ks
			exists[o.Href] = true
			continue
		}
		put(o.Href, o.ETag, c.Href, list, created)
	}
	// Known objects of this calendar the window no longer returns: gone, or moved out of the window.
	var missing []string
	for obj, st := range oldObjs {
		if st.Cal == c.Href && !seen[obj] {
			missing = append(missing, obj)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	still, err := a.dav.fetch(ctx, c.Href, missing)
	if err != nil {
		still = nil
	}
	found := map[string]bool{}
	for _, o := range still {
		found[o.Href] = true
		list, created := expandObject(o, c, from, to, loc, a.me)
		put(o.Href, o.ETag, c.Href, list, created)
		if len(list) == 0 {
			delete(newObjs, o.Href) // out of the window: not kept, told as moved by diff
		}
	}
	for _, obj := range missing {
		if !found[obj] {
			drop(obj)
		}
	}
	return nil
}

func expandObject(o object, c calendar, from, to time.Time, loc *time.Location, me map[string]bool) ([]occurrence, time.Time) {
	byUID, order, err := parse(o.Data)
	if err != nil {
		return nil, time.Time{}
	}
	var out []occurrence
	var created time.Time
	for _, uid := range order {
		list, err := expandComps(byUID[uid], uid, source{Object: o.Href, Calendar: c.Name}, from, to, loc, me)
		if err == nil {
			out = append(out, list...)
		}
		if t := createdOf(byUID[uid]); !t.IsZero() {
			created = t
		}
	}
	return out, created
}

// expandComps is expand for the components of one UID already parsed.
func expandComps(comps []*ical.Component, uid string, src source, from, to time.Time, loc *time.Location, me map[string]bool) ([]occurrence, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//harnessly//calendar//EN")
	cal.Children = comps
	var b strings.Builder
	if err := ical.NewEncoder(&b).Encode(cal); err != nil {
		return nil, err
	}
	return expand(b.String(), src, from, to, loc, me, false)
}

func createdOf(comps []*ical.Component) time.Time {
	for _, c := range comps {
		if c.Props.Get(ical.PropRecurrenceID) != nil {
			continue
		}
		if t, _, err := first(c, ical.PropCreated, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

func byObject(list []kept) map[string][]kept {
	m := map[string][]kept{}
	for _, k := range list {
		m[k.O] = append(m[k.O], k)
	}
	return m
}

func flatten(m map[string][]kept, from time.Time) []kept {
	var out []kept
	for _, list := range m {
		for _, k := range list {
			if k.E >= from.Unix() || k.S >= from.Unix() {
				out = append(out, k)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].S != out[j].S {
			return out[i].S < out[j].S
		}
		return out[i].K < out[j].K
	})
	return out
}

func short(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// diff compares the meetings of each object that moved: one calendar.changed per object and kind of change.
func diff(old map[string][]kept, oldObjs map[string]objState, occ map[string][]kept, newObjs map[string]objState,
	touched, exists, created map[string]bool, now time.Time, loc *time.Location, lastSync int64) []connectors.Event {
	var objs []string
	for obj := range touched {
		objs = append(objs, obj)
	}
	sort.Strings(objs)
	future := func(list []kept) []kept {
		var out []kept
		for _, k := range list {
			if k.S > now.Unix() {
				out = append(out, k)
			}
		}
		return out
	}
	var events []connectors.Event
	for _, obj := range objs {
		before, after := future(old[obj]), future(occ[obj])
		etag := newObjs[obj].E
		if etag == "" {
			etag = "gone"
		}
		tag := func(kind string) string { return "changed:" + short(obj) + ":" + kind + ":" + short(etag) }
		_, wasKnown := oldObjs[obj]
		switch {
		case !wasKnown:
			if len(after) == 0 || after[0].cancelled() {
				continue
			}
			if created[obj] || time.Unix(after[0].S, 0).Before(now.Add(freshAhead)) {
				events = append(events, changedEvent(tag("created"), "created", after[0], nil, loc))
			}
		case !exists[obj]:
			if len(before) > 0 && !before[0].cancelled() {
				events = append(events, changedEvent(tag("cancelled"), "cancelled", before[0], nil, loc))
			}
		default:
			if len(after) == 0 && len(occ[obj]) == 0 && len(before) > 0 {
				// Still there, no longer in the window: moved far away.
				events = append(events, changedEvent(tag("moved"), "moved", before[0], &before[0], loc))
				continue
			}
			was := map[string]kept{}
			for _, k := range before {
				was[k.K] = k
			}
			is := map[string]kept{}
			for _, k := range occ[obj] {
				is[k.K] = k
			}
			var cancelled, moved, updated *kept
			var movedFrom kept
			for _, k := range before {
				n, ok := is[k.K]
				switch {
				case k.cancelled():
				case !ok || n.cancelled():
					if cancelled == nil {
						c := k
						cancelled = &c
					}
				case n.S != k.S || n.E != k.E:
					if moved == nil {
						m := n
						moved, movedFrom = &m, k
					}
				case n.T != k.T || n.L != k.L || n.V != k.V || n.N != k.N:
					if updated == nil {
						u := n
						updated = &u
					}
				}
			}
			// An exception moved from outside the window into it, or a series made to start earlier.
			if moved == nil {
				for _, k := range after {
					if _, ok := was[k.K]; !ok && !k.cancelled() && len(before) > 0 {
						m := k
						moved, movedFrom = &m, before[0]
						break
					}
				}
			}
			if cancelled != nil {
				events = append(events, changedEvent(tag("cancelled"), "cancelled", *cancelled, nil, loc))
			}
			if moved != nil {
				events = append(events, changedEvent(tag("moved"), "moved", *moved, &movedFrom, loc))
			}
			if updated != nil {
				events = append(events, changedEvent(tag("updated"), "updated", *updated, nil, loc))
			}
		}
	}
	return events
}

// facts of a meeting a rule can filter on: times, whole day, calendar, how many people and their names (never their
// addresses), the call's service, the person's own answer.
func facts(k kept, loc *time.Location) map[string]string {
	f := map[string]string{
		"startsAt": k.start(loc).Format(time.RFC3339),
		"endsAt":   k.end(loc).Format(time.RFC3339),
	}
	if k.A {
		f["allDay"] = "true"
		f["startsAt"] = k.start(loc).Format(time.DateOnly)
		f["endsAt"] = k.end(loc).Format(time.DateOnly)
	}
	if k.C != "" {
		f["calendar"] = k.C
	}
	if k.N > 0 {
		f["attendees"] = strconv.Itoa(k.N)
	}
	if k.P != "" {
		f["people"] = k.P
	}
	if k.V != "" {
		f["call"] = callName(k.V)
	}
	if k.Me != "" {
		f["response"] = strings.ToLower(k.Me)
	}
	if k.R {
		f["recurring"] = "true"
	}
	return f
}

func envelope(typ, id string, k kept, loc *time.Location) connectors.Event {
	return connectors.Event{
		Type: typ, ID: id, Actor: k.Org, Title: k.T, URL: k.V, Thread: k.U,
		Ref: "event id=" + k.K, Facts: facts(k, loc),
	}
}

func changedEvent(id, change string, k kept, from *kept, loc *time.Location) connectors.Event {
	ev := envelope(EventChanged, id, k, loc)
	ev.Facts["change"] = change
	if from != nil && change == "moved" && (from.S != k.S || from.K == k.K) {
		ev.Facts["movedFrom"] = from.start(loc).Format(time.RFC3339)
	}
	return ev
}

// timed gives the events of this minute from the kept meetings: about to start, just ended, the day's schedule.
func timed(cur *cursor, a *account, now time.Time) []connectors.Event {
	loc := a.loc
	var events []connectors.Event
	for k, at := range cur.Fired {
		if now.Unix()-at > int64((48 * time.Hour).Seconds()) {
			delete(cur.Fired, k)
		}
	}
	fire := func(key string, ev connectors.Event) {
		if _, done := cur.Fired[key]; done {
			return
		}
		cur.Fired[key] = now.Unix()
		events = append(events, ev)
	}
	for _, k := range cur.Occ {
		if k.A || k.cancelled() || k.Me == "DECLINED" {
			continue
		}
		start, end := k.start(loc), k.end(loc)
		if !now.Before(start.Add(-a.remind)) && now.Before(start) {
			id := "starting:" + short(k.K) + ":" + strconv.FormatInt(k.S, 10)
			ev := envelope(EventStarting, id, k, loc)
			mins := int((start.Sub(now) + time.Minute - 1) / time.Minute)
			ev.Facts["minutes"] = strconv.Itoa(mins)
			fire(id, ev)
		}
		if end.After(start) && !now.Before(end) && now.Before(end.Add(endedFor)) {
			id := "ended:" + short(k.K) + ":" + strconv.FormatInt(k.E, 10)
			ev := envelope(EventEnded, id, k, loc)
			ev.At = end
			fire(id, ev)
		}
	}
	if ev, ok := day(cur, a, now); ok {
		events = append(events, ev)
	}
	return events
}

// day is the morning's schedule: once a day, from the morning hour for four hours (a machine that slept past that
// gives none rather than a stale one).
func day(cur *cursor, a *account, now time.Time) (connectors.Event, bool) {
	local := now.In(a.loc)
	date := local.Format(time.DateOnly)
	if a.morning <= 0 || cur.Day == date || local.Hour() < a.morning || local.Hour() >= a.morning+4 {
		return connectors.Event{}, false
	}
	cur.Day = date
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, a.loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	var meetings, allDay, calls int
	var busy time.Duration
	var firstAt, lastAt time.Time
	for _, k := range cur.Occ {
		if k.cancelled() || k.Me == "DECLINED" {
			continue
		}
		s, e := k.start(a.loc), k.end(a.loc)
		if k.A {
			if s.Before(dayEnd) && e.After(dayStart) {
				allDay++
			}
			continue
		}
		if !s.Before(dayEnd) || !e.After(dayStart) {
			continue
		}
		meetings++
		if k.V != "" {
			calls++
		}
		if firstAt.IsZero() || s.Before(firstAt) {
			firstAt = s
		}
		if e.After(lastAt) {
			lastAt = e
		}
		if !k.F {
			end := e
			if end.After(dayEnd) {
				end = dayEnd
			}
			busy += end.Sub(maxTime(s, dayStart))
		}
	}
	f := map[string]string{
		"date":        date,
		"meetings":    strconv.Itoa(meetings),
		"busyMinutes": strconv.Itoa(int(busy / time.Minute)),
	}
	title := fmt.Sprintf("%s: no meetings", date)
	if meetings > 0 {
		f["first"] = firstAt.Format("15:04")
		f["last"] = lastAt.Format("15:04")
		title = fmt.Sprintf("%s: %d meetings, %s–%s", date, meetings, firstAt.Format("15:04"), lastAt.Format("15:04"))
		if meetings == 1 {
			title = fmt.Sprintf("%s: 1 meeting, %s–%s", date, firstAt.Format("15:04"), lastAt.Format("15:04"))
		}
	}
	if allDay > 0 {
		f["allDay"] = strconv.Itoa(allDay)
	}
	if calls > 0 {
		f["calls"] = strconv.Itoa(calls)
	}
	return connectors.Event{
		Type: EventDay, ID: "day:" + date, Title: title, Thread: "day:" + date,
		Ref: "events from=" + date + " to=" + date, Facts: f,
	}, true
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// icsFile is an .ics address's answer.
type icsFile struct {
	data         string
	name         string
	zone         string
	etag         string
	lastModified string
	notModified  bool
}

// fetchICS asks a calendar's .ics address, conditionally when validators are given.
func fetchICS(ctx context.Context, client *http.Client, link, etag, lastModified string) (icsFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return icsFile{}, connectors.Sayf("The calendar's address is not valid")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/calendar, */*;q=0.1")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	res, err := client.Do(req)
	if err != nil {
		return icsFile{}, say(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return icsFile{notModified: true, etag: etag, lastModified: lastModified}, nil
	}
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return icsFile{}, connectors.Sayf("The calendar's address does not open (%d): it may have been reset; copy the secret address again", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return icsFile{}, connectors.Sayf("The calendar's address answered %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return icsFile{}, say(err)
	}
	data := string(raw)
	if !strings.Contains(data, "BEGIN:VCALENDAR") {
		return icsFile{}, connectors.Sayf("This address gives no calendar: not an .ics file")
	}
	f := icsFile{data: data, etag: res.Header.Get("ETag"), lastModified: res.Header.Get("Last-Modified")}
	dec := ical.NewDecoder(strings.NewReader(data))
	if cal, err := dec.Decode(); err == nil {
		f.name = text(cal.Component, "X-WR-CALNAME")
		f.zone = text(cal.Component, "X-WR-TIMEZONE")
	}
	return f, nil
}
