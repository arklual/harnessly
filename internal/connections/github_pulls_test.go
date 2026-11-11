package connections

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubOpenPullsListsEachOnceWithThePersonsPart(t *testing.T) {
	var asked map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graphql" || r.Header.Get("Authorization") != "Bearer ghp_good" {
			provJSON(w, 401, `{"message":"Bad credentials"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &in)
		asked = in.Variables
		provJSON(w, 200, `{"data":{"viewer":{"login":"alice","name":"Alice"},
"waiting":{"nodes":[{"number":7,"title":"Fix it","url":"https://gh.example/acme/app/pull/7","isDraft":false,
  "createdAt":"2026-11-10T10:00:00Z","updatedAt":"2026-11-11T10:00:00Z","baseRefName":"main","headRefName":"fix",
  "mergeable":"CONFLICTING","totalCommentsCount":3,"author":{"login":"bob","name":"Bob"},"repository":{"nameWithOwner":"acme/app"},
  "assignees":{"nodes":[{"login":"alice","name":"Alice"}]},
  "reviewRequests":{"nodes":[{"requestedReviewer":{}},{"requestedReviewer":{"login":"carol"}}]},
  "labels":{"nodes":[{"name":"bug"}]},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},{}]},
"mine":{"nodes":[{"number":8,"title":"Mine","url":"https://gh.example/acme/app/pull/8","isDraft":true,
  "author":{"login":"Alice"},"repository":{"nameWithOwner":"acme/app"},"commits":{"nodes":[]}}]},
"all":{"nodes":[{"number":7,"title":"Fix it","url":"https://gh.example/acme/app/pull/7","author":{"login":"bob"}}]}}}`)
	}))
	t.Cleanup(srv.Close)
	src := gitHubProvider{}.Open(Conn{Kind: GitHub, BaseURL: srv.URL, APIURL: srv.URL + "/api/v3", Deployment: SelfHosted},
		Credentials{FieldToken: "ghp_good"}, NewHTTP(nil)).(PullLister)
	me, list, err := src.OpenPulls(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me == nil || me.Login != "alice" || len(list) != 2 {
		t.Fatalf("%+v %+v", me, list)
	}
	if asked["w"] != "is:pr is:open archived:false review-requested:@me" {
		t.Errorf("waiting search %v", asked["w"])
	}
	fix := list[0]
	if !fix.Asked || fix.Mine || !fix.Assigned || !fix.Conflicts || fix.Checks != "failed" || fix.Project != "acme/app" ||
		fix.Target != "main" || fix.Source != "fix" || len(fix.Reviewers) != 1 || fix.Author.Name != "Bob" || fix.Comments != 3 {
		t.Errorf("waiting %+v", fix)
	}
	if mine := list[1]; !mine.Mine || mine.Asked || !mine.Draft || mine.Checks != "" {
		t.Errorf("mine %+v", mine)
	}
}
