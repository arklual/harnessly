package docs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
)

// the boards service asks the hub with its own secret, names a person by a proved address and hears which
// boards the hub keeps a record of.
func TestRightsForTheBoardsServiceByProvedAddress(t *testing.T) {
	s, who, _ := circleRig(t, false, false)
	s.cfg.BoardEventsSecret = "boards-secret"
	h := s.Handler()
	ask := func(body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/platform/docs/_rights", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer boards-secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	body, _ := json.Marshal(map[string]any{"email": strings.ToUpper(who["mate"].User.Email), "slugs": []string{"plan", "private", "missing"}})
	code, out := ask(string(body))
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	rights, _ := out["rights"].(map[string]any)
	if rights["plan"] != RightComment || rights["private"] != RightNone || rights["missing"] != RightNone {
		t.Errorf("the mate by address: %v", rights)
	}
	known, _ := out["known"].([]any)
	if len(known) != 2 || known[0] != "plan" || known[1] != "private" {
		t.Errorf("known: %v, want plan and private", known)
	}
	// An address nobody proved names nobody, but the hub still says what it knows.
	code, out = ask(`{"email":"nobody@example.com","slugs":["plan"]}`)
	rights, _ = out["rights"].(map[string]any)
	known, _ = out["known"].([]any)
	if code != http.StatusOK || rights["plan"] != RightNone || len(known) != 1 {
		t.Errorf("unknown address: %d %v", code, out)
	}
	// The documents' secret is not the only one, and neither opens the route when wrong.
	req := httptest.NewRequest(http.MethodPost, "/api/platform/docs/_rights", strings.NewReader(`{"sub":"x","slugs":["plan"]}`))
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("a wrong secret: %d", w.Code)
	}
}

func TestGuestLinkOfABoardOpensOnTheBoardsService(t *testing.T) {
	s, mux, _ := accessRig(t)
	s.cfg.BoardEventsSecret, s.cfg.BoardsPublicURL = "boards-secret", "https://boards.example.com/"
	owner := resaccess.Actor{Email: "author@example.com", Name: "Author"}
	link := func(slug, target, rights string) string {
		t.Helper()
		w := httptest.NewRecorder()
		s.access.Serve(w, httptest.NewRequest("POST", "/x", strings.NewReader(`{"rights":"`+rights+`","ttl":"1d","title":"Client"}`)), owner,
			resaccess.Ref{Kind: resaccess.KindDoc, ID: slug}, []string{"shares"}, resaccess.Info{Owner: owner.Email, Target: target}, nil)
		var out struct{ URL string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 201 {
			t.Fatalf("link: %d %s", w.Code, w.Body.String())
		}
		return out.URL
	}
	board := link("flow-abc", targetBoard, "comment")
	if !strings.HasPrefix(board, "https://boards.example.com/b/flow-abc?g=") {
		t.Fatalf("a board's link opens on the board: %s", board)
	}
	if doc := link("plan", "", "view"); !strings.HasPrefix(doc, "https://hub.example.com/d/") {
		t.Fatalf("a document's link stays the hub's page: %s", doc)
	}
	token := board[strings.Index(board, "?g=")+3:]
	guest := func(secret, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/platform/docs/_guest", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, out := guest("boards-secret", `{"token":"`+token+`","slug":"flow-abc","open":true}`)
	if code != http.StatusOK || out["rights"] != RightComment || out["forWhom"] != "Client" || out["slug"] != "flow-abc" {
		t.Fatalf("what the link allows: %d %v", code, out)
	}
	if code, _ := guest("boards-secret", `{"token":"`+token+`","slug":"other-board"}`); code != http.StatusNotFound {
		t.Errorf("the link on another board: %d, want 404", code)
	}
	if code, _ := guest("boards-secret", `{"token":"`+strings.Repeat("x", 40)+`","slug":"flow-abc"}`); code != http.StatusNotFound {
		t.Errorf("a token nobody made: %d, want 404", code)
	}
	if code, _ := guest("wrong", `{"token":"`+token+`","slug":"flow-abc"}`); code != http.StatusUnauthorized {
		t.Errorf("a wrong secret: %d, want 401", code)
	}
}
