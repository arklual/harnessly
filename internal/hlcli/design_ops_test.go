package hlcli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDesignClaimsHistoryAndPanel(t *testing.T) {
	cases := []struct {
		args         []string
		method, path string
		query        string
		body         string
		answer       string
		out          string
	}{
		{[]string{"design", "claim", "shop", "Main", "page"}, "POST", "/api/cli/design/claims", "",
			`{"action":"claim","board":"Main page","project":"shop","session":"hs-me"}`,
			`{"project":"shop","board":"Main page","refreshed":true}`, "is yours (claim extended)"},
		{[]string{"design", "release"}, "POST", "/api/cli/design/claims", "", `{"action":"release","session":"hs-me"}`,
			`{"released":[{"project":"shop","board":"A"}]}`, "Claims released: 1 (shop/A)"},
		{[]string{"design", "release", "shop", "A"}, "POST", "/api/cli/design/claims", "", `{"action":"release","board":"A","project":"shop","session":"hs-me"}`,
			`{"released":[]}`, "held no claims"},
		{[]string{"design", "who", "shop"}, "GET", "/api/cli/design/claims", "project=shop", "",
			`{"claims":[{"project":"shop","board":"A","person":"Ann","runtime":"claude","chatTitle":"Fix\u001b[0m","since":0}]}`, "chat \"Fix␛[0m\""},
		{[]string{"design", "history", "shop", "Main", "-n", "5"}, "GET", "/api/cli/design/history", "board=Main&n=5&project=shop", "",
			`{"unsaved":true,"lines":[{"shortId":"abc1234","at":0,"who":"Ann","subject":"edit"}]}`, "abc1234"},
		{[]string{"design", "restore", "shop", "Main", "--at", "30m"}, "POST", "/api/cli/design/history", "",
			`{"action":"restore","at":"30m","board":"Main","project":"shop","session":"hs-me"}`,
			`{"board":"Main","revision":"abcdef0123"}`, "back to version abcdef0"},
		{[]string{"design", "state", "shop"}, "GET", "/api/cli/design", "state=shop", "", `{"artifact":"shop","shown":{"state":{"board":"Main"}}}`, `"board": "Main"`},
		{[]string{"design", "logs", "shop"}, "GET", "/api/cli/design", "logs=shop", "", `{"entries":[{"at":0,"kind":"error","page":"p","message":"boom"}]}`, "boom"},
		{[]string{"design", "list"}, "GET", "/api/cli/design", "", "", `{"rules":"/r.md","artifacts":[{"id":"shop","title":"Shop","boards":3,"projectPath":"/p"}]}`, "shop\t3 boards"},
		{[]string{"design", "panel", "shop", "show", "--board", "Main", "--set", "n=3", "--set", "on=true", "--set", "t=hi"}, "POST", "/api/cli/design", "",
			`{"artifact":"shop","control":{"board":"Main","panel":"show","props":{"n":3,"on":true,"t":"hi"}}}`,
			`{"pages":1,"shown":null}`, "reached the open previews: 1"},
	}
	for _, tc := range cases {
		var last seen
		env := previewServer(t, tc.answer, &last)
		env["HARNESSLY_SESSION"] = "hs-me"
		r := runWith(t, t.TempDir(), env, "", tc.args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", tc.args, r)
		}
		if last.method != tc.method || last.path != tc.path || last.query != tc.query {
			t.Fatalf("%v: sent %+v", tc.args, last)
		}
		if tc.body != "" {
			got, _ := json.Marshal(last.body)
			if string(got) != tc.body {
				t.Fatalf("%v: body %s, want %s", tc.args, got, tc.body)
			}
		}
		if !strings.Contains(r.out, tc.out) {
			t.Fatalf("%v: missing %q in\n%s", tc.args, tc.out, r.out)
		}
	}
}

func TestDesignClaimNeedsPane(t *testing.T) {
	var last seen
	env := previewServer(t, `{}`, &last)
	if r := runWith(t, t.TempDir(), env, "", "design", "claim", "shop", "A"); r.code == 0 || last.path != "" {
		t.Fatalf("%+v %+v", r, last)
	}
}
