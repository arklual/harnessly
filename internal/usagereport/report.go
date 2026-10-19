package usagereport

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/pricing"
)

const (
	// WindowDays is how far back the report looks.
	WindowDays   = 400
	sessionLimit = 400
	daySec       = 86400
)

// Day is one day's activity, by the report's zone.
type Day struct {
	Prompts, Sessions, TurnMs, Tools, ToolErrors, Interrupts, Denials, Compactions, PRs float64
	// Hours are the prompts per hour of the day.
	Hours [24]float64
}

// UsageRow is the usage of a day, provider, model and project.
type UsageRow struct {
	Day, Provider, Model, Project                        string
	Input, Output, CacheRead, CacheWrite, Cost, APICalls float64
}

// Session is one conversation, its subagents included.
type Session struct {
	ID, Provider                                             string
	Title, Project                                           *string
	Start, End, Prompts, TurnMs                              float64
	Input, Output, CacheRead, CacheWrite, Cost, SubagentCost float64
	Model                                                    *string
	Tasks, PRs                                               []string
}

// Progress is how far the first pass over the transcripts has got.
type Progress struct{ Done, Total int }

// Report is a runtime's usage report.
type Report struct {
	Person, Machine, GeneratedAt, Timezone string
	Days                                   []string
	DayStats                               map[string]*Day
	Usage                                  []*UsageRow
	Sessions                               []*Session
	Collecting                             *Progress
}

// errModel: a transcript named its model with a value that is not a string; the previous runtime's report failed on it.
var errModel = errors.New("a transcript's model is not a string")

func costOf(provider string, model any, input, output, cacheRead, write5m, write1h float64) (float64, error) {
	m, ok := model.(string)
	if !ok {
		return 0, errModel
	}
	if provider == "claude" {
		return pricing.ClaudeCostByFullID(m, input, output, write5m, write1h, cacheRead), nil
	}
	c, _ := pricing.OpenAICost(m, input+cacheRead, cacheRead, output)
	return c, nil
}

// Clock is the report's day boundary: an offset from UTC in seconds.
type Clock struct{ Offset float64 }

// dayKeys remembers the key of each day number met while building one report.
type dayKeys map[float64]string

func (d dayKeys) key(c Clock, ts float64) string {
	n := math.Floor((ts + c.Offset) / daySec)
	if k, ok := d[n]; ok {
		return k
	}
	k := c.dayKey(ts)
	d[n] = k
	return k
}

