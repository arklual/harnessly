package docs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func askRights(t *testing.T, h http.Handler, token, body string) (int, map[string]string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/platform/docs/_rights", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out struct {
		Rights map[string]string `json:"rights"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out.Rights, w.Header()
}

// The documents service is told each person's right on each document by the same model as the person's own page.
func TestRightsForTheDocumentsService(t *testing.T) {
	s, who, _ := circleRig(t, false, false)
	s.cfg.ArtifactsAccessToken = "service-secret"
	h := s.Handler()
	ask := func(sub string) map[string]string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"sub": sub, "slugs": []string{"plan", "private", "agent", "missing", "plan"}})
		code, rights, _ := askRights(t, h, "service-secret", string(body))
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", sub, code)
		}
		return rights
	}
	for name, want := range map[string]map[string]string{
		// The owner manages their documents: the service hears edit.
		"author": {"plan": RightEdit, "private": RightEdit, "agent": RightView, "missing": RightNone},
		// A member of the owner's team gets the team level; a closed document stays closed.
		"mate": {"plan": RightComment, "private": RightNone, "agent": RightView, "missing": RightNone},
		// Someone of another team gets nothing.
		"stranger": {"plan": RightNone, "private": RightNone, "agent": RightNone, "missing": RightNone},
	} {
		got := ask(who[name].User.ID)
		if len(got) != len(want) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
		for slug, r := range want {
			if got[slug] != r {
				t.Errorf("%s on %s: %q, want %q", name, slug, got[slug], r)
			}
		}
	}
	// A person the hub does not know has no rights.
	if got := ask("nobody"); got["plan"] != RightNone || got["agent"] != RightNone {
		t.Errorf("unknown person: %v", got)
	}
}

func TestRightsNeedTheServiceSecret(t *testing.T) {
	s, who, _ := circleRig(t, false, false)
	body := `{"sub":"` + who["author"].User.ID + `","slugs":["plan"]}`
	// Without the secret set on the hub, the route is not there.
	if code, _, _ := askRights(t, s.Handler(), "anything", body); code != http.StatusNotFound {
		t.Errorf("no secret on the hub: %d, want 404", code)
	}
	s.cfg.ArtifactsAccessToken = "service-secret"
	h := s.Handler()
	for _, tok := range []string{"", "wrong", "t"} {
		code, rights, hdr := askRights(t, h, tok, body)
		if code != http.StatusUnauthorized || rights != nil {
			t.Errorf("token %q: %d %v, want 401", tok, code, rights)
		}
		if !strings.Contains(hdr.Get("WWW-Authenticate"), "invalid_token") {
			t.Errorf("token %q: WWW-Authenticate %q", tok, hdr.Get("WWW-Authenticate"))
		}
	}
	many := make([]string, maxRightsSlugs+1)
	for i := range many {
		many[i] = "d"
	}
	raw, _ := json.Marshal(map[string]any{"sub": who["author"].User.ID, "slugs": many})
	if code, _, _ := askRights(t, h, "service-secret", string(raw)); code != http.StatusBadRequest {
		t.Errorf("%d slugs: %d, want 400", len(many), code)
	}
	if code, _, _ := askRights(t, h, "service-secret", `{"slugs":["plan"]}`); code != http.StatusBadRequest {
		t.Errorf("no sub: %d, want 400", code)
	}
}

// A document the hub has no record of (published straight to the documents service) is decided by the author the
// service names, as in the hub's own list: its author sees it in the documents app, a stranger does not.
func TestRightsOfADocumentWithoutARecordFollowItsAuthor(t *testing.T) {
	s, who, _ := circleRig(t, false, false)
	s.cfg.ArtifactsAccessToken = "service-secret"
	h := s.Handler()
	ask := func(sub string, authors map[string]string) map[string]string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"sub": sub, "slugs": []string{"loose"}, "authors": authors})
		code, rights, _ := askRights(t, h, "service-secret", string(body))
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", sub, code)
		}
		return rights
	}
	named := map[string]string{"loose": who["author"].User.Email}
	if got := ask(who["author"].User.ID, named)["loose"]; got == RightNone || got == "" {
		t.Errorf("author of a document without a record: %q, want a right to read it", got)
	}
	if got := ask(who["stranger"].User.ID, named)["loose"]; got != RightNone {
		t.Errorf("stranger: %q, want none", got)
	}
	// Without the author nothing changes: closed.
	if got := ask(who["author"].User.ID, nil)["loose"]; got != RightNone {
		t.Errorf("no author named: %q, want none", got)
	}
}

// A board drawn straight on the boards service has no record on the hub. Its «Поделиться» opens all the same, and what
// its owner sets there is what the boards service is told from then on.
func TestABoardWithoutARecordIsShared(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	s.cfg.ArtifactsAccessToken = "service-secret"
	acl := func(p string, method, rest, body string) *httptest.ResponseRecorder {
		t.Helper()
		slug, tail, _ := strings.Cut(rest, "/")
		req := httptest.NewRequest(method, "/api/platform/docs/"+rest, strings.NewReader(body))
		req.SetPathValue("slug", slug)
		req.SetPathValue("rest", tail)
		w := httptest.NewRecorder()
		if err := s.aclRoute(w, req, who[p]); err != nil {
			s.answer(w, req, err)
		}
		return w
	}
	if w := acl("author", http.MethodGet, "wall", ""); w.Code != http.StatusOK {
		t.Fatalf("the access of a board without a record: %d %s", w.Code, w.Body.String())
	}
	if w := acl("author", http.MethodGet, "nowhere", ""); w.Code != http.StatusNotFound {
		t.Fatalf("a slug neither service has: %d, want 404", w.Code)
	}
	known := func(sub string) (map[string]string, []string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"sub": sub, "slugs": []string{"wall"}})
		req := httptest.NewRequest(http.MethodPost, "/api/platform/docs/_rights", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer service-secret")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		var out struct {
			Rights map[string]string `json:"rights"`
			Known  []string          `json:"known"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusOK {
			t.Fatalf("rights: %d %s", w.Code, w.Body.String())
		}
		return out.Rights, out.Known
	}
	// Nobody shared it yet: the boards service keeps its own rules.
	if _, k := known(who["stranger"].User.ID); len(k) != 0 {
		t.Fatalf("an unshared board is known: %v", k)
	}
	if w := acl("author", http.MethodPut, "wall/people/stranger@example.com", `{"role":"view"}`); w.Code >= 300 {
		t.Fatalf("share with a person: %d %s", w.Code, w.Body.String())
	}
	rights, k := known(who["stranger"].User.ID)
	if len(k) != 1 || rights["wall"] != RightView {
		t.Fatalf("the person it was shared with: %v %v, want view of a known board", rights, k)
	}
}
