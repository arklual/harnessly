package caldav

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// The actions. Each runs only after the person's yes on the card, once, with exactly the args shown. A change is
// written with If-Match on the ETag read just before: a meeting someone changed in between is not overwritten. With
// attendees, the server's scheduling (RFC 6638) sends the invitations, the changes and the answers itself.

func (calConn) Act(ctx context.Context, in connectors.Instance, action string, args map[string]string) (connectors.Result, error) {
	if readOnly(in) {
		return connectors.Result{}, calConn{}.CanAct(in, action)
	}
	a, _, err := login(ctx, in)
	if err != nil {
		return connectors.Result{}, err
	}
	switch action {
	case "create":
		return a.create(ctx, args)
	case "move", "cancel", "reply":
		return a.change(ctx, action, args)
	}
	return connectors.Result{}, connectors.ErrUnknownTool
}

// timeArg reads "YYYY-MM-DDTHH:MM" (or with seconds, or RFC 3339) in loc, or "YYYY-MM-DD" for a whole day.
func timeArg(v string, loc *time.Location) (time.Time, bool, error) {
	v = strings.TrimSpace(strings.Replace(v, " ", "T", 1))
	if t, err := time.ParseInLocation(time.DateOnly, v, loc); err == nil {
		return t, true, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.In(loc), false, nil
	}
	return time.Time{}, false, fmt.Errorf("%w: a time is YYYY-MM-DDTHH:MM (or YYYY-MM-DD for a whole day)", connectors.ErrBadArgs)
}

func setTime(comp *ical.Component, name string, t time.Time, allDay bool, loc *time.Location) {
	p := ical.NewProp(name)
	switch {
	case allDay:
		p.SetDate(t)
	case loc == time.UTC || loc.String() == "Local" || loc.String() == "":
		p.SetDateTime(t.UTC())
	default:
		p.SetDateTime(t.In(loc))
	}
	comp.Props.Set(p)
}

func newUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *account) writable(name string) (calendar, error) {
	for _, c := range a.cals {
		if name != "" && !strings.EqualFold(c.Name, name) {
			continue
		}
		if c.ReadOnly {
			if name != "" {
				return calendar{}, connectors.Sayf("The calendar %q is read only", name)
			}
			continue
		}
		return c, nil
	}
	if name != "" {
		return calendar{}, connectors.Sayf("No connected calendar is called %q", name)
	}
	return calendar{}, connectors.Sayf("Every connected calendar is read only")
}

func (a *account) myAddress() string {
	for e := range a.me {
		return e
	}
	return ""
}

