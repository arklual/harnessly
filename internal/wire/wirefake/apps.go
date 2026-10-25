package wirefake

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeStaleDays is how long a row stands still to be stale, as the fake says it.
const fakeStaleDays = 5

// appsFeed is the fake machine's Apps screen: a tracker with epics on the person (with their subtasks) and tasks, GitLab
// with merge requests in every state the page draws. The rows are the fake's own; an app the test turns off says why, as
// the runtime does.
type appsFeed struct {
	mu    sync.Mutex
	state map[wire.AppID]wire.AppState
}

var _ wire.AppsSource = (*appsFeed)(nil)

// Apps is the fake machine's Apps screen. Every app is ready until a test turns it off (control `apps`).
func (m *Machine) Apps() wire.AppsSource { return m.apps }

func newAppsFeed() *appsFeed {
	return &appsFeed{state: map[wire.AppID]wire.AppState{wire.AppTracker: wire.AppReady, wire.AppGitLab: wire.AppReady}}
}

func (f *appsFeed) set(app wire.AppID, state wire.AppState) {
	f.mu.Lock()
	f.state[app] = state
	f.mu.Unlock()
}

func (f *appsFeed) reset() {
	f.mu.Lock()
	f.state = map[wire.AppID]wire.AppState{wire.AppTracker: wire.AppReady, wire.AppGitLab: wire.AppReady}
	f.mu.Unlock()
}

func ptr[T any](v T) *T { return &v }

func ago(now time.Time, d time.Duration) int64 { return now.Add(-d).UnixMilli() }

const fakeDay = 24 * time.Hour

func fakeEpic(now time.Time, id, title, state string, age time.Duration, labels []string, kids ...wire.AppChild) wire.AppRow {
	done := 0
	for i := range kids {
		kids[i].URL = "https://tracker.example.com/issue/" + kids[i].ID
		if kids[i].Done {
			done++
		}
	}
	tone := (*wire.AppTone)(nil)
	if state == "In Progress" {
		tone = ptr(wire.ToneAccent)
	}
	var closed *int64
	if state == "Done" {
		closed = ptr(ago(now, age))
	}
	if labels == nil {
		labels = []string{}
	}
	if kids == nil {
		kids = []wire.AppChild{}
	}
	return wire.AppRow{Key: id, ID: id, Title: title, URL: "https://tracker.example.com/issue/" + id, State: ptr(state), Tone: tone,
		Person: ptr("Peter"), CreatedAt: ago(now, age+40*fakeDay), UpdatedAt: ago(now, age), ClosedAt: closed, Author: ptr("Ivan"),
		Assignees: []string{"Peter"}, Reviewers: []string{}, Me: []wire.AppRole{wire.RoleAssignee}, Labels: labels, Priority: ptr("Normal"), Kind: ptr("Epic"),
		Progress: &wire.AppProgress{Done: done, Total: len(kids)}, Children: kids}
}

func fakeKid(id, title, state string, done bool, person *string) wire.AppChild {
	return wire.AppChild{ID: id, Title: title, State: ptr(state), Done: done, Person: person}
}

func fakeTask(now time.Time, id, title, state, kind string, age time.Duration, parent *wire.AppRef, labels ...string) wire.AppRow {
	var tone *wire.AppTone
	if state == "In Progress" || state == "Testing" {
		tone = ptr(wire.ToneAccent)
	}
	var closed *int64
	if state == "Done" {
		closed = ptr(ago(now, age))
	}
	if labels == nil {
		labels = []string{}
	}
	return wire.AppRow{Key: id, ID: id, Title: title, URL: "https://tracker.example.com/issue/" + id, State: ptr(state), Tone: tone,
		Person: ptr("Peter"), CreatedAt: ago(now, age+10*fakeDay), UpdatedAt: ago(now, age), ClosedAt: closed, Author: ptr("Ivan"),
		Assignees: []string{"Peter"}, Reviewers: []string{}, Me: []wire.AppRole{wire.RoleAssignee}, Labels: labels, Priority: ptr("Normal"), Kind: ptr(kind),
		Parent: parent, Children: []wire.AppChild{}}
}

type fakeMR struct {
	project, title, author, state string
	iid                           int
	age                           time.Duration
	reviewers, labels             []string
	mr                            wire.AppMR
	flag                          wire.AppFlag
}

