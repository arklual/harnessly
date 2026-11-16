package caldav

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// The agent's reads: they ask the server at the moment (nothing of the cursor), so they see what the person sees.
// Everything from the calendar (titles, places, names, descriptions) goes in Result.Data, marked as untrusted.

const (
	maxDays    = 62
	maxFree    = 14
	maxListing = 300
)

// window reads the meetings of [from, to) in all the chosen calendars, with descriptions when withText.
func window(ctx context.Context, in connectors.Instance, a *account, from, to time.Time, withText bool) ([]occurrence, error) {
	if a.link != "" {
		f, err := fetchICS(ctx, in.HTTP, a.link, "", "")
		if err != nil {
			return nil, err
		}
		loc := a.loc
		if f.zone != "" && !a.userZone {
			loc = zone(f.zone, loc)
			a.loc = loc
		}
		return expand(f.data, source{Calendar: f.name}, from, to, loc, nil, withText)
	}
	var out []occurrence
	for _, c := range a.cals {
		objs, err := a.dav.query(ctx, c.Href, from.UTC().Format("20060102T150405Z"), to.UTC().Format("20060102T150405Z"))
		if err != nil {
			return nil, say(err)
		}
		for _, o := range objs {
			list, err := expand(o.Data, source{Object: o.Href, Calendar: c.Name}, from, to, a.loc, a.me, withText)
			if err == nil {
				out = append(out, list...)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func dayArg(v string, loc *time.Location, def time.Time) (time.Time, error) {
	if v = strings.TrimSpace(v); v == "" {
		return def, nil
	}
	t, err := time.ParseInLocation(time.DateOnly, v, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: a day is YYYY-MM-DD", connectors.ErrBadArgs)
	}
	return t, nil
}

func clockArg(v string, def int) (int, error) {
	if v = strings.TrimSpace(v); v == "" {
		return def, nil
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, fmt.Errorf("%w: a time of day is HH:MM", connectors.ErrBadArgs)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func midnight(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func (calConn) Read(ctx context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error) {
	switch tool {
	case "events", "free", "event":
	default:
		return connectors.Result{}, connectors.ErrUnknownTool
	}
	a, _, err := login(ctx, in)
	if err != nil {
		return connectors.Result{}, err
	}
	if a.link != "" {
		// The file's own zone, so a day means the calendar's day.
		if f, err := fetchICS(ctx, in.HTTP, a.link, "", ""); err == nil && f.zone != "" && !a.userZone {
			a.loc = zone(f.zone, a.loc)
		}
	}
	from := "calendar: " + in.Name
	switch tool {
	case "events":
		today := midnight(clock(), a.loc)
		start, err := dayArg(args["from"], a.loc, today)
		if err != nil {
			return connectors.Result{}, err
		}
		last, err := dayArg(args["to"], a.loc, start.AddDate(0, 0, 6))
		if err != nil {
			return connectors.Result{}, err
		}
		end := last.AddDate(0, 0, 1)
		if !end.After(start) || end.Sub(start) > maxDays*24*time.Hour {
			return connectors.Result{}, fmt.Errorf("%w: from must be before to, at most %d days", connectors.ErrBadArgs, maxDays)
		}
		list, err := window(ctx, in, a, start, end, false)
		if err != nil {
			return connectors.Result{}, err
		}
		var b strings.Builder
		n := 0
		for _, o := range list {
			if name := strings.TrimSpace(args["calendar"]); name != "" && !strings.EqualFold(name, o.Calendar) {
				continue
			}
			if n == maxListing {
				b.WriteString("…\n")
				break
			}
			n++
			b.WriteString(line(o, a.loc))
		}
		text := fmt.Sprintf("%d meetings from %s to %s (%s).", n, start.Format(time.DateOnly), last.Format(time.DateOnly), a.loc)
		if n == 0 {
			return connectors.Result{Text: text}, nil
		}
		return connectors.Result{Text: text + " Read one whole with `event id=…`.", Data: b.String(), From: from}, nil

	case "free":
		start, err := dayArg(args["date"], a.loc, midnight(clock(), a.loc))
		if err != nil {
			return connectors.Result{}, err
		}
		days := 1
		if v := strings.TrimSpace(args["days"]); v != "" {
			if days, err = strconv.Atoi(v); err != nil || days < 1 {
				return connectors.Result{}, fmt.Errorf("%w: days is a number", connectors.ErrBadArgs)
			}
		}
		days = min(days, maxFree)
		dayFrom, err := clockArg(args["from"], 9*60)
		if err != nil {
			return connectors.Result{}, err
		}
		dayTo, err := clockArg(args["to"], 19*60)
		if err != nil {
			return connectors.Result{}, err
		}
		least := 30
		if v := strings.TrimSpace(args["min"]); v != "" {
			if least, err = strconv.Atoi(v); err != nil || least < 5 {
				return connectors.Result{}, fmt.Errorf("%w: min is a number of minutes, at least 5", connectors.ErrBadArgs)
			}
		}
		if dayTo <= dayFrom {
			return connectors.Result{}, fmt.Errorf("%w: the working day ends before it starts", connectors.ErrBadArgs)
		}
		list, err := window(ctx, in, a, start, start.AddDate(0, 0, days), false)
		if err != nil {
			return connectors.Result{}, err
		}
		return connectors.Result{Text: freeText(list, start, days, dayFrom, dayTo, time.Duration(least)*time.Minute, clock(), a.loc)}, nil

	default: // event
		id := strings.TrimSpace(args["id"])
		if id == "" {
			return connectors.Result{}, fmt.Errorf("%w: id is required", connectors.ErrBadArgs)
		}
		o, err := one(ctx, in, a, id)
		if err != nil {
			return connectors.Result{}, err
		}
		text := "One meeting."
		if a.link != "" {
			text += " This calendar is read only (an .ics address): no action can change it."
		}
		return connectors.Result{Text: text, Data: whole(o, a.loc), From: from}, nil
	}
}

// one finds a meeting by its key: a CalDAV object read at its href, or an event of the .ics file.
func one(ctx context.Context, in connectors.Instance, a *account, id string) (occurrence, error) {
	obj, rid, _ := strings.Cut(id, "#")
	var list []occurrence
	from, to := time.Unix(0, 0), time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	if rid != "" {
		t, err := time.Parse("20060102T150405Z", rid)
		if err != nil {
			t, err = time.ParseInLocation("20060102", rid, a.loc)
		}
		if err != nil {
			return occurrence{}, connectors.ErrNotFound
		}
		// Around the time the rule put it at: an exception moved at most a few weeks away is still found.
		from, to = t.AddDate(0, 0, -45), t.AddDate(0, 0, 45)
	}
	if a.link != "" {
		f, err := fetchICS(ctx, in.HTTP, a.link, "", "")
		if err != nil {
			return occurrence{}, err
		}
		if rid == "" {
			from, to = clock().AddDate(-1, 0, 0), clock().AddDate(1, 0, 0)
		}
		list, err = expand(f.data, source{Calendar: f.name}, from, to, a.loc, nil, true)
		if err != nil {
			return occurrence{}, err
		}
	} else {
		if !a.owns(obj) {
			return occurrence{}, connectors.ErrNotFound
		}
		o, err := a.dav.get(ctx, obj)
		if err != nil {
			if statusOf(err) == 404 {
				return occurrence{}, connectors.ErrNotFound
			}
			return occurrence{}, say(err)
		}
		if rid == "" {
			from, to = clock().AddDate(-2, 0, 0), clock().AddDate(2, 0, 0)
		}
		list, err = expand(o.Data, source{Object: obj, Calendar: a.calendarOf(obj).Name}, from, to, a.loc, a.me, true)
		if err != nil {
			return occurrence{}, say(err)
		}
	}
	for _, o := range list {
		if o.Key == id {
			return o, nil
		}
	}
	// A whole series asked by its object: its next time, else its first.
	if rid == "" {
		var best *occurrence
		for i, o := range list {
			if o.Object != obj {
				continue
			}
			if best == nil || (best.End.Before(clock()) && o.Start.After(best.Start)) {
				best = &list[i]
			}
			if !o.End.Before(clock()) {
				break
			}
		}
		if best != nil {
			return *best, nil
		}
	}
	return occurrence{}, connectors.ErrNotFound
}

// owns says an href is inside one of the connection's calendars: an action never reaches another collection.
func (a *account) owns(href string) bool {
	return a.calendarOf(href).Href != ""
}

// An object's href must be a plain path right inside the calendar (decoded too): no "..", no query, no deeper path, so
// an id cannot walk out of the connected calendars.
func (a *account) calendarOf(href string) calendar {
	plain, err := url.PathUnescape(href)
	if err != nil || !strings.HasPrefix(href, "/") || strings.ContainsAny(plain, "?#\\") || path.Clean(plain) != plain || path.Clean(href) != href {
		return calendar{}
	}
	for _, c := range a.cals {
		rest, ok := strings.CutPrefix(plain, strings.TrimSuffix(c.Href, "/")+"/")
		if ok && rest != "" && !strings.Contains(rest, "/") && rest != "." && rest != ".." {
			return c
		}
	}
	return calendar{}
}

func when1(o occurrence, loc *time.Location) string {
	if o.AllDay {
		last := o.End.AddDate(0, 0, -1)
		if !last.After(o.Start) {
			return o.Start.Format("Mon 2006-01-02") + ", all day"
		}
		return o.Start.Format("Mon 2006-01-02") + " – " + last.Format("Mon 2006-01-02") + ", all day"
	}
	s, e := o.Start.In(loc), o.End.In(loc)
	if s.Format(time.DateOnly) == e.Format(time.DateOnly) || !e.After(s) {
		return s.Format("Mon 2006-01-02 15:04") + "–" + e.Format("15:04")
	}
	return s.Format("Mon 2006-01-02 15:04") + " – " + e.Format("Mon 2006-01-02 15:04")
}

// line is one meeting in a listing.
func line(o occurrence, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(when1(o, loc) + " · " + oneLine(o.Title))
	if o.Location != "" {
		b.WriteString(" · " + oneLine(o.Location))
	}
	if o.Call != "" && !strings.Contains(o.Location, o.Call) {
		b.WriteString(" · " + o.Call)
	}
	if n := len(o.Attendees); n > 0 {
		b.WriteString(fmt.Sprintf(" · %d people", n))
	}
	if o.cancelled() {
		b.WriteString(" · CANCELLED")
	}
	if o.Me != "" && o.Me != "ACCEPTED" {
		b.WriteString(" · my answer: " + strings.ToLower(o.Me))
	}
	if o.Recurring {
		b.WriteString(" · repeats")
	}
	b.WriteString(" · id=" + o.Key + "\n")
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func who(p person) string {
	switch {
	case p.Name != "" && p.Email != "":
		return p.Name + " <" + p.Email + ">"
	case p.Name != "":
		return p.Name
	default:
		return p.Email
	}
}

// whole is one meeting with everything the calendar says of it.
func whole(o occurrence, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nWhen: %s (%s)\n", oneLine(o.Title), when1(o, loc), loc)
	if o.Calendar != "" {
		fmt.Fprintf(&b, "Calendar: %s\n", o.Calendar)
	}
	if o.Location != "" {
		fmt.Fprintf(&b, "Place: %s\n", oneLine(o.Location))
	}
	if o.Call != "" {
		fmt.Fprintf(&b, "Call: %s\n", o.Call)
	}
	if o.Status != "" {
		fmt.Fprintf(&b, "Status: %s\n", strings.ToLower(o.Status))
	}
	if o.Recurring {
		b.WriteString("Repeats: yes\n")
	}
	if o.Organizer.Email != "" || o.Organizer.Name != "" {
		fmt.Fprintf(&b, "Organizer: %s", who(o.Organizer))
		if o.MeOrganizer {
			b.WriteString(" (me)")
		}
		b.WriteString("\n")
	}
	if o.Me != "" {
		fmt.Fprintf(&b, "My answer: %s\n", strings.ToLower(o.Me))
	}
	if len(o.Attendees) > 0 {
		fmt.Fprintf(&b, "Attendees (%d):\n", len(o.Attendees))
		for _, p := range o.Attendees {
			st := strings.ToLower(p.Status)
			if st == "" {
				st = "needs-action"
			}
			fmt.Fprintf(&b, "- %s: %s\n", who(p), st)
		}
	}
	fmt.Fprintf(&b, "id=%s\n", o.Key)
	if d := strings.TrimSpace(o.Description); d != "" {
		if r := []rune(d); len(r) > 8000 {
			d = string(r[:8000]) + "…"
		}
		b.WriteString("\nDescription:\n" + d + "\n")
	}
	return b.String()
}

// freeText is the free windows of each day within the working hours, past the busy meetings (not the cancelled, the
// declined, the ones marked free or the whole-day ones) and past now.
func freeText(list []occurrence, start time.Time, days, dayFrom, dayTo int, least time.Duration, now time.Time, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Free windows of at least %d min, %02d:%02d–%02d:%02d, %s:\n", int(least/time.Minute), dayFrom/60, dayFrom%60, dayTo/60, dayTo%60, loc)
	for i := range days {
		d := start.AddDate(0, 0, i)
		open := time.Date(d.Year(), d.Month(), d.Day(), dayFrom/60, dayFrom%60, 0, 0, loc)
		shut := time.Date(d.Year(), d.Month(), d.Day(), dayTo/60, dayTo%60, 0, 0, loc)
		if open.Before(now) {
			open = now.Truncate(5 * time.Minute)
			if open.Before(now) {
				open = open.Add(5 * time.Minute)
			}
		}
		var windows []string
		cursor := open
		busy := busyOf(list, open, shut)
		for _, iv := range busy {
			if iv[0].Sub(cursor) >= least {
				windows = append(windows, cursor.Format("15:04")+"–"+iv[0].Format("15:04"))
			}
			if iv[1].After(cursor) {
				cursor = iv[1]
			}
		}
		if shut.Sub(cursor) >= least {
			windows = append(windows, cursor.Format("15:04")+"–"+shut.Format("15:04"))
		}
		label := d.Format("Mon 2006-01-02")
		switch {
		case !shut.After(open):
			fmt.Fprintf(&b, "%s: over\n", label)
		case len(windows) == 0:
			fmt.Fprintf(&b, "%s: no free window\n", label)
		default:
			fmt.Fprintf(&b, "%s: %s\n", label, strings.Join(windows, ", "))
		}
	}
	return b.String()
}

// busyOf is the busy time between open and shut, merged and in order.
func busyOf(list []occurrence, open, shut time.Time) [][2]time.Time {
	var out [][2]time.Time
	for _, o := range list {
		if !o.busy(false) {
			continue
		}
		s, e := maxTime(o.Start, open), o.End
		if e.After(shut) {
			e = shut
		}
		if !e.After(s) {
			continue
		}
		out = append(out, [2]time.Time{s, e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0].Before(out[j][0]) })
	var merged [][2]time.Time
	for _, iv := range out {
		if n := len(merged); n > 0 && !iv[0].After(merged[n-1][1]) {
			if iv[1].After(merged[n-1][1]) {
				merged[n-1][1] = iv[1]
			}
			continue
		}
		merged = append(merged, iv)
	}
	return merged
}
