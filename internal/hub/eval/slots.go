package eval

import (
	"strconv"
	"strings"
	"time"
)

// Schedules are kept in Moscow time (UTC+3, no summer time), the way the old gateway kept them: a person says «Monday at
// 10:40», the weeks of «every N weeks» are counted from the Monday of epochMonday.
var (
	msk         = time.FixedZone("MSK", 3*3600)
	epochMonday = time.Date(2026, 10, 5, 0, 0, 0, 0, msk)
)

// parseAt reads "HH:MM".
func parseAt(at string) (h, m int, ok bool) {
	hh, mm, found := strings.Cut(at, ":")
	if !found {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func hasDay(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

// isoDay is 1 for Monday .. 7 for Sunday.
func isoDay(t time.Time) int {
	if d := int(t.Weekday()); d != 0 {
		return d
	}
	return 7
}

// activeWeek says whether the week of slot is one of the schedule's weeks.
func activeWeek(slot time.Time, every int) bool {
	if every <= 1 {
		return true
	}
	const week = 7 * 24 * time.Hour
	d := slot.Sub(epochMonday)
	n := int64(d / week)
	if d < 0 && d%week != 0 {
		n--
	}
	return ((n%int64(every))+int64(every))%int64(every) == 0
}

// LatestSlot is the newest slot of the schedule that is not after now; ok is false for a schedule with no slot in the
// last eight days or one whose newest slot falls in a week it skips (the week after is not looked back at, as before).
func LatestSlot(days []int, atMsk string, every int, now time.Time) (time.Time, bool) {
	h, m, ok := parseAt(atMsk)
	if !ok {
		return time.Time{}, false
	}
	for back := 0; back < 8; back++ {
		d := now.In(msk).AddDate(0, 0, -back)
		if !hasDay(days, isoDay(d)) {
			continue
		}
		slot := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, msk)
		if slot.After(now) {
			continue
		}
		if !activeWeek(slot, every) {
			return time.Time{}, false
		}
		return slot, true
	}
	return time.Time{}, false
}

// NextSlot is the first slot after now.
func NextSlot(days []int, atMsk string, every int, now time.Time) (time.Time, bool) {
	h, m, ok := parseAt(atMsk)
	if !ok {
		return time.Time{}, false
	}
	for ahead := 0; ahead <= 7*max(1, every); ahead++ {
		d := now.In(msk).AddDate(0, 0, ahead)
		if !hasDay(days, isoDay(d)) {
			continue
		}
		slot := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, msk)
		if !slot.After(now) || !activeWeek(slot, every) {
			continue
		}
		return slot, true
	}
	return time.Time{}, false
}