func (c Clock) dayKey(ts float64) string {
	s, _ := jscompat.ISOString((ts + c.Offset) * 1000)
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func (c Clock) hourOf(ts float64) int {
	return time.UnixMilli(int64((ts + c.Offset) * 1000)).UTC().Hour()
}

// dedup is a JavaScript Set of keys.
type dedup map[string]struct{}

// seen adds k and says whether it was there already; an object or array is never there.
func (d dedup) seen(k any) bool {
	s, unique := setKey(k)
	if unique {
		return false
	}
	if _, ok := d[s]; ok {
		return true
	}
	d[s] = struct{}{}
	return false
}

func (d dedup) seenString(s string) bool {
	if _, ok := d[s]; ok {
		return true
	}
	d[s] = struct{}{}
	return false
}

var spaces = regexp.MustCompile(jscompat.SpaceClass + `+`)

// jsRound is Math.round.
func jsRound(x float64) float64 {
	f := math.Floor(x)
	if x-f >= 0.5 {
		f++
	}
	return f
}

type keyed[V any] struct {
	keys []string
	m    map[string]V
}

func (k *keyed[V]) get(key string) (V, bool) {
	v, ok := k.m[key]
	return v, ok
}

func (k *keyed[V]) set(key string, v V) {
	if k.m == nil {
		k.m = map[string]V{}
	}
	if _, ok := k.m[key]; !ok {
		k.keys = append(k.keys, key)
	}
	k.m[key] = v
}

// Build reduces the facts of every transcript into the report, as of nowMs.
func Build(files []*Facts, person, machine, timezone string, clock Clock, nowMs float64) (*Report, error) {
	cutoff := math.Floor(nowMs/1000) - WindowDays*daySec
	// Originals first: a resumed or forked copy re-lists history under old ids.
	ordered := append([]*Facts(nil), files...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.FirstTs != b.FirstTs {
			return a.FirstTs < b.FirstTs
		}
		return !a.Sidechain && b.Sidechain
	})
	seenMsg, seenPrompt, seenTurn, seenTool, seenEvent, seenPR := dedup{}, dedup{}, dedup{}, dedup{}, dedup{}, dedup{}
	days := map[string]*Day{}
	var dayOrder []string
	daySessions := keyed[map[string]struct{}]{}
	usage := keyed[*UsageRow]{}
	sessions := keyed[*Session]{}
	modelCost := map[string]*keyed[float64]{}
	projectPrompts := map[string]*keyed[float64]{}
	keys := dayKeys{}
	dayOf := func(ts float64) *Day {
		k := keys.key(clock, ts)
		d := days[k]
		if d == nil {
			d = &Day{}
			days[k] = d
			dayOrder = append(dayOrder, k)
		}
		return d
	}
	addDaySession := func(day, id string) {
		set, ok := daySessions.get(day)
		if !ok {
			set = map[string]struct{}{}
			daySessions.set(day, set)
		}
		set[id] = struct{}{}
	}
	bump := func(m map[string]*keyed[float64], id, k string, by float64) {
		kv := m[id]
		if kv == nil {
			kv = &keyed[float64]{}
			m[id] = kv
		}
		v, _ := kv.get(k)
		kv.set(k, v+by)
	}
	tasksSeen := map[string]map[string]bool{}
	prsSeen := map[string]map[string]bool{}
	appendOnce := func(seen map[string]map[string]bool, id string, list *[]string, v string) bool {
		s := seen[id]
		if s == nil {
			s = map[string]bool{}
			seen[id] = s
		}
		if s[v] {
			return false
		}
		s[v] = true
		*list = append(*list, v)
		return true
	}

	for _, f := range ordered {
		id := f.Provider + ":" + f.SessionID
		session, ok := sessions.get(id)
		if !ok {
			session = &Session{ID: id, Provider: f.Provider, Tasks: []string{}, PRs: []string{}}
			sessions.set(id, session)
		}
		if !f.Sidechain {
			if session.Title == nil {
				if f.Title != nil {
					t := *f.Title
					session.Title = &t
				} else if f.FirstPrompt != nil {
					t := *f.FirstPrompt
					session.Title = &t
				}
			}
			for _, t := range f.Tasks {
				appendOnce(tasksSeen, id, &session.Tasks, t)
			}
		}
		if f.FirstTs != 0 && (session.Start == 0 || f.FirstTs < session.Start) {
			session.Start = f.FirstTs
		}
		if f.LastTs > session.End {
			session.End = f.LastTs
		}
		for _, pr := range f.PRs {
			appendOnce(prsSeen, id, &session.PRs, pr)
			if seenPR.seenString(pr) {
				continue
			}
			if f.LastTs >= cutoff {
				dayOf(f.LastTs).PRs++
			}
		}

		for _, m := range f.Messages {
			if m.Ts < cutoff || seenMsg.seen(m.Key) {
				continue
			}
			var model any = "unknown"
			if m.Model >= 0 && m.Model < len(f.Models) && f.Models[m.Model] != nil {
				model = f.Models[m.Model]
			}
			project := noProject
			if m.Project >= 0 && m.Project < len(f.Projects) {
				project = f.Projects[m.Project]
			}
			cost, err := costOf(f.Provider, model, m.Input, m.Output, m.CacheRead, m.W5m, m.W1h)
			if err != nil {
				return nil, err
			}
			name := model.(string)
			day := keys.key(clock, m.Ts)
			rowKey := day + "|" + f.Provider + "|" + name + "|" + project
			row, ok := usage.get(rowKey)
			if !ok {
				row = &UsageRow{Day: day, Provider: f.Provider, Model: name, Project: project}
				usage.set(rowKey, row)
			}
			row.Input += m.Input
			row.Output += m.Output
			row.CacheRead += m.CacheRead
			row.CacheWrite += m.W5m + m.W1h
			row.Cost += cost
			row.APICalls++
			session.Input += m.Input
			session.Output += m.Output
			session.CacheRead += m.CacheRead
			session.CacheWrite += m.W5m + m.W1h
			session.Cost += cost
			if f.Sidechain {
				session.SubagentCost += cost
			}
			bump(modelCost, session.ID, name, cost)
			addDaySession(day, session.ID)
		}

		for _, p := range f.Prompts {
			if p.Ts < cutoff || seenPrompt.seen(p.Key) {
				continue
			}
			day := dayOf(p.Ts)
			day.Prompts++
			day.Hours[clock.hourOf(p.Ts)]++
			session.Prompts++
			project := noProject
			if p.Project >= 0 && p.Project < len(f.Projects) {
				project = f.Projects[p.Project]
			}
			bump(projectPrompts, session.ID, project, 1)
			addDaySession(keys.key(clock, p.Ts), session.ID)
		}
		for _, t := range f.Turns {
			if t.Ts < cutoff || seenTurn.seen(t.Key) {
				continue
			}
			dayOf(t.Ts).TurnMs += t.Ms
			session.TurnMs += t.Ms
		}
		for _, t := range f.Tools {
			if t.Ts < cutoff || seenTool.seen(t.Key) {
				continue
			}
			day := dayOf(t.Ts)
			day.Tools++
			day.ToolErrors += t.Error
		}
		for _, ev := range []struct {
			field string
			rows  []EventRow
			count func(*Day)
		}{
			{"interrupts", f.Interrupts, func(d *Day) { d.Interrupts++ }},
			{"denials", f.Denials, func(d *Day) { d.Denials++ }},
			{"compactions", f.Compactions, func(d *Day) { d.Compactions++ }},
		} {
			for _, e := range ev.rows {
				if e.Ts < cutoff || seenEvent.seenString(ev.field+":"+jscompat.String(e.Key)) {
					continue
				}
				ev.count(dayOf(e.Ts))
			}
		}
	}

	for _, day := range daySessions.keys {
		d := days[day]
		if d == nil {
			d = &Day{}
			days[day] = d
			dayOrder = append(dayOrder, day)
		}
		d.Sessions = float64(len(daySessions.m[day]))
	}
	top := func(kv *keyed[float64]) *string {
		if kv == nil || len(kv.keys) == 0 {
			return nil
		}
		best := kv.keys[0]
		for _, k := range kv.keys[1:] {
			if kv.m[k] > kv.m[best] {
				best = k
			}
		}
		return &best
	}
	list := make([]*Session, 0, len(sessions.keys))
	for _, id := range sessions.keys {
		s := sessions.m[id]
		if kv := modelCost[id]; kv != nil {
			s.Model = top(kv)
		}
		if kv := projectPrompts[id]; kv != nil {
			s.Project = top(kv)
		}
		if s.Title != nil && *s.Title != "" {
			t := jscompat.Slice(jscompat.Trim(spaces.ReplaceAllString(*s.Title, " ")), 140)
			s.Title = &t
		}
		if s.End >= cutoff && (s.Cost > 0 || s.Prompts > 0) {
			list = append(list, s)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Cost > list[j].Cost })
	if len(list) > sessionLimit {
		list = list[:sessionLimit]
	}
	sort.Strings(dayOrder)
	rows := make([]*UsageRow, 0, len(usage.keys))
	for _, k := range usage.keys {
		r := usage.m[k]
		r.Cost = jsRound(r.Cost*10000) / 10000
		rows = append(rows, r)
	}
	generated, _ := jscompat.ISOString(nowMs)
	return &Report{Person: person, Machine: machine, GeneratedAt: generated, Timezone: timezone, Days: dayOrder, DayStats: days,
		Usage: rows, Sessions: list}, nil
}

