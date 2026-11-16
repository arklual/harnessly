package docs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
)

// fakeServices are a documents and a boards service that keep what the hub makes, and say what they were asked.
type fakeServices struct {
	mu     sync.Mutex
	docs   map[string]map[string]any // slug → {title, markdown, html, author}
	boards map[string]map[string]any // slug → {title, author, trashed}
	public map[string]bool
	calls  []string
	// trashed is what the documents service was told of the trash; down makes it refuse to hear it.
	trashed map[string]bool
	down    bool
	old     bool // a documents service without the trash route
}

func newFakeServices(t *testing.T, s *Service) *fakeServices {
	f := &fakeServices{docs: map[string]map[string]any{"plan": {"title": "plan", "markdown": "# plan\n\nтекст"}},
		boards: map[string]map[string]any{"wall": {"title": "wall", "author": "author@example.com"}}, public: map[string]bool{}, trashed: map[string]bool{}}
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		body := map[string]any{}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.calls = append(f.calls, "art "+r.Method+" "+r.URL.Path)
		slug := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/"), "/content"), "/access")
		switch {
		case f.old && r.Method == "GET" && r.URL.Path == "/api/v1/artifacts/plan":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifact": map[string]any{"slug": "plan"}})
		case f.old && strings.HasSuffix(r.URL.Path, "/trash"):
			http.NotFound(w, r)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/trash"):
			if f.down || r.Header.Get("Authorization") != "Bearer t" {
				http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/"), "/trash")
			f.trashed[slug] = body["trashed"] == true
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": slug, "trashed": f.trashed[slug]})
		case r.Method == "GET" && r.URL.Path == "/api/v1/artifacts":
			items := []artifact{}
			for slug, d := range f.docs {
				items = append(items, artifact{Slug: slug, Title: d["title"].(string)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case r.Method == "POST" && r.URL.Path == "/api/v1/artifacts":
			if body["new"] != true {
				http.Error(w, `{"error":"duplicate"}`, http.StatusConflict)
				return
			}
			slug := "doc-" + string(rune('a'+len(f.docs)))
			f.docs[slug] = body
			_ = json.NewEncoder(w).Encode(map[string]any{"created": true, "version": map[string]any{"n": 1}, "artifact": map[string]any{"slug": slug}})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/content"):
			_ = json.NewEncoder(w).Encode(map[string]any{"markdown": f.docs[slug]["markdown"], "html": f.docs[slug]["html"]})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/access"):
			_ = json.NewEncoder(w).Encode(map[string]any{"public": f.public[slug]})
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/access"):
			f.public[slug] = body["public"] == true
			_ = json.NewEncoder(w).Encode(map[string]any{"public": f.public[slug]})
		case r.Method == "PATCH":
			f.docs[slug]["title"] = body["title"]
			_ = json.NewEncoder(w).Encode(map[string]any{"artifact": map[string]any{"slug": slug}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(art.Close)
	boards := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, `{"error":"token"}`, http.StatusUnauthorized)
			return
		}
		body := map[string]any{}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.calls = append(f.calls, "boards "+r.Method+" "+r.URL.Path)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/b/api/boards/"), "/")
		switch {
		case r.Method == "GET" && r.URL.Path == "/b/api/boards":
			list := []board{}
			for slug, b := range f.boards {
				if b["trashed"] != true {
					list = append(list, board{Slug: slug, Title: b["title"].(string), Author: b["author"].(string)})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"boards": list})
		case r.Method == "POST" && r.URL.Path == "/b/api/boards":
			slug := "board-" + string(rune('a'+len(f.boards)))
			f.boards[slug] = map[string]any{"title": body["title"], "author": body["author"]}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": slug})
		case r.Method == "POST" && len(parts) == 2 && parts[1] == "copy":
			slug := "board-" + string(rune('a'+len(f.boards)))
			f.boards[slug] = map[string]any{"title": body["title"], "author": body["author"]}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": slug})
		case r.Method == "POST" && len(parts) == 2 && (parts[1] == "trash" || parts[1] == "restore"):
			f.boards[parts[0]]["trashed"] = parts[1] == "trash"
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/meta"):
			slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/b/"), "/meta")
			f.boards[slug]["title"] = body["title"]
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(boards.Close)
	s.art = newArtifacts(art.URL, "t", nil)
	s.cfg.ArtifactsURL = art.URL
	s.cfg.BoardsURL = boards.URL
	return f
}

// Documents and boards are made, copied, renamed and put into the trash through the hub with the boards' rights; a
// thing in the trash opens for nobody until its owner takes it out.
func TestMakeCopyRenameTrashAndRestore(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	f := newFakeServices(t, s)
	call := func(h func(http.ResponseWriter, *http.Request, *auth.Principal) error, person, slug, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		if slug != "" {
			r.SetPathValue("slug", slug)
		}
		w := httptest.NewRecorder()
		if err := h(w, r, who[person]); err != nil {
			s.answer(w, r, err)
		}
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	slugOf := func(out map[string]any) string {
		t.Helper()
		doc, _ := out["doc"].(map[string]any)
		slug, _ := doc["slug"].(string)
		if slug == "" {
			t.Fatalf("no doc in %v", out)
		}
		return slug
	}

	// A document from a template, owned by the mate, in the mate's space, edited by the space.
	code, out := call(s.createRoute, "mate", "", `{"template":"plan"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	doc := slugOf(out)
	if f.docs[doc]["markdown"] == nil || !strings.HasPrefix(f.docs[doc]["markdown"].(string), "# План работ") || f.docs[doc]["author"] != "mate@example.com" {
		t.Fatalf("published: %v", f.docs[doc])
	}
	if code, _ := call(s.createRoute, "stranger", "", `{"kind":"artifact","title":"x"}`); code != http.StatusCreated {
		t.Fatalf("a stranger makes one in their own space: %d", code)
	}
	if code, _ := call(s.createRoute, "admin", "", `{"title":"x"}`); code != http.StatusForbidden {
		t.Fatalf("an administrator in no space without spaceId: %d", code)
	}
	if code, _ := call(s.createRoute, "mate", "", `{"kind":"page","title":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("an unknown kind: %d", code)
	}

	// Renaming needs the right to edit: the space edits, a stranger does not even see it.
	if code, out := call(s.renameRoute, "author", doc, `{"title":"План на квартал"}`); code != 200 || f.docs[doc]["title"] != "План на квартал" {
		t.Fatalf("rename by the space: %d %v", code, out)
	}
	if code, _ := call(s.renameRoute, "stranger", doc, `{"title":"Чужое"}`); code < 400 {
		t.Fatalf("a stranger renames: %d", code)
	}

	// Only the owner or an administrator puts it into the trash; then it opens for nobody and leaves the list.
	if code, _ := call(s.trashRoute, "author", doc, ``); code != http.StatusForbidden {
		t.Fatalf("not the owner trashes: %d", code)
	}
	f.public[doc] = true
	if code, out := call(s.trashRoute, "mate", doc, ``); code != 200 || f.public[doc] || out["trashedBy"] != "mate" || out["kind"] != "artifact" {
		t.Fatalf("trash: %d %v", code, out)
	}
	if code, _ := call(s.detail, "mate", doc, ``); code != http.StatusNotFound {
		t.Fatalf("a trashed document opens: %d", code)
	}
	list, _ := s.summaries(t.Context(), who["admin"], "")
	for _, d := range list {
		if d.Slug == doc {
			t.Fatal("a trashed document is listed")
		}
	}
	rights, _, err := s.rightsOf(httptest.NewRequest("POST", "/", nil), rightsRequest{Sub: who["mate"].User.ID, Slugs: []string{doc}})
	if err != nil || rights[doc] != RightNone {
		t.Fatalf("the documents service is told %v %v", rights, err)
	}
	for person, want := range map[string]int{"mate": 1, "author": 0} {
		_, out := call(s.trashList, person, "", ``)
		if got := len(out["docs"].([]any)); got != want {
			t.Fatalf("%s sees %d in the trash: %v", person, got, out)
		}
	}
	if code, _ := call(s.restoreRoute, "author", doc, ``); code < 400 {
		t.Fatalf("not the owner restores: %d", code)
	}
	if code, out := call(s.restoreRoute, "mate", doc, ``); code != 200 || slugOf(out) != doc {
		t.Fatalf("restore: %d %v", code, out)
	}
	if code, _ := call(s.detail, "author", doc, ``); code != 200 {
		t.Fatalf("a restored document does not open: %d", code)
	}

	// A copy needs the right to read and belongs to whoever made it.
	code, out = call(s.duplicateRoute, "author", doc, ``)
	if code != http.StatusCreated {
		t.Fatalf("duplicate: %d %v", code, out)
	}
	copied := slugOf(out)
	if f.docs[copied]["title"] != "План на квартал (копия)" || f.docs[copied]["markdown"] != f.docs[doc]["markdown"] {
		t.Fatalf("the copy: %v", f.docs[copied])
	}
	if owner := out["doc"].(map[string]any)["owner"].(map[string]any)["email"]; owner != "author@example.com" {
		t.Fatalf("the copy's owner: %v", owner)
	}

	// Boards go the same way, through the boards service with the hub's token.
	code, out = call(s.createRoute, "mate", "", `{"kind":"board","title":"Схема","template":"retro"}`)
	if code != http.StatusCreated {
		t.Fatalf("a board: %d %v", code, out)
	}
	b := slugOf(out)
	if code, _ := call(s.duplicateRoute, "mate", b, `{"title":"Схема 2"}`); code != http.StatusCreated {
		t.Fatalf("a board's copy: %d", code)
	}
	if code, _ := call(s.trashRoute, "mate", b, ``); code != 200 || f.boards[b]["trashed"] != true {
		t.Fatalf("a board into the trash: %d %v", code, f.boards[b])
	}
	if code, _ := call(s.restoreRoute, "mate", b, ``); code != 200 || f.boards[b]["trashed"] != false {
		t.Fatalf("a board out of the trash: %d %v", code, f.boards[b])
	}
	joined := strings.Join(f.calls, "\n")
	for _, want := range []string{"boards POST /b/api/boards\n", "boards POST /b/api/boards/" + b + "/copy", "boards POST /b/api/boards/" + b + "/trash", "boards POST /b/api/boards/" + b + "/restore"} {
		if !strings.Contains(joined+"\n", want) {
			t.Fatalf("the boards service was not asked %q:\n%s", want, joined)
		}
	}
}

// A service acts for a person with the shared secret and the person's platform token the hub signed for that
// service; the secret alone, a token for another audience or of nobody name nobody, and a service never acts as an
// administrator.
func TestServiceActsForAPerson(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	newFakeServices(t, s)
	s.cfg.ArtifactsAccessToken = "svc-secret"
	aud := strings.TrimRight(s.art.base, "/")
	s.appToken = func(token, audience string) (string, error) {
		if audience != aud || !strings.HasPrefix(token, "signed:") {
			return "", errors.New("invalid")
		}
		return strings.TrimPrefix(token, "signed:"), nil
	}
	send := func(h func(http.ResponseWriter, *http.Request, *auth.Principal) error, method, slug, token, person string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/platform/docs", strings.NewReader(`{"title":"Из приложения"}`))
		if slug != "" {
			r.SetPathValue("slug", slug)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(tokenHeader, person)
		w := httptest.NewRecorder()
		s.asPerson(h).ServeHTTP(w, r)
		return w
	}
	if w := send(s.createRoute, "POST", "", "svc-secret", "signed:"+who["mate"].User.ID); w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"email":"mate@example.com"`) {
		t.Fatalf("the service for the mate: %d %s", w.Code, w.Body.String())
	}
	for _, person := range []string{"signed:nobody", who["mate"].User.ID, "forged:" + who["mate"].User.ID} {
		if w := send(s.createRoute, "POST", "", "svc-secret", person); w.Code != http.StatusUnauthorized {
			t.Fatalf("%q: %d", person, w.Code)
		}
	}
	// The administrator trashes anything by themselves; a service with the administrator's token cannot.
	if w := send(s.trashRoute, "DELETE", "plan", "svc-secret", "signed:"+who["admin"].User.ID); w.Code < 400 {
		t.Fatalf("a service acted as an administrator: %d %s", w.Code, w.Body.String())
	}
	if s.serviceToken("wrong") || s.serviceToken("") {
		t.Fatal("a wrong secret passes for a service")
	}
}

// The documents service learns of the trash from the hub: when it does not answer, the trash waits and the sweep
// sends the current state again until the service confirms it; the agent of the owner's machine uses the same trash.
func TestTrashReachesTheDocumentsService(t *testing.T) {
	s, who, logs := circleRig(t, true, false)
	f := newFakeServices(t, s)
	ctx := context.Background()
	call := func(h func(http.ResponseWriter, *http.Request, *auth.Principal) error, slug string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/x", nil)
		r.SetPathValue("slug", slug)
		w := httptest.NewRecorder()
		if err := h(w, r, who["author"]); err != nil {
			s.answer(w, r, err)
		}
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	waiting := func() bool {
		t.Helper()
		var n int
		if err := s.db.S().Q().QueryRow(ctx, `SELECT COUNT(*) FROM doc_meta WHERE slug = 'plan' AND trash_sync IS NOT NULL`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	// Heard at once.
	if code, out := call(s.trashRoute, "plan"); code != 200 || out["serviceConfirmed"] != true || !f.trashed["plan"] || waiting() {
		t.Fatalf("trash: %d %v %v", code, out, f.trashed)
	}
	if code, _ := call(s.restoreRoute, "plan"); code != 200 || f.trashed["plan"] || waiting() {
		t.Fatalf("restore: %d %v", code, f.trashed)
	}

	// Not heard: the hub hides it anyway, says so in the log, and the sweep tells the service when it is back.
	f.down = true
	if code, out := call(s.trashRoute, "plan"); code != 200 || out["serviceConfirmed"] != false || f.trashed["plan"] || !waiting() {
		t.Fatalf("trash while the service is down: %d %v", code, out)
	}
	if !strings.Contains(logs.String(), "did not confirm the trash") {
		t.Fatalf("the failure is not in the log: %s", logs)
	}
	s.syncTrash(ctx)
	if !waiting() || !strings.Contains(logs.String(), "still does not confirm") {
		t.Fatal("the sweep forgot a trash the service did not hear")
	}
	f.down = false
	s.syncTrash(ctx)
	if !f.trashed["plan"] || waiting() {
		t.Fatalf("the sweep did not tell the service: %v", f.trashed)
	}

	// Out of the trash while the service is down, then back: the service hears the latest state.
	f.down = true
	if code, _ := call(s.restoreRoute, "plan"); code != 200 || !waiting() {
		t.Fatalf("restore while the service is down: %d", code)
	}
	f.down = false
	s.syncTrash(ctx)
	if f.trashed["plan"] || waiting() {
		t.Fatalf("the service still has it in the trash: %v", f.trashed)
	}

	// A service without the trash route answers 404 as for a document it does not have: the hub keeps waiting.
	f.old = true
	if code, out := call(s.trashRoute, "plan"); code != 200 || out["serviceConfirmed"] != false || !waiting() {
		t.Fatalf("trash on an old service: %d %v", code, out)
	}
	f.old = false
	if code, _ := call(s.restoreRoute, "plan"); code != 200 || f.trashed["plan"] || waiting() {
		t.Fatalf("restore after the old service: %d", code)
	}

	// The agent of the owner's machine puts the owner's document into the trash and takes it out; nobody else's.
	owner := Who{OwnerID: who["author"].User.ID}
	if rep := s.acl(ctx, Who{OwnerID: who["mate"].User.ID}, aclEvent{Slug: "plan", Route: "/plan/trash", Body: json.RawMessage(`{"trashed":true}`)}); rep.Status != http.StatusForbidden {
		t.Fatalf("not the owner's machine: %d", rep.Status)
	}
	if rep := s.acl(ctx, owner, aclEvent{Slug: "plan", Route: "/plan/trash", Body: json.RawMessage(`{}`)}); rep.Status != http.StatusBadRequest {
		t.Fatalf("no trashed: %d", rep.Status)
	}
	if rep := s.acl(ctx, owner, aclEvent{Slug: "plan", Route: "/plan/trash", Body: json.RawMessage(`{"trashed":true}`)}); rep.Status != 200 || !f.trashed["plan"] {
		t.Fatalf("the agent trashes: %d %v", rep.Status, rep.Body)
	}
	if code, _ := call(s.detail, "plan"); code != http.StatusNotFound {
		t.Fatalf("a document the agent trashed opens: %d", code)
	}
	if rep := s.acl(ctx, owner, aclEvent{Slug: "plan", Route: "/plan/access", Body: json.RawMessage(`{"public":true}`)}); rep.Status != http.StatusNotFound {
		t.Fatalf("a trashed document is shared: %d", rep.Status)
	}
	if rep := s.acl(ctx, owner, aclEvent{Slug: "plan", Route: "/plan/trash", Body: json.RawMessage(`{"trashed":false}`)}); rep.Status != 200 || f.trashed["plan"] {
		t.Fatalf("the agent restores: %d %v", rep.Status, rep.Body)
	}
}
