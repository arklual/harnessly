package webvitals

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func post(lim *Limiter, body string, got *[]Sample) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	Serve(w, r, lim, "k", func(s Sample) { *got = append(*got, s) })
	return w
}

func TestServeTakesKnownNames(t *testing.T) {
	var got []Sample
	if w := post(nil, `{"name":"sinopia_home_full","seconds":1.5}`, &got); w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	if len(got) != 1 || got[0].Name != SinopiaHomeFull || got[0].Seconds != 1.5 {
		t.Fatalf("got %+v", got)
	}
}

func TestServeDropsWhatIsNotAMeasurement(t *testing.T) {
	for _, body := range []string{
		`{"name":"fcp","seconds":1}`, `{"name":"lcp","seconds":0}`, `{"name":"lcp","seconds":-3}`,
		`{"name":"lcp","seconds":9999}`, `{"name":"","seconds":1}`,
	} {
		var got []Sample
		if w := post(nil, body, &got); w.Code != http.StatusNoContent || len(got) != 0 {
			t.Errorf("%s: status %d, sinks %d", body, w.Code, len(got))
		}
	}
}

func TestServeRefusesBadBodyAndMethod(t *testing.T) {
	var got []Sample
	if w := post(nil, `not json`, &got); w.Code != http.StatusBadRequest {
		t.Errorf("bad json: %d", w.Code)
	}
	if w := post(nil, `{"name":"lcp","seconds":1,"pad":"`+strings.Repeat("x", 600)+`"}`, &got); w.Code != http.StatusBadRequest {
		t.Errorf("long body: %d", w.Code)
	}
	w := httptest.NewRecorder()
	Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), nil, "k", func(Sample) {})
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("get: %d", w.Code)
	}
}

func TestLimiterCutsAKeyNotOthers(t *testing.T) {
	l := &Limiter{Max: 2, Window: time.Minute}
	t0 := time.Now()
	if !l.Allow("a", t0) || !l.Allow("a", t0) || l.Allow("a", t0) {
		t.Fatal("two pass, the third does not")
	}
	if !l.Allow("b", t0) {
		t.Fatal("another key is not cut")
	}
	if !l.Allow("a", t0.Add(time.Minute)) {
		t.Fatal("a new window starts")
	}
}

func TestServeLimitAnswers429(t *testing.T) {
	l := &Limiter{Max: 1, Window: time.Minute}
	var got []Sample
	post(l, `{"name":"lcp","seconds":1}`, &got)
	if w := post(l, `{"name":"lcp","seconds":1}`, &got); w.Code != http.StatusTooManyRequests || len(got) != 1 {
		t.Fatalf("status %d, sinks %d", w.Code, len(got))
	}
}