// Empty is the report while nothing is built yet.
func Empty(person, machine, timezone string, nowMs float64, p Progress) *Report {
	generated, _ := jscompat.ISOString(nowMs)
	return &Report{Person: person, Machine: machine, GeneratedAt: generated, Timezone: timezone, DayStats: map[string]*Day{},
		Usage: []*UsageRow{}, Sessions: []*Session{}, Collecting: &p}
}

// With is the report for another person, and with the progress of a pass still running (nil when none).
func (r *Report) With(person string, collecting *Progress) *Report {
	c := *r
	c.Person = person
	c.Collecting = collecting
	return &c
}

func (r *Report) write(w *jw) {
	w.WriteString(`{"version":1,"person":`)
	w.str(r.Person)
	w.WriteString(`,"machine":`)
	w.str(r.Machine)
	w.WriteString(`,"generatedAt":`)
	w.str(r.GeneratedAt)
	w.WriteString(`,"timezone":`)
	w.str(r.Timezone)
	w.WriteString(`,"days":{`)
	for i, k := range r.Days {
		if i > 0 {
			w.WriteByte(',')
		}
		d := r.DayStats[k]
		w.key(k)
		w.WriteString(`{"prompts":`)
		w.num(d.Prompts)
		w.WriteString(`,"sessions":`)
		w.num(d.Sessions)
		w.WriteString(`,"turnMs":`)
		w.num(d.TurnMs)
		w.WriteString(`,"tools":`)
		w.num(d.Tools)
		w.WriteString(`,"toolErrors":`)
		w.num(d.ToolErrors)
		w.WriteString(`,"interrupts":`)
		w.num(d.Interrupts)
		w.WriteString(`,"denials":`)
		w.num(d.Denials)
		w.WriteString(`,"compactions":`)
		w.num(d.Compactions)
		w.WriteString(`,"prs":`)
		w.num(d.PRs)
		w.WriteString(`,"hours":[`)
		for h, n := range d.Hours {
			if h > 0 {
				w.WriteByte(',')
			}
			w.num(n)
		}
		w.WriteString(`]}`)
	}
	w.WriteString(`},"usage":[`)
	for i, u := range r.Usage {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteByte('[')
		w.str(u.Day)
		w.WriteByte(',')
		w.str(u.Provider)
		w.WriteByte(',')
		w.str(u.Model)
		w.WriteByte(',')
		w.str(u.Project)
		for _, x := range []float64{u.Input, u.Output, u.CacheRead, u.CacheWrite, u.Cost, u.APICalls} {
			w.WriteByte(',')
			w.num(x)
		}
		w.WriteByte(']')
	}
	w.WriteString(`],"sessions":[`)
	for i, s := range r.Sessions {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteString(`{"id":`)
		w.str(s.ID)
		w.WriteString(`,"provider":`)
		w.str(s.Provider)
		w.WriteString(`,"title":`)
		w.strPtr(s.Title)
		w.WriteString(`,"project":`)
		w.strPtr(s.Project)
		for _, f := range []struct {
			k string
			v float64
		}{{"start", s.Start}, {"end", s.End}, {"prompts", s.Prompts}, {"turnMs", s.TurnMs}, {"input", s.Input}, {"output", s.Output},
			{"cacheRead", s.CacheRead}, {"cacheWrite", s.CacheWrite}, {"cost", s.Cost}, {"subagentCost", s.SubagentCost}} {
			w.WriteString(`,"`)
			w.WriteString(f.k)
			w.WriteString(`":`)
			w.num(f.v)
		}
		w.WriteString(`,"model":`)
		w.strPtr(s.Model)
		w.WriteString(`,"tasks":`)
		w.strings(s.Tasks)
		w.WriteString(`,"prs":`)
		w.strings(s.PRs)
		w.WriteByte('}')
	}
	w.WriteString(`],"collecting":`)
	if r.Collecting == nil {
		w.WriteString("null")
	} else {
		w.WriteString(`{"done":`)
		w.num(float64(r.Collecting.Done))
		w.WriteString(`,"total":`)
		w.num(float64(r.Collecting.Total))
		w.WriteByte('}')
	}
	w.WriteByte('}')
}

// JSON is the report as the previous runtime wrote it.
func (r *Report) JSON() []byte {
	var w jw
	r.write(&w)
	return w.Bytes()
}
