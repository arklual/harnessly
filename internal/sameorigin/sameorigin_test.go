package sameorigin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCrossSite(t *testing.T) {
	for name, tc := range map[string]struct {
		h     map[string]string
		cross bool
	}{
		"no browser headers":           {nil, false},
		"same origin by origin":        {map[string]string{"Origin": "http://app.test"}, false},
		"same origin by fetch site":    {map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://proxy.example"}, false},
		"typed address":                {map[string]string{"Sec-Fetch-Site": "none"}, false},
		"behind a proxy":               {map[string]string{"Origin": "https://public.example", "X-Forwarded-Host": "public.example, inner"}, false},
		"another site":                 {map[string]string{"Origin": "https://evil.example"}, true},
		"a sibling host":               {map[string]string{"Origin": "http://app.test.evil.example"}, true},
		"the null origin":              {map[string]string{"Origin": "null"}, true},
		"cross-site fetch":             {map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		"same-site fetch beats origin": {map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://app.test"}, true},
	} {
		r := httptest.NewRequest("GET", "http://app.test/socket", nil)
		for k, v := range tc.h {
			r.Header.Set(k, v)
		}
		if got := CrossSite(r); got != tc.cross {
			t.Errorf("%s: %v", name, got)
		}
		w := httptest.NewRecorder()
		if refused := Refuse(w, r); refused != tc.cross || tc.cross && w.Code != http.StatusForbidden || !tc.cross && w.Code != http.StatusOK {
			t.Errorf("%s: refused %v with %d", name, refused, w.Code)
		}
	}
}
