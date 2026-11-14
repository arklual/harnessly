package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// a runtime with no hub keeps the person's notices in its own database and serves them.
func TestNotificationRoutesWithoutHub(t *testing.T) {
	t.Setenv("RUNTIME_STORE_DB", "")
	d := deps{DataDir: t.TempDir()}
	d.SessionOK = func(*http.Request) bool { return true }
	routes := notificationRoutes(d)
	serve := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return w
			}
		}
		t.Fatalf("no route for %s", path)
		return nil
	}
	base := platformAPI + "/notifications"
	if w := serve("POST", base+"/items?person=p", `{"key":"k1","chat":"c","at":1,"doc":{"t":"x"}}`); w.Code >= 300 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	w := serve("GET", base+"?person=p", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"k1"`) {
		t.Fatalf("state: %d %s", w.Code, w.Body)
	}
}
