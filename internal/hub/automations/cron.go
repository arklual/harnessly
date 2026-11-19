package automations

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a parsed five-field cron expression: minute, hour, day of month, month, day of week. Each field is a set
// of allowed values. As in Vixie cron, when both the day of month and the day of week are restricted, a day matches
// when either matches.
type cronSpec struct {
	minute, hour, dom, month, dow uint64
	domAny, dowAny                bool
}

var cronAliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

// parseCron reads "m h dom mon dow": numbers, "*", ranges "a-b", steps "*/n" and "a-b/n", lists "a,b", and the
// aliases @hourly, @daily, @weekly, @monthly, @yearly. Day of week is 0–7, both 0 and 7 being Sunday. Month and day
// names are not taken: a number is what the reader of the rule sees in the table too.
func parseCron(expr string) (cronSpec, error) {
	expr = strings.TrimSpace(expr)
	if a, ok := cronAliases[strings.ToLower(expr)]; ok {
		expr = a
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return cronSpec{}, fmt.Errorf("в расписании пять полей: минута, час, день месяца, месяц, день недели")
	}
	var c cronSpec
	var err error
	if c.minute, _, err = cronField(f[0], 0, 59); err != nil {
		return c, fmt.Errorf("минута: %w", err)
	}
	if c.hour, _, err = cronField(f[1], 0, 23); err != nil {
		return c, fmt.Errorf("час: %w", err)
	}
	if c.dom, c.domAny, err = cronField(f[2], 1, 31); err != nil {
		return c, fmt.Errorf("день месяца: %w", err)
	}
	if c.month, _, err = cronField(f[3], 1, 12); err != nil {
		return c, fmt.Errorf("месяц: %w", err)
	}
	if c.dow, c.dowAny, err = cronField(f[4], 0, 7); err != nil {
		return c, fmt.Errorf("день недели: %w", err)
	}
	if c.dow&(1<<7) != 0 {
		c.dow |= 1 // 7 is Sunday too
	}
	return c, nil
}

// cronField is the set of one field; any says the field was "*" (or "*/1"), which matters for the day rule.
func cronField(s string, lo, hi int) (set uint64, any bool, err error) {
	for _, part := range strings.Split(s, ",") {
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			if step, err = strconv.Atoi(stepS); err != nil || step < 1 || step > hi {
				return 0, false, fmt.Errorf("шаг %q", stepS)
			}
		}
		from, to := lo, hi
		switch {
		case rng == "*":
			if step == 1 && len(s) == len(part) {
				any = true
			}
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			if from, err = strconv.Atoi(a); err != nil {
				return 0, false, fmt.Errorf("%q", part)
			}
			if to, err = strconv.Atoi(b); err != nil {
				return 0, false, fmt.Errorf("%q", part)
			}
		default:
			if from, err = strconv.Atoi(rng); err != nil {
				return 0, false, fmt.Errorf("%q", part)
			}
			to = from
			if hasStep {
				to = hi
			}
		}
		if from < lo || to > hi || from > to {
			return 0, false, fmt.Errorf("%q вне %d–%d", part, lo, hi)
		}
		for v := from; v <= to; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, any, nil
}

// matches says whether the minute of t (in t's own location) is a slot of the expression.
func (c cronSpec) matches(t time.Time) bool {
	if c.minute&(1<<uint(t.Minute())) == 0 || c.hour&(1<<uint(t.Hour())) == 0 || c.month&(1<<uint(t.Month())) == 0 {
		return false
	}
	domOK := c.dom&(1<<uint(t.Day())) != 0
	dowOK := c.dow&(1<<uint(t.Weekday())) != 0
	if c.domAny || c.dowAny {
		return domOK && dowOK
	}
	return domOK || dowOK
}

// lastSlot is the latest slot at or before now and after `since` and within `window` of now, in loc; false when there
// is none. Slots further back than window are not caught up: a hub that was down at nine does not send the nine
// o'clock digest at noon.
func (c cronSpec) lastSlot(now, since time.Time, window time.Duration, loc *time.Location) (time.Time, bool) {
	t := now.In(loc).Truncate(time.Minute)
	floor := now.Add(-window)
	for ; !t.Before(floor) && !t.Before(since); t = t.Add(-time.Minute) {
		if c.matches(t) {
			return t, true
		}
	}
	return time.Time{}, false
}

// next is the first slot after t (in t's location), within 400 days; false when there is none. A day or an hour that
// cannot match is stepped over whole.
func (c cronSpec) next(t time.Time) (time.Time, bool) {
	t = t.Truncate(time.Minute).Add(time.Minute)
	end := t.AddDate(0, 0, 400)
	for t.Before(end) {
		dayOK := c.month&(1<<uint(t.Month())) != 0
		if dayOK {
			domOK := c.dom&(1<<uint(t.Day())) != 0
			dowOK := c.dow&(1<<uint(t.Weekday())) != 0
			if c.domAny || c.dowAny {
				dayOK = domOK && dowOK
			} else {
				dayOK = domOK || dowOK
			}
		}
		if !dayOK {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if c.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if c.matches(t) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}
