package main

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const testHost = "gitlab.example.org"

// updated is when the merge requests of a project last changed; searchable the projects the GitLab's search finds.
var (
	updated    = map[string]string{"team/harnessly": "2026-10-20T10:00:00Z", "other/harnessly": "2026-10-23T10:00:00Z", "ai/model-gateway": "2026-10-01T10:00:00Z"}
	searchable = map[string][]string{"tickets": {"acme/tickets", "elsewhere/tickets"}, "empty": nil}
)

// openFixture is a runtime whose GitLab holds the merge requests in have ("group/proj!iid"), and whose chats stand in
// folders: /w/repo is a checkout, /w/umbrella holds three, /w/plain nothing.
func openFixture(have map[string]string, asked *atomic.Int32) v2Open {
	remotes := map[string][]string{
		"/w/repo":               {"https://github.com/o/r.git", "git@" + testHost + ":group/sub/proj.git"},
		"/w/umbrella/harnessly": {"git@" + testHost + ":team/harnessly.git"},
		"/w/umbrella/llm":       {"https://" + testHost + "/ai/model-gateway.git"},
		"/w/umbrella/mirror":    {"https://github.com/o/mirror.git"},
		"/w/umbrella/twin":      {"git@" + testHost + ":other/harnessly.git"},
	}
	return v2Open{
		record: func(conv string) (wire.Conversation, bool) {
			return wire.Conversation{Cwd: "/w/" + conv, TranscriptPath: "/t/" + conv}, conv != "gone"
		},
		mrLinks: func(path string) []sources.MRMention {
			if path != "/t/talk" {
				return nil
			}
			m := func(project string, iid string) sources.MRMention {
				return sources.MRMention{URL: "https://" + testHost + "/" + project + "/-/merge_requests/" + iid, Host: testHost, Project: project, IID: iid}
			}
			return []sources.MRMention{m("a/old", "9"), m("a/b", "42"), m("a/new", "9"), {Host: "evil.example", Project: "x/y", IID: "9"}}
		},
		targets: panel.Targets{GitLabHosts: []string{testHost}, TrackerHosts: []string{"tracker.example.org"}, TaskKey: regexp.MustCompile(`^ABC-\d+$`)},
		remotes: func(_ context.Context, dir string) []string { return remotes[dir] },
		repos: func(dir string) []string {
			if dir == "/w/umbrella" {
				return []string{"harnessly", "llm", "mirror", "twin"}
			}
			return nil
		},
		mr: func(_ context.Context, host, project string, iid int) (mrInfo, bool) {
			if asked != nil {
				asked.Add(1)
			}
			title, ok := have[project+"!"+strconv.Itoa(iid)]
			return mrInfo{url: "https://" + host + "/" + project + "/-/merge_requests/" + strconv.Itoa(iid), title: title, updatedAt: updated[project]}, ok
		},
		projects: func(_ context.Context, _, name string) []string { return searchable[name] },
	}
}

func opens(t *testing.T, o v2Open, conv, ref string) *wire.OpenTarget {
	t.Helper()
	got, err := o.Open(context.Background(), conv, ref)
	if errors.Is(err, wire.ErrNothingToOpen) {
		return nil
	}
	if err != nil {
		t.Fatalf("Open(%s, %q): %v", conv, ref, err)
	}
	return &got
}

func mrURL(project string, iid int) string {
	return "https://" + testHost + "/" + project + "/-/merge_requests/" + strconv.Itoa(iid)
}

func TestV2OpenTakesTheMergeRequestOfTheChatsCheckout(t *testing.T) {
	o := openFixture(map[string]string{"group/sub/proj!42": "The 42nd"}, nil)
	cases := []struct{ conv, ref, tab, target string }{
		{"repo", "!42", "gitlab", mrURL("group/sub/proj", 42)},
		// What the chat's own texts link wins over the checkout, the latest first; both are offered.
		{"talk", "!42", "gitlab", mrURL("a/b", 42)},
		{"repo", "ABC-7", "task", "ABC-7"},
		{"repo", "https://tracker.example.org/issue/ABC-7", "task", "ABC-7"},
		{"repo", "https://example.com/", "browser", "https://example.com/"},
	}
	for _, c := range cases {
		got := opens(t, o, c.conv, c.ref)
		if got == nil || string(got.Tab) != c.tab || got.Target != c.target || len(got.Choices) != 0 {
			t.Errorf("Open(%s, %q) = %+v, want %s %s", c.conv, c.ref, got, c.tab, c.target)
		}
	}
	// The checkout's GitLab has to have it: a number it does not have opens nothing.
	if got := opens(t, o, "repo", "!43"); got != nil {
		t.Errorf("a merge request the GitLab does not have opens %+v", got)
	}
	if got := opens(t, o, "plain", "!42"); got != nil {
		t.Errorf("a folder without a checkout opens %+v", got)
	}
	if got := opens(t, o, "repo", "WORD-1"); got != nil {
		t.Errorf("an unknown key opens %+v", got)
	}
	if _, err := o.Open(context.Background(), "gone", "ABC-1"); err == nil || errors.Is(err, wire.ErrNothingToOpen) {
		t.Errorf("a conversation that is not here opens: %v", err)
	}
}