func (m fakeMR) row(now time.Time, me string) wire.AppRow {
	link := "https://gitlab.example.com/team/" + m.project + "/-/merge_requests/" + itoa(m.iid)
	roles := []wire.AppRole{}
	if m.author == me {
		roles = append(roles, wire.RoleAuthor)
	}
	for _, r := range m.reviewers {
		if r == me {
			roles = append(roles, wire.RoleReviewer)
		}
	}
	tone := (*wire.AppTone)(nil)
	switch m.flag {
	case wire.FlagReview:
		tone = ptr(wire.ToneAccent)
	case wire.FlagConflict, wire.FlagCIFailed:
		tone = ptr(wire.ToneWarn)
	}
	var closed *int64
	if m.state != "opened" {
		closed = ptr(ago(now, m.age))
	}
	assignees := []string{}
	if m.author == me {
		assignees = append(assignees, me)
	}
	mr := m.mr
	reviewers := append([]string{}, m.reviewers...)
	mr.Source = ptr("feature/" + m.project)
	return wire.AppRow{Key: link, ID: "!" + itoa(m.iid), Title: m.title, URL: link, State: ptr(m.state), Flag: ptr(m.flag), Tone: tone,
		Project: ptr(m.project), Person: ptr(m.author), Target: ptr("main"), CreatedAt: ago(now, m.age+3*fakeDay), UpdatedAt: ago(now, m.age), ClosedAt: closed,
		Author: ptr(m.author), Assignees: assignees, Reviewers: reviewers, Me: roles, Labels: append([]string{}, m.labels...), Children: []wire.AppChild{}, MR: &mr}
}

func itoa(n int) string { return strconv.Itoa(n) }