func (a *account) create(ctx context.Context, args map[string]string) (connectors.Result, error) {
	cal, err := a.writable(strings.TrimSpace(args["calendar"]))
	if err != nil {
		return connectors.Result{}, err
	}
	start, allDay, err := timeArg(args["start"], a.loc)
	if err != nil {
		return connectors.Result{}, err
	}
	var end time.Time
	switch {
	case strings.TrimSpace(args["end"]) != "":
		if end, _, err = timeArg(args["end"], a.loc); err != nil {
			return connectors.Result{}, err
		}
		if allDay && !end.After(start) {
			end = start.AddDate(0, 0, 1)
		}
	case allDay:
		end = start.AddDate(0, 0, 1)
	default:
		mins := 30
		if v := strings.TrimSpace(args["minutes"]); v != "" {
			if mins, err = strconv.Atoi(v); err != nil || mins < 1 || mins > 24*60 {
				return connectors.Result{}, fmt.Errorf("%w: minutes is a number up to a day", connectors.ErrBadArgs)
			}
		}
		end = start.Add(time.Duration(mins) * time.Minute)
	}
	if !end.After(start) {
		return connectors.Result{}, fmt.Errorf("%w: the end is before the start", connectors.ErrBadArgs)
	}
	uid := newUID()
	ev := ical.NewEvent()
	ev.Props.SetText(ical.PropUID, uid)
	ev.Props.SetDateTime(ical.PropDateTimeStamp, clock().UTC())
	ev.Props.SetDateTime(ical.PropCreated, clock().UTC())
	ev.Props.SetText(ical.PropSummary, strings.TrimSpace(args["title"]))
	setTime(ev.Component, ical.PropDateTimeStart, start, allDay, a.loc)
	setTime(ev.Component, ical.PropDateTimeEnd, end, allDay, a.loc)
	if v := strings.TrimSpace(args["location"]); v != "" {
		ev.Props.SetText(ical.PropLocation, v)
	}
	if v := strings.TrimSpace(args["description"]); v != "" {
		ev.Props.SetText(ical.PropDescription, v)
	}
	var invited []string
	if v := strings.TrimSpace(args["attendees"]); v != "" {
		list, err := mail.ParseAddressList(v)
		if err != nil {
			return connectors.Result{}, fmt.Errorf("%w: attendees are e-mail addresses, comma-separated", connectors.ErrBadArgs)
		}
		me := a.myAddress()
		if me == "" {
			return connectors.Result{}, connectors.Sayf("The account says no e-mail of its own: invitations cannot be sent from it")
		}
		org := ical.NewProp(ical.PropOrganizer)
		org.Value = "mailto:" + me
		ev.Props.Set(org)
		self := ical.NewProp(ical.PropAttendee)
		self.Value = "mailto:" + me
		self.Params.Set("PARTSTAT", "ACCEPTED")
		self.Params.Set("ROLE", "CHAIR")
		ev.Props.Add(self)
		for _, addr := range list {
			p := ical.NewProp(ical.PropAttendee)
			p.Value = "mailto:" + strings.ToLower(addr.Address)
			if addr.Name != "" {
				p.Params.Set("CN", addr.Name)
			}
			p.Params.Set("PARTSTAT", "NEEDS-ACTION")
			p.Params.Set("RSVP", "TRUE")
			p.Params.Set("ROLE", "REQ-PARTICIPANT")
			ev.Props.Add(p)
			invited = append(invited, addr.Address)
		}
	}
	data, err := encode([]*ical.Component{ev.Component}, a.loc)
	if err != nil {
		return connectors.Result{}, err
	}
	href := strings.TrimSuffix(cal.Href, "/") + "/" + uid + ".ics"
	if err := a.dav.put(ctx, href, "", data); err != nil {
		return connectors.Result{}, say(err)
	}
	text := fmt.Sprintf("Created in %q: %s. id=%s", cal.Name, when1(occurrence{Start: start, End: end, AllDay: allDay}, a.loc), href)
	if len(invited) > 0 {
		text += fmt.Sprintf(". The server sends the invitations to %d people", len(invited))
	}
	return connectors.Result{Text: text}, nil
}

// encode writes the components of one object as a VCALENDAR.
func encode(comps []*ical.Component, loc *time.Location) ([]byte, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//harnessly//calendar//EN")
	cal.Children = comps
	var b strings.Builder
	if err := ical.NewEncoder(&b).Encode(cal); err != nil {
		return nil, fmt.Errorf("the meeting cannot be written: %w", err)
	}
	return []byte(b.String()), nil
}

// bump moves SEQUENCE and DTSTAMP: the attendees' calendars take the change as newer.
func bump(comp *ical.Component) {
	seq := 0
	if p := comp.Props.Get(ical.PropSequence); p != nil {
		seq, _ = strconv.Atoi(strings.TrimSpace(p.Value))
	}
	p := ical.NewProp(ical.PropSequence)
	p.Value = strconv.Itoa(seq + 1)
	comp.Props.Set(p)
	comp.Props.SetDateTime(ical.PropDateTimeStamp, clock().UTC())
}

// split is the object's components: the series (or the single meeting) and its exceptions by recurrence key.
func split(data string) ([]*ical.Component, error) {
	dec := ical.NewDecoder(strings.NewReader(data))
	cal, err := dec.Decode()
	if err != nil {
		return nil, connectors.Sayf("The meeting on the server is not iCalendar")
	}
	return cal.Children, nil
}

