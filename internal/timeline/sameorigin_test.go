package timeline

import (
	"net/http/httptest"
	"testing"
)

// A page of another origin does not get the socket: the browser sends the session cookie to any site, and a socket
// answers to whoever opened it.
func TestSocketRefusesAPageOfAnotherOrigin(t *testing.T) {
	for name, h := range map[string]map[string]string{
		"another origin":   {"Origin": "https://evil.example"},
		"cross-site fetch": {"Sec-Fetch-Site": "cross-site"},
		"same-site fetch":  {"Sec-Fetch-Site": "same-site", "Origin": "http://app.test"},
	} {
		r := httptest.NewRequest("GET", "http://app.test/socket?session=s", nil)
		for k, v := range h {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		New(Deps{}).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}