func TestV2OpenListsEveryProjectThatHasTheNumber(t *testing.T) {
	o := openFixture(nil, nil)
	got := opens(t, o, "talk", "!9")
	if got == nil || got.Target != mrURL("a/new", 9) || len(got.Choices) != 2 || got.Choices[1].Target != mrURL("a/old", 9) ||
		got.Choices[0].Label != "a/new" || got.Choices[0].Detail != nil {
		t.Errorf("two links of !9 open %+v", got)
	}
}

// Only the links of the texts count, and only on the deployment's GitLabs.
func TestV2OpenIgnoresLinksOfOtherServers(t *testing.T) {
	o := openFixture(nil, nil)
	if got := opens(t, o, "talk", "!77"); got != nil {
		t.Errorf("%+v", got)
	}
	o.mrLinks = func(string) []sources.MRMention {
		return []sources.MRMention{{URL: "https://evil.example/x/y/-/merge_requests/9", Host: "evil.example", Project: "x/y", IID: "9"}}
	}
	if got := opens(t, o, "talk", "!9"); got != nil {
		t.Errorf("a link to another server opens %+v", got)
	}
}

func TestV2OpenFindsTheProjectInAFolderOfRepositories(t *testing.T) {
	have := map[string]string{
		"team/harnessly!80":    "Chips that open",
		"other/harnessly!80":   "",
		"ai/model-gateway!104": "Models",
		"team/harnessly!5":     "Five",
	}
	o := openFixture(have, nil)
	o.state = &mrState{}

	// The name before the number picks the project: by the last segment, by the whole path or by the folder's name.
	single := map[string]string{
		"model-gateway !104":    mrURL("ai/model-gateway", 104),
		"ai/model-gateway !104": mrURL("ai/model-gateway", 104),
		"llm !104":              mrURL("ai/model-gateway", 104),
		"!104":                  mrURL("ai/model-gateway", 104),
		"team/harnessly !80":    mrURL("team/harnessly", 80),
		"other/harnessly !80":   mrURL("other/harnessly", 80),
		"!5":                    mrURL("team/harnessly", 5),
		"a word before !104":    "",
		"Harnessly !5":          mrURL("team/harnessly", 5),
		"unknown/project !104":  mrURL("ai/model-gateway", 104),
		"harnessly !104":        "",
		"model-gateway !999":    "",
		"!999":                  "",
	}
	for ref, want := range single {
		if !mrRef.MatchString(ref) {
			if want != "" {
				t.Errorf("%q is not a merge request ref", ref)
			}
			continue
		}
		got := opens(t, o, "umbrella", ref)
		switch {
		case want == "" && got != nil:
			t.Errorf("%q opens %+v", ref, got)
		case want != "" && (got == nil || got.Target != want || len(got.Choices) != 0):
			t.Errorf("%q opens %+v, want %s", ref, got, want)
		}
	}

	// Two projects named harnessly both have !80 ("harnessly !80" is theirs both), the one named by a path has one.
	got := opens(t, o, "umbrella", "harnessly !80")
	if got == nil || len(got.Choices) != 2 || got.Choices[0].Label != "team/harnessly" ||
		got.Choices[0].Detail == nil || *got.Choices[0].Detail != "Chips that open" || got.Choices[1].Label != "other/harnessly" ||
		got.Choices[1].Detail != nil || got.Target != got.Choices[0].Target {
		t.Errorf("harnessly !80 opens %+v", got)
	}
	// Without a name, !80 is in the same two places.
	if got := opens(t, o, "umbrella", "!80"); got == nil || len(got.Choices) != 2 {
		t.Errorf("!80 opens %+v", got)
	}
}

