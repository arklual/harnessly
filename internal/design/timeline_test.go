package design

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const timelineT0 = int64(1760000000000)

func fixedClock(t *testing.T, at int64) *int64 {
	t.Helper()
	now := at
	prev := nowMs
	nowMs = func() int64 { return now }
	t.Cleanup(func() { nowMs = prev })
	return &now
}

func timelineLibrary(t *testing.T, data string) *Library {
	t.Helper()
	l := New(Config{DataDir: data, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	return l
}

func ptr(s string) *string { return &s }

func TestTimelineKeepsOnlyWellFormedEvents(t *testing.T) {
	fixedClock(t, timelineT0)
	l := timelineLibrary(t, "")
	raw := fmt.Sprintf(`[
	  {"k":"click","t":%d,"tag":"button","tx":"Buy","x":10.456,"y":20},
	  {"k":"nope","t":%d},
	  {"k":"click","t":1,"tx":"clock far in the past"},
	  {"k":"console","t":%d,"l":"error","m":%q,"nested":{"a":1},"list":[1],"Bad Key":"x","ok":true}
	]`, timelineT0-5000, timelineT0, timelineT0-1000, strings.Repeat("я", 400))
	if n := l.RecordTimeline("d1", ptr("Main.dc.html"), &raw); n != 3 {
		t.Fatalf("took %d events", n)
	}
	got := l.Timeline("d1", 0, 0)
	if len(got) != 3 || got[0].K != "click" || got[0].T != timelineT0-5000 || got[0].Num("x") != 10.46 || got[0].Page != "Main.dc.html" {
		t.Fatalf("events = %+v", got)
	}
	if got[2].K != "click" || got[2].T != timelineT0 {
		t.Fatalf("a time far from the clock is the clock's: %+v", got[2])
	}
	c := got[1]
	if len(c.Str("m")) > timelineMaxText || len(c.Str("m")) < timelineMaxText-1 || c.Has("nested") || c.Has("list") || c.Has("Bad Key") || c.Fields["ok"] != true {
		t.Fatalf("console event = %+v", c)
	}
	for _, bad := range []string{`not json`, `{"k":"click"}`, `[]`, ``} {
		if n := l.RecordTimeline("d1", nil, &bad); n != 0 {
			t.Errorf("%q took %d events", bad, n)
		}
	}
	if n := l.RecordTimeline("d1", nil, nil); n != 0 {
		t.Error("nil batch")
	}
}

func TestTimelinePersistsPerDesignAndFilters(t *testing.T) {
	fixedClock(t, timelineT0)
	dir := t.TempDir()
	l := timelineLibrary(t, dir)
	batch := func(art string, kinds ...string) {
		var items []string
		for i, k := range kinds {
			items = append(items, fmt.Sprintf(`{"k":%q,"t":%d,"y":%d}`, k, timelineT0-int64(len(kinds)-i)*1000, i))
		}
		raw := "[" + strings.Join(items, ",") + "]"
		l.RecordTimeline(art, ptr("p"), &raw)
	}
	batch("d1", "nav", "click", "scroll", "lt")
	batch("d2", "nav")
	if _, err := os.Stat(filepath.Join(dir, timelineDirName, "d1.jsonl")); err != nil {
		t.Fatal(err)
	}
	again := timelineLibrary(t, dir)
	all := again.Timeline("d1", 0, 0)
	if len(all) != 4 || all[0].K != "nav" || all[3].K != "lt" {
		t.Fatalf("after a restart: %+v", all)
	}
	if len(again.Timeline("d2", 0, 0)) != 1 {
		t.Fatal("designs are kept apart")
	}
	mid := again.Timeline("d1", timelineT0-3000, timelineT0-2000)
	if len(mid) != 2 || mid[0].K != "click" || mid[1].K != "scroll" {
		t.Fatalf("from/to are both included: %+v", mid)
	}
	if len(again.Timeline("d1", timelineT0+1, 0)) != 0 || len(again.Timeline("none", 0, 0)) != 0 {
		t.Fatal("empty ranges")
	}
}

func TestTimelineIsCappedByCountAndAge(t *testing.T) {
	clock := fixedClock(t, timelineT0)
	dir := t.TempDir()
	l := timelineLibrary(t, dir)
	record := func(n int, at int64) {
		for done := 0; done < n; done += timelineMaxBatch {
			var items []string
			for i := 0; i < timelineMaxBatch && done+i < n; i++ {
				items = append(items, fmt.Sprintf(`{"k":"scroll","t":%d,"y":%d}`, at, done+i))
			}
			raw := "[" + strings.Join(items, ",") + "]"
			l.RecordTimeline("d1", ptr("p"), &raw)
		}
	}
	record(TimelineMaxEvents+500, timelineT0)
	n := len(l.Timeline("d1", 0, 0))
	if n > TimelineMaxEvents || n < TimelineMaxEvents/2 {
		t.Fatalf("kept %d events after %d", n, TimelineMaxEvents+500)
	}
	lines := strings.Count(readFile(t, filepath.Join(dir, timelineDirName, "d1.jsonl")), "\n")
	if lines > TimelineMaxEvents+timelineCheckEvery+timelineMaxBatch {
		t.Fatalf("the file has %d lines", lines)
	}
	// Hours later the old events are gone from reads at once, and from the file at the next check.
	*clock = timelineT0 + TimelineMaxAgeMs + 60_000
	if got := l.Timeline("d1", 0, 0); len(got) != 0 {
		t.Fatalf("%d events older than the age limit are still read", len(got))
	}
	record(timelineCheckEvery, *clock)
	fresh := timelineLibrary(t, dir)
	if got := fresh.Timeline("d1", 0, 0); len(got) != timelineCheckEvery {
		t.Fatalf("after the check %d events are kept, want %d", len(got), timelineCheckEvery)
	}
	if info, _ := os.Stat(filepath.Join(dir, timelineDirName, "d1.jsonl")); info.Size() > TimelineMaxFileBytes {
		t.Fatalf("file is %d bytes", info.Size())
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTimelineByteLimit(t *testing.T) {
	fixedClock(t, timelineT0)
	dir := t.TempDir()
	l := timelineLibrary(t, dir)
	text := strings.Repeat("x", timelineMaxText)
	for i := 0; i < 4500; i += 30 {
		var items []string
		for j := 0; j < 30; j++ {
			items = append(items, fmt.Sprintf(`{"k":"console","t":%d,"l":"error","m":%q,"a":%q,"b":%q,"c":%q,"d":%q,"e":%q}`, timelineT0, text, text, text, text, text, text))
		}
		raw := "[" + strings.Join(items, ",") + "]"
		l.RecordTimeline("d1", ptr("p"), &raw)
	}
	info, err := os.Stat(filepath.Join(dir, timelineDirName, "d1.jsonl"))
	if err != nil || info.Size() > TimelineMaxFileBytes+int64(timelineCheckEvery*2*timelineMaxText+4096) {
		t.Fatalf("file = %v %v", info, err)
	}
	if n := len(l.Timeline("d1", 0, 0)); n >= 4500 || n < 1000 {
		t.Fatalf("kept %d of 4500 big events", n)
	}
}

func TestParseWhenAndSpan(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 10, 27, 14, 5, 0, 0, loc)
	cases := map[string]time.Time{
		"now":                       now,
		"-2m":                       now.Add(-2 * time.Minute),
		"-90s":                      now.Add(-90 * time.Second),
		"-1h":                       now.Add(-time.Hour),
		"14:03:22":                  time.Date(2026, 10, 27, 14, 3, 22, 0, loc),
		"14:03":                     time.Date(2026, 10, 27, 14, 3, 0, 0, loc),
		"9:00:01.5":                 time.Date(2026, 10, 27, 9, 0, 1, 500_000_000, loc),
		"23:59:00":                  time.Date(2026, 10, 26, 23, 59, 0, 0, loc),
		"2026-10-27T11:03:22Z":      time.Date(2026, 10, 27, 11, 3, 22, 0, time.UTC),
		"2026-10-27T14:03:22+03:00": time.Date(2026, 10, 27, 14, 3, 22, 0, loc),
	}
	for in, want := range cases {
		got, err := ParseWhen(in, now, loc)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "yesterday", "25:00:00", "14:61", "-2x", "2026-10-27"} {
		if _, err := ParseWhen(bad, now, loc); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	for in, want := range map[string]time.Duration{"30s": 30 * time.Second, "2m": 2 * time.Minute, "45": 45 * time.Second, "1.5": 1500 * time.Millisecond} {
		if got, err := ParseSpan(in); err != nil || got != want {
			t.Errorf("span %q = %v, %v", in, got, err)
		}
	}
	if _, err := ParseSpan("soon"); err == nil {
		t.Error("span soon")
	}
}

func ev(at time.Time, kind, page string, fields map[string]any) TimelineEvent {
	return TimelineEvent{T: at.UnixMilli(), K: kind, Page: page, Fields: fields}
}

func sampleEvents(base time.Time) []TimelineEvent {
	p := "Main.dc.html?v=Desktop"
	at := func(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }
	return []TimelineEvent{
		ev(at(0), "nav", p, map[string]any{"u": p, "w": 1440, "h": 900, "dpr": 2}),
		ev(at(500), "tweak", p, map[string]any{"n": "theme", "v": "dark"}),
		ev(at(1000), "click", p, map[string]any{"tag": "button", "sel": "main > button.cta", "tx": "Buy now", "x": 412, "y": 318}),
		ev(at(2000), "scroll", p, map[string]any{"y": 0, "max": 5200}),
		ev(at(2250), "scroll", p, map[string]any{"y": 600, "max": 5200}),
		ev(at(2500), "scroll", p, map[string]any{"y": 1840, "max": 5200}),
		ev(at(2600), "lt", p, map[string]any{"d": 412}),
		ev(at(3000), "jank", p, map[string]any{"d": 180, "n": 10}),
		ev(at(3100), "req", p, map[string]any{"me": "GET", "u": "/api/items", "st": 500, "d": 2100, "x": "fetch"}),
		ev(at(3200), "console", p, map[string]any{"l": "error", "m": "TypeError: x is undefined"}),
		ev(at(4000), "nav", "Other.dc.html", map[string]any{"u": "Other.dc.html", "w": 1440, "h": 900}),
	}
}

func TestRenderTimelineIsCompactAndMergesScrolls(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	base := time.Date(2026, 10, 27, 14, 3, 20, 0, loc)
	out := RenderTimeline(sampleEvents(base), loc, 0)
	for _, want := range []string{
		"# page Main.dc.html?v=Desktop\n",
		"14:03:20.000  nav      Main.dc.html?v=Desktop  1440x900\n",
		"14:03:20.500  tweak    theme=dark\n",
		`14:03:21.000  click    main > button.cta "Buy now" (412,318)` + "\n",
		"14:03:22.000  scroll   y 0 -> 1840 of 5200 (3 moves, 500 ms)\n",
		"14:03:22.600  longtask 412 ms main thread busy\n",
		"14:03:23.000  jank     frame gap 180 ms (10 frames dropped)\n",
		"14:03:23.100  request  GET /api/items 500 2.1 s\n",
		"14:03:23.200  console.error TypeError: x is undefined\n",
		"# page Other.dc.html\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "scroll") != 1 {
		t.Errorf("scrolls not merged:\n%s", out)
	}
	short := RenderTimeline(sampleEvents(base), loc, 5)
	if !strings.Contains(short, "... ") || strings.Count(short, "\n") > 8 {
		t.Errorf("limit:\n%s", short)
	}
	if empty := RenderTimeline(nil, loc, 0); !strings.Contains(empty, "No events") {
		t.Errorf("empty: %q", empty)
	}
}

func TestMomentCollectsTheStateBeforeAndTheWindowAround(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	base := time.Date(2026, 10, 27, 14, 3, 20, 0, loc)
	clock := fixedClock(t, base.UnixMilli()+60_000)
	l := timelineLibrary(t, "")
	var items []string
	for _, e := range sampleEvents(base) {
		b, _ := json.Marshal(e)
		items = append(items, string(b))
	}
	// A vital long before the window still counts as the state at that time.
	items = append(items, fmt.Sprintf(`{"k":"vital","t":%d,"n":"LCP","v":1234}`, base.UnixMilli()-20_000))
	raw := "[" + strings.Join(items, ",") + "]"
	l.RecordTimeline("d1", nil, &raw)
	_ = clock

	m := l.MomentAt("d1", base.UnixMilli()+3000, 900)
	kinds := map[string]int{}
	for _, e := range m.Events {
		kinds[e.K]++
	}
	if len(m.Events) != 6 || kinds["jank"] != 1 || kinds["req"] != 1 || kinds["console"] != 1 || kinds["lt"] != 1 || kinds["scroll"] != 2 {
		t.Fatalf("window = %v", kinds)
	}
	out := RenderMoment(m, loc)
	for _, want := range []string{
		"Moment 14:03:23.000 (window +-900ms)",
		"Page: Main.dc.html?v=Desktop (opened 14:03:20.000)",
		"Viewport: 1440x900 @2x",
		"Tweaks: theme=dark",
		"Vitals so far: LCP 1.2 s",
		`14:03:21.000  click    main > button.cta "Buy now"`,
		"14:03:23.100  request  GET /api/items 500 2.1 s",
		"14:03:22.600  longtask 412 ms",
		"14:03:23.000  jank",
		"Reading: 1 errors; 1 failed requests",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "1 errors; 1 failed requests; 1 long tasks, worst 412 ms; 1 frame-drop records that coincide with long tasks") {
		t.Errorf("reading:\n%s", out)
	}
	if strings.Contains(out, "Other.dc.html") {
		t.Errorf("a later page shows at an earlier moment:\n%s", out)
	}
	quiet := RenderMoment(l.MomentAt("nothing", base.UnixMilli(), 5000), loc)
	if !strings.Contains(quiet, "Page: unknown") || !strings.Contains(quiet, "nothing unusual") {
		t.Errorf("quiet:\n%s", quiet)
	}
}

func TestTimelineRoutes(t *testing.T) {
	fixedClock(t, timelineT0)
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	raw := fmt.Sprintf(`[{"k":"click","t":%d,"tx":"Go"},{"k":"lt","t":%d,"d":99}]`, timelineT0-2000, timelineT0-1000)
	l.RecordTimeline("p1", ptr("Main.dc.html"), &raw)
	get := func(q string) (int, string) {
		ans := a.CLIDesign(httptest.NewRequest(http.MethodGet, "/api/cli/design?"+q, nil))
		b, _ := json.Marshal(ans.Body)
		return ans.Status, string(b)
	}
	code, body := get(fmt.Sprintf("timeline=p1&from=%d&to=%d", timelineT0-1500, timelineT0))
	if code != 200 || !strings.Contains(body, `"events":[{"d":99,"k":"lt","p":"Main.dc.html","t":1759999999000}]`) {
		t.Fatalf("timeline = %d %s", code, body)
	}
	code, body = get(fmt.Sprintf("moment=p1&at=%d&window=3000", timelineT0))
	if code != 200 || !strings.Contains(body, `"window":3000`) || !strings.Contains(body, `"tx":"Go"`) {
		t.Fatalf("moment = %d %s", code, body)
	}
	for q, want := range map[string]int{"timeline=Bad%20id": 400, "timeline=p1&from=x": 400, "moment=p1": 400, "moment=p1&at=-4": 400} {
		if code, body := get(q); code != want {
			t.Errorf("%s = %d %s", q, code, body)
		}
	}
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"p1": "none"})); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"timeline=p1", "moment=p1&at=1"} {
		if code, _ := get(q); code != 404 {
			t.Errorf("hidden %s = %d", q, code)
		}
	}
}

// The page writes its batch with the log line's request.
func TestLogRequestCarriesTimelineBatches(t *testing.T) {
	fixedClock(t, timelineT0)
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	l.srv = &server{}
	q := fmt.Sprintf(`/__lib/log?a=p1&p=Main.dc.html&e=%s`, "%5B%7B%22k%22%3A%22click%22%2C%22t%22%3A"+fmt.Sprint(timelineT0)+"%2C%22tx%22%3A%22Hi%22%7D%5D")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", q, nil)
	r.Host = "127.0.0.1:18470"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	l.serveLibrary(w, r, 18470)
	if w.Code != 204 {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	if got := l.Timeline("p1", 0, 0); len(got) != 1 || got[0].Str("tx") != "Hi" || got[0].Page != "Main.dc.html" {
		t.Fatalf("events = %+v", got)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", q, nil)
	r.Host = "127.0.0.1:18470"
	l.serveLibrary(w, r, 18470)
	if w.Code != 403 || len(l.Timeline("p1", 0, 0)) != 1 {
		t.Fatalf("a cross-site request recorded: %d", w.Code)
	}
}
