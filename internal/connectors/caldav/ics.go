package caldav

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"

	_ "time/tzdata" // the zones of TZID must load on a machine without zone files
)

// occurrence is one meeting at one time: a single event, or one time of a repeating one (with its exception applied).
type occurrence struct {
	// Key names it for the reads and the actions: "<object>" or "<object>#<recurrence id>" (the time the rule put it
	// at, UTC), where object is the href on a CalDAV server or "uid:<UID>" in an .ics file.
	Key       string
	Object    string
	UID       string
	Calendar  string
	Start     time.Time
	End       time.Time
	AllDay    bool
	Recurring bool
	Title     string
	Location  string
	Call      string
	Status    string // CONFIRMED, TENTATIVE, CANCELLED
	Free      bool   // TRANSP:TRANSPARENT: does not make the person busy
	Organizer person
	Attendees []person
	// Me is the person's own answer when they are an attendee: NEEDS-ACTION, ACCEPTED, DECLINED, TENTATIVE.
	Me          string
	MeOrganizer bool
	Description string
}

type person struct {
	Name   string
	Email  string
	Status string
}

func (o occurrence) cancelled() bool { return o.Status == "CANCELLED" }
func (o occurrence) declined() bool  { return o.Me == "DECLINED" }

// busy says the occurrence takes the person's time: not cancelled, not declined, not marked free, and not a whole day
// unless it says it is busy.
func (o occurrence) busy(opaqueAllDay bool) bool {
	if o.cancelled() || o.declined() || o.Free {
		return false
	}
	return !o.AllDay || opaqueAllDay
}

// windowsZones are the zone names Outlook and Exchange write in TZID, for the ones people here meet most.
var windowsZones = map[string]string{
	"Russian Standard Time":          "Europe/Moscow",
	"Russia Time Zone 3":             "Europe/Samara",
	"Ekaterinburg Standard Time":     "Asia/Yekaterinburg",
	"N. Central Asia Standard Time":  "Asia/Novosibirsk",
	"North Asia Standard Time":       "Asia/Krasnoyarsk",
	"North Asia East Standard Time":  "Asia/Irkutsk",
	"Kaliningrad Standard Time":      "Europe/Kaliningrad",
	"W. Europe Standard Time":        "Europe/Berlin",
	"Central Europe Standard Time":   "Europe/Budapest",
	"Romance Standard Time":          "Europe/Paris",
	"GMT Standard Time":              "Europe/London",
	"FLE Standard Time":              "Europe/Kiev",
	"GTB Standard Time":              "Europe/Bucharest",
	"Turkey Standard Time":           "Europe/Istanbul",
	"Arabian Standard Time":          "Asia/Dubai",
	"Georgian Standard Time":         "Asia/Tbilisi",
	"Caucasus Standard Time":         "Asia/Yerevan",
	"Central Asia Standard Time":     "Asia/Almaty",
	"Israel Standard Time":           "Asia/Jerusalem",
	"India Standard Time":            "Asia/Kolkata",
	"China Standard Time":            "Asia/Shanghai",
	"Tokyo Standard Time":            "Asia/Tokyo",
	"Singapore Standard Time":        "Asia/Singapore",
	"Eastern Standard Time":          "America/New_York",
	"Central Standard Time":          "America/Chicago",
	"Mountain Standard Time":         "America/Denver",
	"Pacific Standard Time":          "America/Los_Angeles",
	"UTC":                            "UTC",
	"Coordinated Universal Time":     "UTC",
	"Greenwich Standard Time":        "Atlantic/Reykjavik",
	"E. Europe Standard Time":        "Europe/Chisinau",
	"Belarus Standard Time":          "Europe/Minsk",
	"Volgograd Standard Time":        "Europe/Volgograd",
	"Astrakhan Standard Time":        "Europe/Astrakhan",
	"Saratov Standard Time":          "Europe/Saratov",
	"Omsk Standard Time":             "Asia/Omsk",
	"Yakutsk Standard Time":          "Asia/Yakutsk",
	"Vladivostok Standard Time":      "Asia/Vladivostok",
	"Magadan Standard Time":          "Asia/Magadan",
	"Russia Time Zone 10":            "Asia/Srednekolymsk",
	"Russia Time Zone 11":            "Asia/Kamchatka",
	"West Asia Standard Time":        "Asia/Tashkent",
	"Azerbaijan Standard Time":       "Asia/Baku",
	"SE Asia Standard Time":          "Asia/Bangkok",
	"Korea Standard Time":            "Asia/Seoul",
	"AUS Eastern Standard Time":      "Australia/Sydney",
	"E. South America Standard Time": "America/Sao_Paulo",
}