// exception is the component of one time of a series: the existing exception, or a new one copied from the series.
func exception(comps []*ical.Component, master *ical.Component, rid string, at time.Time, allDay bool, loc *time.Location) ([]*ical.Component, *ical.Component) {
	for _, c := range comps {
		if c.Name != ical.CompEvent {
			continue
		}
		p := c.Props.Get(ical.PropRecurrenceID)
		if p == nil {
			continue
		}
		if ts, date, err := when(p, loc); err == nil && ridKey(ts[0], date) == rid {
			return comps, c
		}
	}
	ov := ical.NewComponent(ical.CompEvent)
	for name, props := range master.Props {
		switch name {
		case ical.PropRecurrenceRule, ical.PropRecurrenceDates, ical.PropExceptionDates, ical.PropDateTimeStart, ical.PropDateTimeEnd, ical.PropDuration:
			continue
		}
		ov.Props[name] = append([]ical.Prop(nil), props...)
	}
	start := master.Props.Get(ical.PropDateTimeStart)
	r := ical.NewProp(ical.PropRecurrenceID)
	switch {
	case allDay:
		r.SetDate(at)
	case start != nil && start.Params.Get(ical.PropTimezoneID) != "":
		r.Params.Set(ical.PropTimezoneID, start.Params.Get(ical.PropTimezoneID))
		r.Value = at.In(zone(start.Params.Get(ical.PropTimezoneID), loc)).Format("20060102T150405")
	default:
		r.SetDateTime(at.UTC())
	}
	ov.Props.Set(r)
	return append(comps, ov), ov
}

