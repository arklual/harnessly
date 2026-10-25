package wire_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const openConv = "tab-1"

var errGone = errors.New("gone")

func openRefsQuery(refs ...string) string {
	q := url.Values{"ref": refs}
	return "/api/v2/conversations/" + openConv + "/open-refs?" + q.Encode()
}

// GET .../open-refs answers each ref in the order asked: where it opens, several places as choices, or null. Without
// OpenAll the refs are asked of Open one by one; a conversation that is not here is a 404; a bad query is a 400.
func TestOpenRefsAnswersEveryRefAsked(t *testing.T) {
	open := func(_ context.Context, conv, ref string) (wire.OpenTarget, error) {
		switch {
		case conv != openConv:
			return wire.OpenTarget{}, errGone
		case ref == "!1":
			return wire.OpenTarget{Tab: wire.OpenGitLab, Target: "https://gitlab.example.com/g/p/-/merge_requests/1"}, nil
		case ref == "!2":
			a := wire.OpenChoice{Tab: wire.OpenGitLab, Target: "https://gitlab.example.com/g/a/-/merge_requests/2", Label: "g/a"}
			b := wire.OpenChoice{Tab: wire.OpenGitLab, Target: "https://gitlab.example.com/g/b/-/merge_requests/2", Label: "g/b"}
			return wire.OpenTarget{Tab: a.Tab, Target: a.Target, Choices: []wire.OpenChoice{a, b}}, nil
		}
		return wire.OpenTarget{}, wire.ErrNothingToOpen
	}
	srv := &wire.Server{Open: open, NotFound: func(err error) bool { return err == errGone }}

	code, res := meCall(t, srv, "GET", openRefsQuery("!1", "!2", "!3"), "")
	refs, _ := res["refs"].([]any)
	if code != 200 || len(refs) != 3 {
		t.Fatalf("%d %v", code, res)
	}
	first, second, third := refs[0].(map[string]any), refs[1].(map[string]any), refs[2].(map[string]any)
	if first["ref"] != "!1" || first["open"].(map[string]any)["tab"] != "gitlab" || first["open"].(map[string]any)["choices"] != nil {
		t.Errorf("first: %v", first)
	}
	if choices, _ := second["open"].(map[string]any)["choices"].([]any); len(choices) != 2 || choices[1].(map[string]any)["label"] != "g/b" ||
		choices[1].(map[string]any)["detail"] != nil {
		t.Errorf("second: %v", second)
	}
	if third["ref"] != "!3" || third["open"] != nil {
		t.Errorf("third: %v", third)
	}

	// OpenAll takes over when there is one.
	srv.OpenAll = func(_ context.Context, _ string, refs []string) ([]*wire.OpenTarget, error) {
		return make([]*wire.OpenTarget, len(refs)), nil
	}
	if code, res := meCall(t, srv, "GET", openRefsQuery("!1"), ""); code != 200 || res["refs"].([]any)[0].(map[string]any)["open"] != nil {
		t.Errorf("OpenAll: %d %v", code, res)
	}
	srv.OpenAll = func(context.Context, string, []string) ([]*wire.OpenTarget, error) { return nil, nil }
	if code, _ := meCall(t, srv, "GET", openRefsQuery("!1"), ""); code != 500 {
		t.Errorf("a short answer: %d", code)
	}
	srv.OpenAll = nil

	if code, _ := meCall(t, srv, "GET", strings.Replace(openRefsQuery("!1"), openConv, "tab-2", 1), ""); code != 404 {
		t.Errorf("a conversation that is not here: %d", code)
	}
	many := make([]string, wire.OpenRefsMax+1)
	for i := range many {
		many[i] = "!1"
	}
	for name, path := range map[string]string{
		"no ref":       "/api/v2/conversations/" + openConv + "/open-refs",
		"empty ref":    openRefsQuery(""),
		"too long":     openRefsQuery("!" + strings.Repeat("1", wire.OpenRefMax)),
		"too many":     openRefsQuery(many...),
		"invalid utf8": "/api/v2/conversations/" + openConv + "/open-refs?ref=%ff",
	} {
		if code, _ := meCall(t, srv, "GET", path, ""); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := meCall(t, &wire.Server{}, "GET", openRefsQuery("!1"), ""); code != 404 {
		t.Errorf("no Open: %d", code)
	}
}

// An answer that breaks its own contract is not sent.
func TestOpenTargetChoicesAreCheckedBeforeTheyGoOut(t *testing.T) {
	a := wire.OpenChoice{Tab: wire.OpenGitLab, Target: "https://gitlab.example.com/g/a/-/merge_requests/2", Label: "g/a"}
	b := wire.OpenChoice{Tab: wire.OpenGitLab, Target: "https://gitlab.example.com/g/b/-/merge_requests/2", Label: "g/b"}
	good := wire.OpenTarget{Tab: a.Tab, Target: a.Target, Choices: []wire.OpenChoice{a, b}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]wire.OpenTarget{
		"one choice":   {Tab: a.Tab, Target: a.Target, Choices: []wire.OpenChoice{a}},
		"not first":    {Tab: a.Tab, Target: b.Target, Choices: []wire.OpenChoice{a, b}},
		"no label":     {Tab: a.Tab, Target: a.Target, Choices: []wire.OpenChoice{a, {Tab: b.Tab, Target: b.Target}}},
		"unknown tab":  {Tab: a.Tab, Target: a.Target, Choices: []wire.OpenChoice{a, {Tab: "nowhere", Target: b.Target, Label: "x"}}},
		"nine choices": {Tab: a.Tab, Target: a.Target, Choices: []wire.OpenChoice{a, b, b, b, b, b, b, b, b}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%s passes", name)
		}
	}
}