// zone is the location of a TZID: an IANA name, a Windows name, or a prefixed one ("/mozilla.org/…/Europe/Berlin");
// an unknown one is the person's zone.
func zone(tzid string, fallback *time.Location) *time.Location {
	tzid = strings.Trim(strings.TrimSpace(tzid), `"`)
	if tzid == "" {
		return fallback
	}
	if name, ok := windowsZones[tzid]; ok {
		tzid = name
	}
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc
	}
	parts := strings.Split(strings.Trim(tzid, "/"), "/")
	for i := range parts {
		if loc, err := time.LoadLocation(strings.Join(parts[i:], "/")); err == nil {
			return loc
		}
	}
	return fallback
}

// when reads a DTSTART-like value: a date (a whole day, at midnight in loc), a UTC time, a time in its TZID, or a
// floating time in loc. A list (EXDATE:a,b) gives every value.
func when(p *ical.Prop, loc *time.Location) ([]time.Time, bool, error) {
	var out []time.Time
	date := strings.EqualFold(p.Params.Get(ical.ParamValue), "DATE")
	tz := zone(p.Params.Get(ical.PropTimezoneID), loc)
	for _, v := range strings.Split(p.Value, ",") {
		v = strings.TrimSpace(v)
		switch {
		case date || len(v) == 8:
			date = true
			t, err := time.ParseInLocation("20060102", v, loc)
			if err != nil {
				return nil, false, err
			}
			out = append(out, t)
		case strings.HasSuffix(v, "Z"):
			t, err := time.ParseInLocation("20060102T150405Z", v, time.UTC)
			if err != nil {
				return nil, false, err
			}
			out = append(out, t)
		default:
			t, err := time.ParseInLocation("20060102T150405", v, tz)
			if err != nil {
				return nil, false, err
			}
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, false, errors.New("no time")
	}
	return out, date, nil
}

func first(comp *ical.Component, name string, loc *time.Location) (time.Time, bool, error) {
	p := comp.Props.Get(name)
	if p == nil {
		return time.Time{}, false, errors.New("no " + name)
	}
	ts, date, err := when(p, loc)
	if err != nil {
		return time.Time{}, false, err
	}
	return ts[0], date, nil
}

func text(comp *ical.Component, name string) string {
	p := comp.Props.Get(name)
	if p == nil {
		return ""
	}
	s, err := p.Text()
	if err != nil {
		return p.Value
	}
	return strings.TrimSpace(s)
}

// ridKey is the stable name of a time the rule put an occurrence at.
func ridKey(t time.Time, allDay bool) string {
	if allDay {
		return t.Format("20060102")
	}
	return t.UTC().Format("20060102T150405Z")
}

// calls are the hosts of video calls, by the word a rule may filter on.
var calls = []struct{ host, name string }{
	{"telemost.yandex.", "telemost"},
	{"telemost.360.yandex.", "telemost"},
	{"meet.google.com", "meet"},
	{"zoom.us", "zoom"},
	{"zoom.com", "zoom"},
	{"teams.microsoft.com", "teams"},
	{"teams.live.com", "teams"},
	{"meet.jit.si", "jitsi"},
	{"webex.com", "webex"},
	{"ktalk.ru", "ktalk"},
	{"salutejazz.ru", "jazz"},
	{"jazz.sber.ru", "jazz"},
	{"whereby.com", "whereby"},
}

var linkRe = regexp.MustCompile(`https://[^\s<>"'\\)\]]+`)

// callOf finds the link of a video call in the fields where calendars put it.
func callOf(comp *ical.Component, location, description string) string {
	var hay []string
	for _, name := range []string{"CONFERENCE", "X-GOOGLE-CONFERENCE", "X-MICROSOFT-SKYPETEAMSMEETINGURL", "URL"} {
		for _, p := range comp.Props.Values(name) {
			hay = append(hay, p.Value)
		}
	}
	hay = append(hay, location, description)
	for _, h := range hay {
		for _, link := range linkRe.FindAllString(h, -1) {
			if callName(link) != "" {
				return link
			}
		}
	}
	return ""
}

// callName is the word for a call link's service: "zoom", "meet", "telemost"; "" when it is not a call.
func callName(link string) string {
	l := strings.ToLower(link)
	host := strings.TrimPrefix(l, "https://")
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	for _, c := range calls {
		if strings.Contains(host, c.host) {
			return c.name
		}
	}
	return ""
}

func personOf(p ical.Prop) person {
	email := strings.TrimSpace(p.Value)
	if strings.HasPrefix(strings.ToLower(email), "mailto:") {
		email = email[len("mailto:"):]
	} else {
		email = ""
	}
	name := strings.Trim(strings.TrimSpace(p.Params.Get("CN")), `"`)
	if a, err := mail.ParseAddress(name); err == nil && a.Name == "" {
		name = "" // a CN that is only an address is no name
	}
	if strings.Contains(name, "@") {
		name = ""
	}
	return person{Name: name, Email: strings.ToLower(email), Status: strings.ToUpper(p.Params.Get("PARTSTAT"))}
}

// parse decodes iCalendar text into its VEVENTs grouped by UID (a repeating meeting and its exceptions travel
// together), in the order met.
func parse(data string) (map[string][]*ical.Component, []string, error) {
	dec := ical.NewDecoder(strings.NewReader(data))
	byUID := map[string][]*ical.Component{}
	var order []string
	for {
		cal, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if len(order) > 0 {
				break
			}
			return nil, nil, err
		}
		for _, child := range cal.Children {
			if child.Name != ical.CompEvent {
				continue
			}
			uid := text(child, ical.PropUID)
			if uid == "" {
				sum := sha256.Sum256([]byte(text(child, ical.PropSummary) + child.Props.Get(ical.PropDateTimeStart).Value))
				uid = "nouid-" + hex.EncodeToString(sum[:8])
			}
			if _, seen := byUID[uid]; !seen {
				order = append(order, uid)
			}
			byUID[uid] = append(byUID[uid], child)
		}
	}
	return byUID, order, nil
}

