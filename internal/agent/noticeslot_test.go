package agent

import (
	"strings"
	"testing"
)

// the agent passes the hub a slot list only when it changed, and keeps the last one for a link that comes.
func TestSlotCacheSendsOnlyNewLists(t *testing.T) {
	var c slotCache
	if c.last() != nil {
		t.Fatal("an empty cache has a list")
	}
	one := []byte(`{"k":"slot","notices":[{"id":"q1","createdAt":1,"doc":{"kind":"confirm"}},{"doc":{}}]}`)
	got := c.take(one)
	if got == nil || !strings.Contains(string(got), `"event":"noticeslot"`) || !strings.Contains(string(got), `"q1"`) {
		t.Fatalf("first list: %s", got)
	}
	if strings.Count(string(got), `"id"`) != 1 {
		t.Fatalf("a notice without an id went on: %s", got)
	}
	if c.take(one) != nil {
		t.Fatal("the same list went again")
	}
	if string(c.last()) != string(got) {
		t.Fatal("the kept list")
	}
	if c.take([]byte(`not json`)) != nil {
		t.Fatal("a broken frame went on")
	}
	if c.take([]byte(`{"k":"slot","notices":[]}`)) == nil {
		t.Fatal("an emptied slot is news")
	}
}
