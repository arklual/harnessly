package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func growRights(t *testing.T, dir string, rights ...string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = rights
	raw, _ = json.Marshal(m)
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// HRN-913: a dev install that asked for nothing and then gained a right in its folder waits, and its screen lists the
// right it waits for (it listed nothing: the manifest kept at install time), so the click on it is taken.
func TestDevGrownScreenShowsWhatWaits(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	dir := pkgWith(t, "example.grow", "0.1.0")
	if _, _, err := r.Install(dir, InstallOptions{Consent: ConsentAsk, Dev: true}); err != nil {
		t.Fatal(err)
	}
	growRights(t, dir, "ai")
	a, err := r.Find("example.grow")
	if err != nil {
		t.Fatal(err)
	}
	if !a.NeedsConsent() {
		t.Fatal("a dev folder that gained a right must wait")
	}
	if len(AccessOf(a.Manifest, nil)) != 0 {
		t.Fatal("the installed manifest is the old one (the test assumes it)")
	}
	now := AccessKeys(a.AccessNow())
	if !slices.ContainsFunc(now, func(k string) bool { return strings.HasPrefix(k, "perm:ai") }) {
		t.Fatalf("the screen must list the right the folder gained: %v", now)
	}
	if codes := AccessCodes(a.WaitingAccess()); !slices.Equal(codes, []string{"ai"}) {
		t.Fatalf("waiting: %v", codes)
	}
	if st := StateOf(a, "", nil, nil, nil); st.Reason != ReasonConsent || st.Detail != "ai" {
		t.Fatalf("state %+v", st)
	}
	if _, err := r.Get("example.grow"); err == nil {
		t.Fatal("Get must not reach a waiting app")
	}
	if _, err := r.Consent("example.grow", "0.1.0", now, time.Now()); err != nil {
		t.Fatalf("the click on what the screen shows: %v", err)
	}
	if _, ok := listed(t, r, "example.grow"); !ok {
		t.Fatal("agreed, the app is on")
	}
}

func TestPaneWhyNotAndExplainSeeWaitingDev(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	dir := pkgWith(t, "example.wait", "0.1.0", "ai")
	if _, _, err := r.Install(dir, InstallOptions{Consent: ConsentAsk, Dev: true}); err != nil {
		t.Fatal(err)
	}
	why := r.PaneWhyNot("example.wait")
	if !strings.Contains(why, "waits for the person's consent to its access: ai") || !strings.Contains(why, "«Приложения» → example.wait") {
		t.Fatalf("pane: %q", why)
	}
	if r.PaneWhyNot("example.none") != "" {
		t.Fatal("an app not installed has no state to tell")
	}
	a, err := r.Find("example.wait")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AppendViewLog("example.wait", []ViewLogLine{{Level: ViewLogError, Text: "boom"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	log, _ := r.ViewLog("example.wait", 0)
	lines := Explain(ExplainInput{ID: "example.wait", App: &a, ViewLog: log})
	text := ""
	for _, l := range lines {
		text += l.Layer + " " + l.Says + "\n"
		if l.Layer == "machine" && strings.Contains(l.Says, "not installed") {
			t.Fatalf("explain does not see the dev install: %s", text)
		}
	}
	for _, want := range []string{"as a dev install", "consent waits for the person's consent to ai", "the person agrees in harnessly's page", "views 1 line(s)", "the last a error line", "harnessly app logs example.wait"} {
		if !strings.Contains(text, want) {
			t.Errorf("explain lacks %q:\n%s", want, text)
		}
	}
	if !slices.ContainsFunc(lines, func(l ExplainLine) bool { return l.Layer == "consent" && l.Off }) {
		t.Error("the consent line must be the one that keeps it off")
	}
}

func TestViewLogBounded(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	now := time.Now()
	var batch []ViewLogLine
	for range ViewLogBatchMax + 10 {
		batch = append(batch, ViewLogLine{Level: ViewLogError, Text: strings.Repeat("x", ViewLogTextMax+50)})
	}
	batch = append([]ViewLogLine{{Level: "debug", Text: "dropped"}, {Level: ViewLogWarn}}, batch...)
	kept, err := r.AppendViewLog("example.log", batch, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != ViewLogBatchMax || len([]rune(kept[0].Text)) != ViewLogTextMax || kept[0].At != now.UnixMilli() {
		t.Fatalf("kept %d, first %d chars at %d", len(kept), len([]rune(kept[0].Text)), kept[0].At)
	}
	for range 5 {
		if _, err := r.AppendViewLog("example.log", batch, now); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := r.ViewLog("example.log", 0)
	if len(all) != ViewLogMax {
		t.Fatalf("the log keeps %d, want %d", len(all), ViewLogMax)
	}
	if last, _ := r.ViewLog("example.log", 3); len(last) != 3 {
		t.Fatal("limit")
	}
	if _, err := r.AppendViewLog("../x", batch, now); err == nil {
		t.Fatal("a bad id is refused")
	}
	// The app's text stays one line: no newline, carriage return or terminal escape survives.
	kept, _ = r.AppendViewLog("example.log", []ViewLogLine{{Level: ViewLogError, Text: "a\nb\r\x1b[31mc\u2028d"}, {Level: ViewLogError, Text: "\n\t"}}, now)
	if len(kept) != 1 || kept[0].Text != "a b  [31mc d" {
		t.Fatalf("control characters: %+v", kept)
	}
	if err := r.ClearViewLog("example.log"); err != nil {
		t.Fatal(err)
	}
	if none, _ := r.ViewLog("example.log", 0); len(none) != 0 {
		t.Fatal("cleared")
	}
}
