package analytics

import (
	"net/url"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

const (
	day     = "2006-01-02"
	maxDays = 400
	// defaultTZ is the zone of a person in no organization (the default of orgs.timezone).
	defaultTZ = "Europe/Moscow"
)

// Period is [from, to) as calendar days of a time zone.
type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
	TZ   string `json:"tz"`
}

// span is a period as instants.
type span struct {
	from, to time.Time // midnights in loc
	loc      *time.Location
	months   int // > 0 when the span is whole calendar months
}

func (s span) period() Period {
	return Period{From: s.from.Format(day), To: s.to.Format(day), TZ: s.loc.String()}
}

func (s span) ms() (int64, int64) { return s.from.UnixMilli(), s.to.UnixMilli() }

func (s span) days() float64 { return float64(daysBetween(s.from, s.to)) }

func daysBetween(a, b time.Time) int {
	n := 0
	for d := a; d.Before(b); d = d.AddDate(0, 0, 1) {
		n++
	}
	return n
}

func midnight(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func monthStart(t time.Time, loc *time.Location) time.Time { return usage.MonthStart(t, loc) }

func wholeMonths(from, to time.Time) int {
	if from.Day() != 1 || to.Day() != 1 {
		return 0
	}
	return (to.Year()-from.Year())*12 + int(to.Month()-from.Month())
}

func newSpan(from, to time.Time, loc *time.Location) span {
	return span{from: from, to: to, loc: loc, months: wholeMonths(from, to)}
}

func parseDay(v, field string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(day, v, loc)
	if err != nil {
		return time.Time{}, httpx.Invalid(field, "a day as YYYY-MM-DD")
	}
	return t, nil
}

// parseRange reads from/to (fromKey, toKey); ok is false when neither is given.
func parseRange(q url.Values, fromKey, toKey string, loc *time.Location) (span, bool, error) {
	fv, tv := q.Get(fromKey), q.Get(toKey)
	if fv == "" && tv == "" {
		return span{}, false, nil
	}
	if fv == "" || tv == "" {
		return span{}, false, httpx.Invalid(fromKey, fromKey+" and "+toKey+" go together")
	}
	from, err := parseDay(fv, fromKey, loc)
	if err != nil {
		return span{}, false, err
	}
	to, err := parseDay(tv, toKey, loc)
	if err != nil {
		return span{}, false, err
	}
	if !to.After(from) {
		return span{}, false, httpx.Invalid(toKey, toKey+" must be after "+fromKey)
	}
	if daysBetween(from, to) > maxDays {
		return span{}, false, httpx.Invalid(toKey, "at most 400 days")
	}
	return newSpan(from, to, loc), true, nil
}

// parsePeriod reads period=month|week|day (the current one, month by default) or from/to.
func parsePeriod(q url.Values, loc *time.Location, now time.Time) (span, error) {
	s, ok, err := parseRange(q, "from", "to", loc)
	if err != nil || ok {
		return s, err
	}
	today := midnight(now, loc)
	switch q.Get("period") {
	case "", "month":
		m := monthStart(now, loc)
		return newSpan(m, m.AddDate(0, 1, 0), loc), nil
	case "week":
		back := (int(today.Weekday()) + 6) % 7 // Monday first
		w := today.AddDate(0, 0, -back)
		return newSpan(w, w.AddDate(0, 0, 7), loc), nil
	case "day":
		return newSpan(today, today.AddDate(0, 0, 1), loc), nil
	}
	return span{}, httpx.Invalid("period", "month, week or day")
}

// comparison reads cmpFrom/cmpTo, or takes the same length right before: as many months for whole months, as many days
// otherwise.
func comparison(q url.Values, s span) (span, error) {
	c, ok, err := parseRange(q, "cmpFrom", "cmpTo", s.loc)
	if err != nil || ok {
		return c, err
	}
	if s.months > 0 {
		return newSpan(s.from.AddDate(0, -s.months, 0), s.from, s.loc), nil
	}
	return newSpan(s.from.AddDate(0, 0, -daysBetween(s.from, s.to)), s.from, s.loc), nil
}