// change runs move, cancel and reply on a meeting read anew from the server.
func (a *account) change(ctx context.Context, action string, args map[string]string) (connectors.Result, error) {
	id := strings.TrimSpace(args["id"])
	obj, rid, _ := strings.Cut(id, "#")
	if !a.owns(obj) {
		return connectors.Result{}, connectors.ErrNotFound
	}
	if a.calendarOf(obj).ReadOnly {
		return connectors.Result{}, connectors.Sayf("This calendar is read only")
	}
	occ, err := one(ctx, connectors.Instance{}, a, id)
	if err != nil {
		return connectors.Result{}, err
	}
	o, err := a.dav.get(ctx, obj)
	if err != nil {
		return connectors.Result{}, say(err)
	}
	comps, err := split(o.Data)
	if err != nil {
		return connectors.Result{}, err
	}
	var master *ical.Component
	for _, c := range comps {
		if c.Name == ical.CompEvent && c.Props.Get(ical.PropRecurrenceID) == nil {
			master = c
			break
		}
	}
	// target is the component the change is written to: the single meeting, the series, or one time's exception.
	target := master
	var ridTime time.Time
	if rid != "" {
		if t, err := time.Parse("20060102T150405Z", rid); err == nil {
			ridTime = t
		} else if t, err := time.ParseInLocation("20060102", rid, a.loc); err == nil {
			ridTime = t
		}
		if master == nil {
			// Only exceptions on the server: the one of this time.
			for _, c := range comps {
				if p := c.Props.Get(ical.PropRecurrenceID); p != nil {
					if ts, date, err := when(p, a.loc); err == nil && ridKey(ts[0], date) == rid {
						target = c
					}
				}
			}
		} else if action != "cancel" {
			comps, target = exception(comps, master, rid, ridTime, occ.AllDay, a.loc)
		}
	}
	if target == nil {
		return connectors.Result{}, connectors.ErrNotFound
	}
	var text string
	switch action {
	case "move":
		if !occ.MeOrganizer {
			return connectors.Result{}, connectors.Sayf("Only the organizer moves a meeting; %s organizes this one. Answer it instead, or write to them", who(occ.Organizer))
		}
		start, allDay, err := timeArg(args["start"], a.loc)
		if err != nil {
			return connectors.Result{}, err
		}
		var end time.Time
		if v := strings.TrimSpace(args["end"]); v != "" {
			if end, _, err = timeArg(v, a.loc); err != nil {
				return connectors.Result{}, err
			}
		} else if allDay && occ.AllDay {
			end = start.AddDate(0, 0, max(int(occ.End.Sub(occ.Start).Round(24*time.Hour)/(24*time.Hour)), 1))
		} else {
			end = start.Add(occ.End.Sub(occ.Start))
		}
		if !end.After(start) {
			return connectors.Result{}, fmt.Errorf("%w: the end is before the start", connectors.ErrBadArgs)
		}
		zoneOf := a.loc
		if p := target.Props.Get(ical.PropDateTimeStart); p != nil && p.Params.Get(ical.PropTimezoneID) != "" {
			zoneOf = zone(p.Params.Get(ical.PropTimezoneID), a.loc)
		}
		setTime(target, ical.PropDateTimeStart, start, allDay, zoneOf)
		target.Props.Del(ical.PropDuration)
		setTime(target, ical.PropDateTimeEnd, end, allDay, zoneOf)
		bump(target)
		text = fmt.Sprintf("Moved to %s", when1(occurrence{Start: start, End: end, AllDay: allDay}, a.loc))
	case "cancel":
		if !occ.MeOrganizer {
			return connectors.Result{}, connectors.Sayf("Only the organizer cancels a meeting; %s organizes this one. Answer it with reply answer=declined", who(occ.Organizer))
		}
		if rid == "" || master == nil {
			if err := a.dav.remove(ctx, obj, o.ETag); err != nil {
				return connectors.Result{}, say(err)
			}
			return connectors.Result{Text: "Cancelled; the server tells the attendees"}, nil
		}
		// One time of a series: the series loses that time (EXDATE), its exception goes.
		ex := ical.NewProp(ical.PropExceptionDates)
		if start := master.Props.Get(ical.PropDateTimeStart); occ.AllDay {
			ex.SetDate(ridTime.In(a.loc))
		} else if start != nil && start.Params.Get(ical.PropTimezoneID) != "" {
			ex.Params.Set(ical.PropTimezoneID, start.Params.Get(ical.PropTimezoneID))
			ex.Value = ridTime.In(zone(start.Params.Get(ical.PropTimezoneID), a.loc)).Format("20060102T150405")
		} else {
			ex.SetDateTime(ridTime.UTC())
		}
		master.Props.Add(ex)
		bump(master)
		var kept []*ical.Component
		for _, c := range comps {
			if p := c.Props.Get(ical.PropRecurrenceID); p != nil {
				if ts, date, err := when(p, a.loc); err == nil && ridKey(ts[0], date) == rid {
					continue
				}
			}
			kept = append(kept, c)
		}
		comps = kept
		text = "This time of the series is cancelled; the server tells the attendees"
	case "reply":
		answer := strings.ToUpper(strings.TrimSpace(args["answer"]))
		switch answer {
		case "ACCEPTED", "DECLINED", "TENTATIVE":
		case "ACCEPT", "YES":
			answer = "ACCEPTED"
		case "DECLINE", "NO":
			answer = "DECLINED"
		default:
			return connectors.Result{}, fmt.Errorf("%w: answer is accepted, declined or tentative", connectors.ErrBadArgs)
		}
		found := false
		props := target.Props[ical.PropAttendee]
		for i := range props {
			if e := personOf(props[i]).Email; e != "" && a.me[e] {
				props[i].Params.Set("PARTSTAT", answer)
				props[i].Params.Del("RSVP")
				found = true
			}
		}
		if !found {
			return connectors.Result{}, connectors.Sayf("The person is not among the attendees of this meeting")
		}
		target.Props[ical.PropAttendee] = props
		target.Props.SetDateTime(ical.PropDateTimeStamp, clock().UTC())
		text = "Answered " + strings.ToLower(answer) + "; the server tells the organizer"
	}
	data, err := encode(comps, a.loc)
	if err != nil {
		return connectors.Result{}, err
	}
	if err := a.dav.put(ctx, obj, o.ETag, data); err != nil {
		return connectors.Result{}, say(err)
	}
	return connectors.Result{Text: text + ". id=" + id}, nil
}
