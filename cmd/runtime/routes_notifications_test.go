package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/noticeslot"
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

// whose notices a request reads comes from its session, not from its query: a person cannot read another's.
func TestNotificationPersonComesFromTheSession(t *testing.T) {
	t.Setenv("RUNTIME_STORE_DB", "")
	d := deps{DataDir: t.TempDir()}
	d.SessionOK = func(*http.Request) bool { return true }
	d.Claims = func(r *http.Request) map[string]any { return map[string]any{"sub": r.Header.Get("X-Sub")} }
	routes := notificationRoutes(d)
	serve := func(sub, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Sub", sub)
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
	if w := serve("a@x.ru", "POST", base+"/items?person=b@x.ru", `{"key":"ka","chat":"c","at":1,"doc":{"t":"x"}}`); w.Code >= 300 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	if w := serve("b@x.ru", "GET", base+"?person=a@x.ru", ""); strings.Contains(w.Body.String(), `"ka"`) {
		t.Fatalf("b read a's notice: %s", w.Body)
	}
	if w := serve("user", "GET", base+"?person=a@x.ru", ""); strings.Contains(w.Body.String(), `"ka"`) {
		t.Fatalf("the machine's login read a's notice: %s", w.Body)
	}
	if w := serve("a@x.ru", "GET", base, ""); !strings.Contains(w.Body.String(), `"ka"`) {
		t.Fatalf("a lost their notice: %s", w.Body)
	}
}

// the slot's open questions go to the agent as one list, again after each move, without a view of their own.
func TestSlotFeedSendsTheListAndEachMove(t *testing.T) {
	slot := noticeslot.New()
	f := newSlotFeed(slot)
	first := f.WorkFrames()
	if len(first) != 1 || !strings.Contains(string(first[0]), `"k":"slot"`) || !strings.Contains(string(first[0]), `"notices":[]`) {
		t.Fatalf("first frame: %s", first)
	}
	got := make(chan []byte, 4)
	stop := f.WatchWork(func(b []byte) { got <- b })
	defer stop()
	if _, err := slot.Open(noticeslot.Source{Kind: noticeslot.SourceAgent, ID: "conv-1", Name: "agent"}, "conv-1",
		noticeslot.Spec{Kind: noticeslot.KindCustom, Title: "Push?", View: `<div><button data-answer="yes">Yes</button><b>x</b></div>`}); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-got:
		s := string(b)
		if !strings.Contains(s, `Push?`) || strings.Contains(s, `<b>x</b>`) || !strings.Contains(s, `"conversation":"conv-1"`) {
			t.Fatalf("frame: %s", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no frame after the slot moved")
	}
}