func (f *appsFeed) Feed(_ context.Context) (wire.AppsFeed, error) {
	now := time.Now()
	const day = fakeDay
	asEpic := func(id, title string) *wire.AppRef {
		return &wire.AppRef{ID: id, Title: title, URL: "https://tracker.example.com/issue/" + id}
	}
	peter := ptr("Peter")
	assistant := asEpic("ABC-2628", "Assistant: releases 1.56–1.62")
	analytics := asEpic("ABC-2902", "New analytics for the organizer")
	tracker := wire.AppFeed{ID: wire.AppTracker, State: wire.AppReady, Host: ptr("tracker.example.com"), Me: peter, Sections: []wire.AppSection{
		{ID: wire.SectionEpics, Rows: []wire.AppRow{
			fakeEpic(now, "ABC-2628", assistant.Title, "In Progress", 3*time.Hour, []string{"repo:assistant"},
				fakeKid("ABC-2630", "Right panel as in the desktop app", "Done", true, peter),
				fakeKid("ABC-2631", "Choice widget in the answers", "Done", true, peter),
				fakeKid("ABC-2640", "Revision +1 for the cluster", "In Progress", false, peter),
				fakeKid("ABC-2641", "Machine agent in the sandbox", "New", false, nil)),
			fakeEpic(now, "ABC-2902", analytics.Title, "In Progress", 26*time.Hour, []string{"repo:admin-ui", "waiting:decision"},
				fakeKid("ABC-2910", "Six tabs of the dashboard", "Done", true, peter),
				fakeKid("ABC-2911", "Bar lists instead of tables", "In Progress", false, peter),
				fakeKid("ABC-2912", "Charts grid", "New", false, nil)),
			fakeEpic(now, "ABC-2486", "Parser hub", "New", 4*day, []string{"repo:loki-ui"}),
			fakeEpic(now, "ABC-2526", "Harness measurement EXP-14", "Testing", 8*day, nil,
				fakeKid("ABC-2528", "Phase 0", "Done", true, peter), fakeKid("ABC-2529", "Phase 1", "Done", true, peter)),
			fakeEpic(now, "ABC-1941", "Refund documents", "Done", 12*day, []string{"repo:payments"},
				fakeKid("ABC-1950", "The request form", "Done", true, peter), fakeKid("ABC-1951", "The documents pack", "Done", true, peter)),
		}},
		{ID: wire.SectionTasks, Rows: []wire.AppRow{
			fakeTask(now, "ABC-2640", "Revision +1 for the cluster", "In Progress", "Feature", 2*time.Hour, assistant, "repo:assistant"),
			fakeTask(now, "ABC-2911", "Bar lists instead of tables", "In Progress", "Feature", 30*time.Hour, analytics, "repo:admin-ui"),
			fakeTask(now, "ABC-3039", "Skeletons in the admin's order list", "In Progress", "Bug", 9*day, nil, "repo:admin-ui"),
			fakeTask(now, "ABC-2984", "Send tickets to the mail again", "Testing", "Bug", 12*day, nil, "repo:events", "waiting:external"),
			fakeTask(now, "ABC-3101", "A hypothesis about the cart drop", "New", "Hypothesis", 2*day, nil),
			fakeTask(now, "ABC-3120", "The scanner's offline cache", "New", "Engineering", 5*day, nil, "repo:scanner-mobile"),
			fakeTask(now, "OPS-214", "Rotate the staging certificates", "New", "Incident", 1*day, nil),
			fakeTask(now, "ABC-2559", "Findings schema in the review workflow", "Done", "Engineering", 3*day, nil),
		}},
	}}
	review := wire.AppMR{Ready: true, Pipeline: ptr(wire.PipelineSuccess), Approvals: &wire.AppApprovals{Given: 0, Required: 1}, Comments: 2}
	list := []fakeMR{
		{project: "admin", iid: 502, title: "Refund filter by the new statuses", author: "Ivan", state: "opened", age: 2 * time.Hour, reviewers: []string{"Peter"}, labels: []string{"frontend"}, flag: wire.FlagReview, mr: review},
		{project: "metrics", iid: 499, title: "A batch of /v1/collect holds 50 events at most", author: "Oleg", state: "opened", age: 28 * time.Hour, reviewers: []string{"Peter", "Ivan"}, labels: []string{"backend", "analytics"}, flag: wire.FlagReview, mr: wire.AppMR{Pipeline: ptr(wire.PipelineRunning), Approvals: &wire.AppApprovals{Given: 1, Required: 2}, Comments: 7, Unresolved: true}},
		{project: "webapp", iid: 478, title: "Offline ticket cache", author: "Peter", state: "opened", age: 6 * day, reviewers: []string{"Ivan"}, flag: wire.FlagConflict, mr: wire.AppMR{Conflicts: true, NeedsRebase: true, Pipeline: ptr(wire.PipelineFailed), Approvals: &wire.AppApprovals{Given: 0, Required: 1}}},
		{project: "payments", iid: 471, title: "Payout queue metrics", author: "Ivan", state: "opened", age: 8 * day, reviewers: []string{"Oleg"}, labels: []string{"backend"}, flag: wire.FlagCIFailed, mr: wire.AppMR{Pipeline: ptr(wire.PipelineFailed), Comments: 1}},
		{project: "storefront", iid: 466, title: "Lazy route of the catalog", author: "Oleg", state: "opened", age: 11 * day, flag: wire.FlagNoReviewer, labels: []string{"frontend", "performance"}, mr: wire.AppMR{Draft: true, Pipeline: ptr(wire.PipelineSuccess)}},
		{project: "harnessly", iid: 107, title: "Apps: the left picker and the full tracker", author: "Peter", state: "opened", age: 40 * time.Minute, reviewers: []string{"Ivan"}, labels: []string{"frontend"}, flag: wire.FlagOpen, mr: wire.AppMR{Ready: true, Pipeline: ptr(wire.PipelineRunning), Approvals: &wire.AppApprovals{Given: 1, Required: 1}}},
		{project: "harnessly", iid: 105, title: "Queue reorder keeps the draft", author: "Peter", state: "merged", age: 2 * day, reviewers: []string{"Ivan"}, flag: wire.FlagOpen, mr: wire.AppMR{Pipeline: ptr(wire.PipelineSuccess), Approvals: &wire.AppApprovals{Given: 1, Required: 1}}},
		{project: "notifier", iid: 13, title: "The moderation bot posts the cards", author: "Oleg", state: "closed", age: 5 * day, flag: wire.FlagOpen, mr: wire.AppMR{Pipeline: ptr(wire.PipelineCanceled)}},
	}
	rows := make([]wire.AppRow, len(list))
	for i, m := range list {
		rows[i] = m.row(now, "Peter")
	}
	gitlab := wire.AppFeed{ID: wire.AppGitLab, State: wire.AppReady, Host: ptr("gitlab.example.com"), Me: ptr("Peter"), Sections: []wire.AppSection{{ID: wire.SectionMRs, Rows: rows}}}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := wire.AppsFeed{At: now.UnixMilli(), StaleDays: fakeStaleDays, Apps: []wire.AppFeed{tracker, gitlab}}
	for i, app := range out.Apps {
		switch f.state[app.ID] {
		case wire.AppOff:
			out.Apps[i] = wire.AppFeed{ID: app.ID, State: wire.AppOff, Reason: ptr(wire.AppNoToken), Host: app.Host, Sections: []wire.AppSection{}}
		case wire.AppFailed:
			out.Apps[i] = wire.AppFeed{ID: app.ID, State: wire.AppFailed, Reason: ptr(wire.AppUnreachable), Host: app.Host, Sections: []wire.AppSection{}}
		}
	}
	return out, nil
}
