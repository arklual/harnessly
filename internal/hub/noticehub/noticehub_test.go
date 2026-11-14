package noticehub

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/notices"
	_ "modernc.org/sqlite"
)

func TestSwitchAndPerson(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/h.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := notices.EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	on := false
	s := New(notices.Wrap(db, false), func(*http.Request, string, string) bool { return on })
	u, m := store.User{ID: "u1", Email: "A@example.com"}, store.Machine{ID: "m1"}
	do := func(method, p, body string) (*httptest.ResponseRecorder, bool) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/m/m1"+p, strings.NewReader(body))
		return w, s.ServeMachine(w, r, u, m)
	}
	if _, served := do("GET", "/api/x/notifications", ""); served {
		t.Fatal("switch off: the machine answers")
	}
	on = true
	if _, served := do("GET", "/api/x/other", ""); served {
		t.Fatal("not a notifications path")
	}
	w, served := do("POST", "/api/x/notifications/items", `{"key":"k","chatId":"c","at":1,"doc":{"a":1}}`)
	if !served || w.Code != 200 {
		t.Fatalf("items: %v %d", served, w.Code)
	}
	w, _ = do("GET", "/api/x/notifications", "")
	if !strings.Contains(w.Body.String(), `"key":"k"`) {
		t.Fatal(w.Body.String())
	}
	do("POST", "/api/x/notifications/read", `{"keys":["k"]}`)
	w, _ = do("GET", "/api/x/notifications", "")
	if strings.Contains(w.Body.String(), `"items":[{`) {
		t.Fatal("read item still listed: " + w.Body.String())
	}
}
