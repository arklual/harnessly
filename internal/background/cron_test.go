package background

import (
	"testing"
	"time"
)

// A cron job of CronCreate is pending work of the session until CronDelete ends it or its life runs out.
func TestCronJobsArePendingUntilDeletedOrExpired(t *testing.T) {
	ts := "2026-11-17T10:00:00Z"
	at, _ := time.Parse(time.RFC3339, ts)
	start := float64(at.UnixMilli())
	lines := []string{
		line(`{"type":"tool_use","id":"c1","name":"CronCreate","input":{"cron":"*/5 * * * *","prompt":"check the queue"}}`, ts),
		result("c1", "Scheduled recurring job 1a2b3c4d (*/5 * * * *). Session-only (not written to disk, dies when Claude exits). Auto-expires after 7 days.", ts),
		line(`{"type":"tool_use","id":"c2","name":"CronCreate","input":{"cron":"17 4 1 1 *","prompt":"once","recurring":false}}`, ts),
		result("c2", "Scheduled one-shot task bcf51e2a (17 4 1 1 *). Next run: Jan 1 4:17am. Session-only (not written to disk, dies when Claude exits). It will fire once then auto-delete.", ts),
	}
	got := ParseTasks(lines, start+60_000)
	if len(got) != 2 || got[0].ID != "cron-1a2b3c4d" || got[0].Kind != Cron || got[1].ID != "cron-bcf51e2a" {
		t.Fatalf("both jobs pending: %+v", got)
	}
	// The one-shot one is over after a day, the recurring one after a week.
	if got := ParseTasks(lines, start+25*3_600_000); len(got) != 1 || got[0].ID != "cron-1a2b3c4d" {
		t.Fatalf("a day later: %v", ids(got))
	}
	if got := ParseTasks(lines, start+8*24*3_600_000); len(got) != 0 {
		t.Fatalf("a week later: %v", ids(got))
	}
	deleted := append(lines,
		line(`{"type":"tool_use","id":"c3","name":"CronDelete","input":{"id":"1a2b3c4d"}}`, ts),
		result("c3", "Cancelled job 1a2b3c4d.", ts))
	if got := ParseTasks(deleted, start+60_000); len(got) != 1 || got[0].ID != "cron-bcf51e2a" {
		t.Fatalf("after CronDelete: %v", ids(got))
	}
}