// The project a text names need not be checked out in the chat's folder: the GitLab's own search knows the projects
// called so, and a path is a project by itself; a word of the sentence is none.
func TestV2OpenAsksTheGitLabForAProjectNoCheckoutHas(t *testing.T) {
	have := map[string]string{"acme/tickets!7": "Seven", "elsewhere/tickets!7": "", "elsewhere/tickets!8": "Eight", "team/harnessly!5": "Five"}
	o := openFixture(have, nil)
	if got := opens(t, o, "umbrella", "tickets !7"); got == nil || len(got.Choices) != 2 || got.Choices[0].Label != "acme/tickets" {
		t.Errorf("tickets !7 opens %+v", got)
	}
	if got := opens(t, o, "umbrella", "tickets !8"); got == nil || got.Target != mrURL("elsewhere/tickets", 8) || len(got.Choices) != 0 {
		t.Errorf("tickets !8 opens %+v", got)
	}
	// Path of a project nobody checked out.
	if got := opens(t, o, "umbrella", "Acme/Tickets !7"); got == nil || got.Target != mrURL("acme/tickets", 7) {
		t.Errorf("a path opens %+v", got)
	}
	// A name the search finds nothing for goes on as if it was not written.
	if got := opens(t, o, "umbrella", "empty !5"); got == nil || got.Target != mrURL("team/harnessly", 5) {
		t.Errorf("a word of the sentence stops the search: %+v", got)
	}
	o.projects = nil
	if got := opens(t, o, "umbrella", "tickets !7"); got != nil {
		t.Errorf("without a search %+v", got)
	}
}

// Where many checkouts have the number, the merge request changed last is the first.
func TestV2OpenPutsTheRecentlyChangedFirst(t *testing.T) {
	o := openFixture(map[string]string{"team/harnessly!80": "Old", "other/harnessly!80": "New", "ai/model-gateway!80": "Oldest"}, nil)
	got := opens(t, o, "umbrella", "!80")
	if got == nil || len(got.Choices) != 3 || got.Choices[0].Label != "other/harnessly" || got.Choices[1].Label != "team/harnessly" ||
		got.Choices[2].Label != "ai/model-gateway" || got.Target != got.Choices[0].Target {
		t.Errorf("%+v", got)
	}
}

// Without a lookup nothing can be known to exist, so nothing opens.
func TestV2OpenOpensNothingItCannotVerify(t *testing.T) {
	o := openFixture(nil, nil)
	o.mr = nil
	if got := opens(t, o, "repo", "!42"); got != nil {
		t.Errorf("%+v", got)
	}
}

func TestV2OpenAllAnswersEveryRefInOrder(t *testing.T) {
	o := openFixture(map[string]string{"ai/model-gateway!104": "Models", "team/harnessly!80": ""}, nil)
	refs := []string{"harnessly !80", "!404", "model-gateway !104", "ABC-3", "example.com/not-a-ref"}
	got, err := o.OpenAll(context.Background(), "umbrella", refs)
	if err != nil || len(got) != len(refs) {
		t.Fatalf("%v %d", err, len(got))
	}
	want := []string{mrURL("team/harnessly", 80), "", mrURL("ai/model-gateway", 104), "ABC-3", ""}
	for i, w := range want {
		switch {
		case w == "" && got[i] != nil:
			t.Errorf("%q opens %+v", refs[i], got[i])
		case w != "" && (got[i] == nil || got[i].Target != w):
			t.Errorf("%q opens %+v, want %s", refs[i], got[i], w)
		}
	}
	if _, err := o.OpenAll(context.Background(), "gone", refs); err == nil {
		t.Error("a conversation that is not here opens")
	}
}

func TestMergeRequestRefGrammar(t *testing.T) {
	for ref, want := range map[string]string{
		"!12":                    "| 12",
		"harnessly !80":          "harnessly | 80",
		"group/sub/proj !7":      "group/sub/proj | 7",
		"my.proj-2 !1234":        "my.proj-2 | 1234",
		"two words !1":           "",
		"harnessly!80":           "",
		"!1234567":               "",
		"/etc !5":                "",
		"harnessly  !80":         "",
		"harnessly !80 and more": "",
	} {
		m := mrRef.FindStringSubmatch(ref)
		got := ""
		if m != nil {
			got = strings.TrimSpace(m[1]) + " | " + m[2]
			if m[1] == "" {
				got = "| " + m[2]
			}
		}
		if got != want {
			t.Errorf("%q = %q, want %q", ref, got, want)
		}
	}
}

func TestRemoteProject(t *testing.T) {
	for raw, want := range map[string]string{
		"https://gitlab.example.org/g/p.git":        "gitlab.example.org g/p",
		"ssh://git@gitlab.example.org:2222/g/p.git": "gitlab.example.org g/p",
		"git@gitlab.example.org:g/p":                "gitlab.example.org g/p",
		"/local/path":                               " ",
	} {
		h, p := remoteProject(raw)
		if h+" "+p != want {
			t.Errorf("remoteProject(%q) = %q %q", raw, h, p)
		}
	}
}
