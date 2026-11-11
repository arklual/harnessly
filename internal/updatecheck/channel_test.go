package updatecheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseChannel(t *testing.T) {
	for raw, want := range map[string]string{"": ChannelStable, "stable": ChannelStable, " Stable ": ChannelStable, "latest": ChannelLatest, "LATEST": ChannelLatest} {
		if got, ok := ParseChannel(raw); got != want || !ok {
			t.Errorf("ParseChannel(%q) = %q, %v", raw, got, ok)
		}
	}
	if got, ok := ParseChannel("nightly"); got != ChannelStable || ok {
		t.Errorf("an unknown channel must fall to stable and say so: %q, %v", got, ok)
	}
}

func TestParsePointer(t *testing.T) {
	p, err := ParsePointer([]byte(`{"schema":1,"channel":"stable","version":"v0.16.180","released_at":"2026-10-20T10:00:00Z","later_field":true}`))
	if err != nil || p.Version != "0.16.180" {
		t.Fatalf("%+v, %v", p, err)
	}
	for name, body := range map[string]string{
		"not json":       `<html>`,
		"other schema":   `{"schema":2,"channel":"stable","version":"0.16.180"}`,
		"other channel":  `{"schema":1,"channel":"latest","version":"0.16.180"}`,
		"no version":     `{"schema":1,"channel":"stable"}`,
		"a pre-release":  `{"schema":1,"channel":"stable","version":"1.0.0-rc.1"}`,
		"files manifest": `{"version":"0.16.180","files":{"runtime-linux-amd64":{"sha256":"00","size":1}}}`,
	} {
		if _, err := ParsePointer([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPromoteWindow(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	rel := func(age time.Duration) Candidate {
		return Candidate{TagName: "v0.16.180", HTMLURL: "https://github.com/o/r/releases/tag/v0.16.180",
			PublishedAt: now.Add(-age).Format(time.RFC3339)}
	}
	day := 24 * time.Hour

	p, err := Promote(rel(7*day), Promotion{Now: now})
	if err != nil {
		t.Fatalf("seven days out is the default window: %v", err)
	}
	want := Pointer{Schema: 1, Channel: "stable", Version: "0.16.180", URL: "https://github.com/o/r/releases/tag/v0.16.180",
		ReleasedAt: "2026-11-03T12:00:00Z", PromotedAt: "2026-11-10T12:00:00Z"}
	if p != want {
		t.Fatalf("pointer %+v", p)
	}

	_, err = Promote(rel(7*day-time.Minute), Promotion{Now: now})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "2026-11-10T12:01:00Z") {
		t.Fatalf("a minute short of the window must be refused with the time it opens: %v", err)
	}
	if _, err = Promote(rel(2*day), Promotion{Now: now, Window: day}); err != nil {
		t.Fatalf("a shorter window given by hand: %v", err)
	}
	if _, err = Promote(rel(-time.Hour), Promotion{Now: now}); !errors.Is(err, ErrRefused) {
		t.Fatalf("a release from the future: %v", err)
	}
}

func TestPromoteRefusals(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	for name, rel := range map[string]Candidate{
		"draft":             {TagName: "v0.16.180", PublishedAt: old, Draft: true},
		"pre-release flag":  {TagName: "v0.16.180", PublishedAt: old, Prerelease: true},
		"pre-release tag":   {TagName: "v1.0.0-rc.1", PublishedAt: old},
		"not a version":     {TagName: "channels", PublishedAt: old},
		"withdrawn title":   {TagName: "v0.16.180", PublishedAt: old, Name: "v0.16.180 [WITHDRAWN]"},
		"withdrawn notes":   {TagName: "v0.16.180", PublishedAt: old, Body: "## Notes\n[withdrawn] breaks the update of 0.16.170"},
		"no published time": {TagName: "v0.16.180"},
	} {
		if _, err := Promote(rel, Promotion{Now: now}); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}

	rel := Candidate{TagName: "v0.16.150", PublishedAt: old}
	cur := &Pointer{Schema: 1, Channel: "stable", Version: "0.16.180"}
	if _, err := Promote(rel, Promotion{Now: now, Current: cur}); !errors.Is(err, ErrRefused) {
		t.Fatalf("stable must not move back by itself: %v", err)
	}
	if p, err := Promote(rel, Promotion{Now: now, Current: cur, AllowOlder: true}); err != nil || p.Version != "0.16.150" {
		t.Fatalf("moving back when asked: %+v, %v", p, err)
	}
	if _, err := Promote(Candidate{TagName: "v0.16.180", PublishedAt: old}, Promotion{Now: now, Current: cur}); err != nil {
		t.Fatalf("promoting the stable release again is no error: %v", err)
	}
}

// source serves a latest release and, when stable is not empty, a stable pointer; it remembers the paths asked for.
type source struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newSource(t *testing.T, stable string) *source {
	t.Helper()
	s := &source{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.16.200","html_url":"https://github.com/o/r/releases/tag/v0.16.200"}`))
		case r.URL.Path == "/stable.json" && stable != "":
			_, _ = w.Write([]byte(stable))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *source) asked() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.paths, " ")
}

const stablePointer = `{"schema":1,"channel":"stable","version":"0.16.180","url":"https://github.com/o/r/releases/tag/v0.16.180","released_at":"2026-10-20T10:00:00Z"}`

func TestStableChannelFollowsThePointerNotTheLatestRelease(t *testing.T) {
	s := newSource(t, stablePointer)
	c := &Checker{Current: "0.16.100", URL: s.URL + "/latest", Channel: ChannelStable, StableURL: s.URL + "/stable.json"}
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := Status{Current: "0.16.100", Latest: "0.16.180", URL: "https://github.com/o/r/releases/tag/v0.16.180",
		PublishedAt: "2026-10-20T10:00:00Z", UpdateAvailable: true, Channel: "stable"}
	if got := c.Status(); got != want {
		t.Fatalf("got %+v", got)
	}
	if s.asked() != "/stable.json" {
		t.Fatalf("the stable channel asked for %q", s.asked())
	}

	// Ahead of stable (it ran on latest before): nothing to take, and nothing goes back.
	ahead := &Checker{Current: "0.16.200", URL: s.URL + "/latest", Channel: ChannelStable, StableURL: s.URL + "/stable.json"}
	if err := ahead.Check(context.Background()); err != nil || ahead.Status().UpdateAvailable {
		t.Fatalf("ahead of stable: %+v, %v", ahead.Status(), err)
	}
}

func TestLatestChannelReadsEveryRelease(t *testing.T) {
	s := newSource(t, stablePointer)
	for _, channel := range []string{ChannelLatest, ""} {
		c := &Checker{Current: "0.16.100", URL: s.URL + "/latest", Channel: channel, StableURL: s.URL + "/stable.json"}
		if err := c.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := c.Status(); got.Latest != "0.16.200" || !got.UpdateAvailable || got.Channel != channel || got.Note != "" {
			t.Fatalf("channel %q: %+v", channel, got)
		}
	}
	if s.asked() != "/latest /latest" {
		t.Fatalf("the latest channel asked for %q", s.asked())
	}
}

// A source of releases from before the channels has no pointer: the stable channel offers nothing, says why, and never
// reads the latest release instead.
func TestStableChannelWithoutAPointerStaysAndSaysWhy(t *testing.T) {
	s := newSource(t, "")
	c := &Checker{Current: "0.16.100", URL: s.URL + "/latest", Channel: ChannelStable, StableURL: s.URL + "/stable.json"}
	if err := c.Check(context.Background()); !errors.Is(err, ErrNoStable) {
		t.Fatalf("err %v", err)
	}
	got := c.Status()
	if got.UpdateAvailable || got.Latest != "" || !strings.Contains(got.Note, "No stable release is named") ||
		!strings.Contains(got.Note, "RUNTIME_UPDATE_CHANNEL=latest") {
		t.Fatalf("got %+v", got)
	}
	if s.asked() != "/stable.json" {
		t.Fatalf("asked for %q", s.asked())
	}

	// A mirror of the latest-release address alone: no request at all, and the setting that names the pointer.
	m := &Checker{Current: "0.16.100", URL: s.URL + "/latest", Channel: ChannelStable}
	if err := m.Check(context.Background()); !errors.Is(err, ErrNoStable) {
		t.Fatalf("err %v", err)
	}
	if got := m.Status(); got.UpdateAvailable || !strings.Contains(got.Note, "RUNTIME_UPDATE_STABLE_URL") {
		t.Fatalf("got %+v", got)
	}
	if s.asked() != "/stable.json" {
		t.Fatalf("a mirror without a pointer was asked: %q", s.asked())
	}
}

func TestStableChannelRefusesAPointerItCannotRead(t *testing.T) {
	// The files manifest a hub serves is not a pointer, whatever version it carries.
	s := newSource(t, `{"version":"0.16.200","files":{}}`)
	c := &Checker{Current: "0.16.100", Channel: ChannelStable, StableURL: s.URL + "/stable.json"}
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("no error")
	}
	if got := c.Status(); got.UpdateAvailable || got.Latest != "" || got.Note == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestStableChannelDefaultsToTheProjectsPointer(t *testing.T) {
	var asked string
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		asked = r.URL.String()
		rec := httptest.NewRecorder()
		_, _ = rec.WriteString(stablePointer)
		return rec.Result(), nil
	})}
	for _, url := range []string{"", DefaultURL} {
		c := &Checker{Current: "0.16.100", URL: url, Channel: ChannelStable, Client: client}
		if err := c.Check(context.Background()); err != nil || asked != DefaultStableURL {
			t.Fatalf("URL %q: asked %q, %v", url, asked, err)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The address of a mirror may carry its credentials: a note shows it without them.
func TestNoteShowsNoCredentialsOfTheMirror(t *testing.T) {
	s := newSource(t, "")
	withSecret := strings.Replace(s.URL, "http://", "http://reader:s3cret@", 1) + "/stable.json?token=t0ken"
	c := &Checker{Current: "0.16.100", Channel: ChannelStable, StableURL: withSecret}
	if err := c.Check(context.Background()); !errors.Is(err, ErrNoStable) {
		t.Fatalf("err %v", err)
	}
	note := c.Status().Note
	if strings.Contains(note, "s3cret") || strings.Contains(note, "reader") || strings.Contains(note, "t0ken") ||
		!strings.Contains(note, s.URL+"/stable.json yet") {
		t.Fatalf("note %q", note)
	}
	if got := shownURL("::not a url"); got != "the configured address" {
		t.Fatalf("an address that does not parse is shown as %q", got)
	}
}
