package apps

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// the host's own lines and the steps of a load live in the same ring as the page's; the ring keeps the last
// ViewLogMax whatever wrote them.
func TestAppLogKeepsHostLinesAndTrims(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	for _, level := range []string{ViewLogServe, ViewLogMCP, ViewLogLoad, ViewLogError} {
		if !ValidViewLevel(level) {
			t.Fatalf("%s is not a level", level)
		}
	}
	reg.LogEvent("acme.demo", "main", ViewLogServe, "GET ui/missing.js — 404")
	reg.LogEvent("acme.demo", "", ViewLogMCP, "tools.call x — host_unavailable: 502")
	lines, _ := reg.ViewLog("acme.demo", 0)
	if len(lines) != 2 || lines[0].Level != ViewLogServe || lines[1].Level != ViewLogMCP {
		t.Fatalf("lines: %+v", lines)
	}
	for i := 0; i < ViewLogMax+30; i++ {
		reg.LogEvent("acme.demo", "", ViewLogLoad, "step "+strings.Repeat("x", i%7))
	}
	if all, _ := reg.ViewLog("acme.demo", 0); len(all) != ViewLogMax {
		t.Fatalf("the ring holds %d, not %d", len(all), ViewLogMax)
	}
}

func TestViewLogSinceAndOnce(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	now := time.UnixMilli(1_800_000_000_000)
	_, _ = reg.AppendViewLog("acme.demo", []ViewLogLine{{At: 1_799_999_990_000, Level: ViewLogError, Text: "old"}, {At: 1_799_999_999_000, Level: ViewLogError, Text: "new"}}, now)
	got, _ := reg.ViewLogSince("acme.demo", 1_799_999_995_000, 0)
	if len(got) != 1 || got[0].Text != "new" {
		t.Fatalf("since: %+v", got)
	}
	for i := 0; i < 5; i++ {
		reg.LogEventOnce("acme.demo", "main", ViewLogServe, "GET same — 404")
	}
	if n := countText(reg, "GET same — 404"); n != 1 {
		t.Fatalf("a line repeated within seconds is written once, not %d times", n)
	}
}

func countText(reg *Registry, text string) int {
	all, _ := reg.ViewLog("acme.demo", 0)
	n := 0
	for _, l := range all {
		if l.Text == text {
			n++
		}
	}
	return n
}

func TestJudgeLoad(t *testing.T) {
	ok := Judge("a", "main", []ViewLogLine{{At: 1000, Level: ViewLogLoad, Text: "check page opened"}, {At: 1090, Level: ViewLogLoad, Text: "handshake ok after 90 ms"}})
	if !ok.OK() || ok.Ms != 90 || ok.Errors != 0 {
		t.Fatalf("clean load: %+v", ok)
	}
	bad := Judge("a", "main", []ViewLogLine{{At: 1000, Level: ViewLogLoad, Text: "check page opened"}, {At: 1090, Level: ViewLogLoad, Text: "handshake ok after 90 ms"}, {At: 1100, Level: ViewLogServe, Text: "GET x — 404"}})
	if bad.OK() || bad.State != CheckFailed || bad.Errors != 1 || !bad.Handshake {
		t.Fatalf("a refused file: %+v", bad)
	}
	none := Judge("a", "main", nil)
	if none.OK() || none.Handshake || none.Lines == nil {
		t.Fatalf("no handshake: %+v", none)
	}
	warn := Judge("a", "main", []ViewLogLine{{At: 1000, Level: ViewLogLoad, Text: "check page opened"}, {At: 1090, Level: ViewLogLoad, Text: "handshake ok after 90 ms"}, {At: 1100, Level: ViewLogWarn, Text: "deprecated"}})
	if !warn.OK() {
		t.Fatalf("a warning is not a failure: %+v", warn)
	}
}

// a check link opens any local view, is never listed, and is the only link whose page writes to the log.
func TestCheckLink(t *testing.T) {
	g, reg, _ := guestRig(t)
	token, link, err := g.CreateCheck("acme.demo", "")
	if err != nil || !link.Check {
		t.Fatalf("create: %v %+v", err, link)
	}
	if list, _ := g.List(""); len(list) != 0 {
		t.Fatalf("a check link is listed: %+v", list)
	}
	base := GuestPrefix + token + "/"
	page := guestDo(g, http.MethodGet, base, "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), `&#34;check&#34;:true`) {
		t.Fatalf("page: %d %s", page.Code, page.Body.String())
	}
	rep := guestDo(g, http.MethodPost, base+"__guest/report", `{"lines":[{"level":"load","text":"handshake ok after 5 ms"},{"level":"error","text":"boom\nsecond"},{"level":"nope","text":"x"}]}`)
	if rep.Code != 200 || !strings.Contains(rep.Body.String(), `"kept":2`) {
		t.Fatalf("report: %d %s", rep.Code, rep.Body.String())
	}
	if lines, _ := reg.ViewLog("acme.demo", 0); len(lines) != 2 || lines[1].Text != "boom second" || lines[0].View == "" {
		t.Fatalf("lines: %+v", lines)
	}
	// A file the view asks for and the server refuses is a line of the log too.
	if w := guestDo(g, http.MethodGet, base+"acme.demo/ui/nope.js", ""); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
	if lines, _ := reg.ViewLog("acme.demo", 0); !strings.Contains(lines[len(lines)-1].Text, "ui/nope.js") || lines[len(lines)-1].Level != ViewLogServe {
		t.Fatalf("the refusal is not in the log: %+v", lines)
	}
	// A guest's own link reports nothing.
	plain, _, _ := g.Create("acme.demo", "", 0, false)
	if w := guestDo(g, http.MethodPost, GuestPrefix+plain+"/__guest/report", `{"lines":[{"level":"error","text":"x"}]}`); w.Code != http.StatusForbidden {
		t.Fatalf("a guest link reported: %d", w.Code)
	}
	// The link ends with its time.
	g.Now = func() time.Time { return time.UnixMilli(1_800_000_000_000).Add(CheckTTL + time.Second) }
	if w := guestDo(g, http.MethodGet, base, ""); w.Code != http.StatusNotFound {
		t.Fatalf("a check link outlived its time: %d", w.Code)
	}
}