// span is how long an event lasts: DTEND minus DTSTART, else DURATION, else a day for a date and nothing for a time.
func span(comp *ical.Component, start time.Time, allDay bool, loc *time.Location) (time.Duration, int) {
	if end, _, err := first(comp, ical.PropDateTimeEnd, loc); err == nil && end.After(start) {
		if allDay {
			// Whole days count in days, not hours: the day of a clock change has 23 or 25 hours.
			days := int(end.Sub(start).Round(24*time.Hour) / (24 * time.Hour))
			return 0, max(days, 1)
		}
		return end.Sub(start), 0
	}
	if p := comp.Props.Get(ical.PropDuration); p != nil {
		if d, err := p.Duration(); err == nil && d > 0 {
			if allDay {
				return 0, max(int(d/(24*time.Hour)), 1)
			}
			return d, 0
		}
	}
	if allDay {
		return 0, 1
	}
	return 0, 0
}

func endOf(start time.Time, d time.Duration, days int) time.Time {
	if days > 0 {
		return start.AddDate(0, 0, days)
	}
	return start.Add(d)
}

// source is where an object came from, for the keys and the words of the reads.
type source struct {
	Object   string // href, or "" in an .ics file (the key is then "uid:<UID>")
	Calendar string
}

// expand turns the events of one object (or of a whole .ics file) into occurrences that touch [from, to): single ones
// as they are, repeating ones by their rule with EXDATE, RDATE and the exceptions (RECURRENCE-ID) applied. me are the
// person's addresses, to find their own answer.
func expand(data string, src source, from, to time.Time, loc *time.Location, me map[string]bool, withText bool) ([]occurrence, error) {
	byUID, order, err := parse(data)
	if err != nil {
		return nil, err
	}
	var out []occurrence
	for _, uid := range order {
		var master *ical.Component
		overrides := map[string]*ical.Component{}
		for _, comp := range byUID[uid] {
			rid := comp.Props.Get(ical.PropRecurrenceID)
			if rid == nil {
				if master == nil {
					master = comp
				}
				continue
			}
			ts, date, err := when(rid, loc)
			if err != nil {
				continue
			}
			overrides[ridKey(ts[0], date)] = comp
		}
		object := src.Object
		if object == "" {
			object = "uid:" + uid
		}
		make1 := func(comp *ical.Component, start time.Time, allDay bool, d time.Duration, days int, key string, recurring bool) {
			end := endOf(start, d, days)
			if !end.After(from) && !(end.Equal(start) && start.Equal(from)) {
				return
			}
			if !start.Before(to) {
				return
			}
			out = append(out, occurrenceOf(comp, src, uid, object, key, start, end, allDay, recurring, me, withText, loc))
		}
		if master == nil {
			// Only exceptions of a series we cannot see (an invitation to one time): each is a meeting of its own.
			for rk, comp := range overrides {
				start, allDay, err := first(comp, ical.PropDateTimeStart, loc)
				if err != nil {
					continue
				}
				d, days := span(comp, start, allDay, loc)
				make1(comp, start, allDay, d, days, object+"#"+rk, true)
			}
			continue
		}
		start, allDay, err := first(master, ical.PropDateTimeStart, loc)
		if err != nil {
			continue
		}
		d, days := span(master, start, allDay, loc)
		rule := master.Props.Get(ical.PropRecurrenceRule)
		if rule == nil {
			make1(master, start, allDay, d, days, object, false)
			continue
		}
		set, err := ruleSet(master, rule.Value, start, loc)
		if err != nil {
			// A rule we cannot read: the first time is still a meeting.
			make1(master, start, allDay, d, days, object, false)
			continue
		}
		// Look back by the length of a meeting, so one that started before from and still goes is seen.
		back := d + time.Duration(days)*24*time.Hour
		seen := map[string]bool{}
		for _, t := range set.Between(from.Add(-back-time.Hour), to, true) {
			if allDay {
				t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			}
			rk := ridKey(t, allDay)
			seen[rk] = true
			if ov := overrides[rk]; ov != nil {
				os, oAllDay, err := first(ov, ical.PropDateTimeStart, loc)
				if err != nil {
					os, oAllDay = t, allDay
				}
				od, odays := span(ov, os, oAllDay, loc)
				if ov.Props.Get(ical.PropDateTimeEnd) == nil && ov.Props.Get(ical.PropDuration) == nil {
					od, odays = d, days
				}
				make1(ov, os, oAllDay, od, odays, object+"#"+rk, true)
				continue
			}
			make1(master, t, allDay, d, days, object+"#"+rk, true)
		}
		// An exception moved here from a time outside the range.
		for rk, ov := range overrides {
			if seen[rk] {
				continue
			}
			os, oAllDay, err := first(ov, ical.PropDateTimeStart, loc)
			if err != nil {
				continue
			}
			od, odays := span(ov, os, oAllDay, loc)
			make1(ov, os, oAllDay, od, odays, object+"#"+rk, true)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// ruleSet builds the times of a repeating event: RRULE from DTSTART in its own zone (so 10:00 stays 10:00 across a
// clock change), minus EXDATE, plus RDATE.
func ruleSet(master *ical.Component, rule string, start time.Time, loc *time.Location) (*rrule.Set, error) {
	opt, err := rrule.StrToROptionInLocation(rule, start.Location())
	if err != nil {
		return nil, err
	}
	opt.Dtstart = start
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, err
	}
	set := &rrule.Set{}
	set.RRule(r)
	for _, p := range master.Props.Values(ical.PropExceptionDates) {
		ts, _, err := when(&p, loc)
		if err != nil {
			continue
		}
		for _, t := range ts {
			set.ExDate(t)
		}
	}
	for _, p := range master.Props.Values(ical.PropRecurrenceDates) {
		ts, _, err := when(&p, loc)
		if err != nil {
			continue
		}
		for _, t := range ts {
			set.RDate(t)
		}
	}
	return set, nil
}

func occurrenceOf(comp *ical.Component, src source, uid, object, key string, start, end time.Time, allDay, recurring bool, me map[string]bool, withText bool, loc *time.Location) occurrence {
	o := occurrence{
		Key: key, Object: object, UID: uid, Calendar: src.Calendar,
		Start: start.In(loc), End: end.In(loc), AllDay: allDay, Recurring: recurring,
		Title:    text(comp, ical.PropSummary),
		Location: text(comp, ical.PropLocation),
		Status:   strings.ToUpper(text(comp, ical.PropStatus)),
		Free:     strings.EqualFold(text(comp, ical.PropTransparency), "TRANSPARENT"),
	}
	if allDay {
		o.Start, o.End = start, end
	}
	desc := text(comp, ical.PropDescription)
	o.Call = callOf(comp, o.Location, desc)
	if withText {
		o.Description = desc
	}
	if p := comp.Props.Get(ical.PropOrganizer); p != nil {
		o.Organizer = personOf(*p)
		o.MeOrganizer = o.Organizer.Email != "" && me[o.Organizer.Email]
	}
	for _, p := range comp.Props.Values(ical.PropAttendee) {
		a := personOf(p)
		if a.Email != "" && a.Email == o.Organizer.Email {
			continue
		}
		if a.Email != "" && me[a.Email] {
			o.Me = a.Status
			if o.Me == "" {
				o.Me = "NEEDS-ACTION"
			}
		}
		o.Attendees = append(o.Attendees, a)
	}
	if o.Organizer.Email == "" && len(o.Attendees) == 0 {
		o.MeOrganizer = true // a meeting of one's own, without invitations
	}
	return o
}

// fingerprint is a short hash of an object's text: the ETag of an event of an .ics file, which has none.
func fingerprint(comps []*ical.Component) string {
	h := sha256.New()
	for _, c := range comps {
		cal := ical.NewCalendar()
		cal.Props.SetText(ical.PropVersion, "2.0")
		cal.Props.SetText(ical.PropProductID, "-//harnessly//calendar//EN")
		cal.Children = append(cal.Children, c)
		_ = ical.NewEncoder(h).Encode(cal)
	}
	return hex.EncodeToString(h.Sum(nil)[:10])
}
