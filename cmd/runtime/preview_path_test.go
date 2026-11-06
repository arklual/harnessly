package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var testKey = strings.Repeat("0a", 16)

func TestSplitPreviewPath(t *testing.T) {
	t.Setenv("RUNTIME_APP_BASE_PATH", "/m/abc")
	k := "-" + testKey
	for path, want := range map[string]struct {
		slot int
		tail string
	}{
		"/m/abc/__pv/1" + k + "/x/y": {1, "/x/y"},
		"/m/abc/__pv/6" + k:          {6, "/"},
		"/m/abc/__pv/3" + k + "/":    {3, "/"},
		"/m/abc/__pv/3/x":            {-1, ""},
		"/m/abc/__pv/3-short/x":      {-1, ""},
		"/m/abc/__pv/9" + k + "/x":   {-1, ""},
		"/m/abc/__pv/12" + k + "/x":  {-1, ""},
		"/m/abc/api/state":           {0, ""},
		"/agents/__pv/1" + k + "/x":  {0, ""},
	} {
		slot, key, tail := splitPreviewPath(path)
		if slot != want.slot || tail != want.tail || slot > 0 && key != testKey {
			t.Errorf("%s: %d %q %q", path, slot, key, tail)
		}
	}
}

func TestModeAuto(t *testing.T) {
	https, _ := url.Parse("https://harnessly.example")
	plain, _ := url.Parse("http://127.0.0.1:8022")
	ports := &previewSlots{ports: []int{8023}, mode: "auto"}
	if ports.usesPath(plain) {
		t.Fatal("a plain http page with slot listeners keeps its ports")
	}
	if !ports.usesPath(https) {
		t.Fatal("an https page without public origins goes by path")
	}
	origins := &previewSlots{ports: []int{8023}, public: []string{"https://p1.example"}, mode: "auto"}
	if origins.usesPath(https) {
		t.Fatal("public origins win over the path")
	}
	if !pathOnlySlots().usesPath(plain) || !(&previewSlots{ports: []int{8023}, mode: "path"}).usesPath(plain) {
		t.Fatal("forced or listener-less slots go by path")
	}
	if (&previewSlots{ports: []int{8023}, mode: "ports"}).usesPath(https) {
		t.Fatal("forced ports stay ports")
	}
}

func TestPathSlotOrigin(t *testing.T) {
	t.Setenv("RUNTIME_APP_BASE_PATH", "/m/abc")
	s := pathOnlySlots()
	https, _ := url.Parse("https://harnessly.example")
	o := "https://harnessly.example" + pathPrefix(2, newPathKey())
	if o == "https://harnessly.example"+pathPrefix(2, newPathKey()) {
		t.Fatal("every open has a key of its own")
	}
	if slot, _, _ := splitPreviewPath(prefixOf(o) + "/x"); slot != 2 || !strings.HasPrefix(o, "https://harnessly.example/m/abc/__pv/2-") {
		t.Fatalf("the slot origin's prefix routes back: %q", o)
	}
	if prefixOf("https://p1.example") != "" || !s.usesPath(https) {
		t.Fatal("prefixOf")
	}
}

func TestPathGuardRefusesOpaque(t *testing.T) {
	t.Setenv("RUNTIME_APP_BASE_PATH", "/agents")
	reached := false
	h := pathOnlySlots().mount(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	for name, headers := range map[string]map[string]string{
		"fetch from the opaque page":  {"Origin": "null", "Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors"},
		"socket from the opaque page": {"Origin": "null", "Upgrade": "websocket"},
		"image from another site":     {"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image"},
	} {
		reached = false
		r := httptest.NewRequest("GET", "https://h.example/agents/api/state", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if reached || w.Code != 403 {
			t.Errorf("%s: reached=%v code=%d", name, reached, w.Code)
		}
	}
	reached = false
	r := httptest.NewRequest("GET", "https://h.example/agents/", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Fatal("the page's own request passes")
	}
	reached = false
	r = httptest.NewRequest("GET", "https://h.example/agents/", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Dest", "document")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Fatal("following a link to the page passes")
	}
}

func TestPreviewReloadNavigation(t *testing.T) {
	t.Setenv("RUNTIME_APP_BASE_PATH", "/agents")
	reached := false
	h := pathOnlySlots().mount(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	r := httptest.NewRequest("GET", "https://h.example/dashboard?tab=2", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if reached || w.Code != 200 || !strings.Contains(body, `"hl-preview-reload"`) || !strings.Contains(body, `"/dashboard?tab=2"`) {
		t.Fatalf("reload page: %d %v %s", w.Code, reached, body)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("the reload page runs sandboxed")
	}
}

func TestPathStripsOriginHeaders(t *testing.T) {
	h := http.Header{}
	for _, name := range []string{"Clear-Site-Data", "Strict-Transport-Security", "Access-Control-Allow-Origin", "Service-Worker-Allowed"} {
		h.Set(name, "x")
	}
	h.Set("Content-Type", "text/html")
	pathSlotHeaders(h)
	for _, name := range []string{"Clear-Site-Data", "Strict-Transport-Security", "Service-Worker-Allowed"} {
		if h.Get(name) != "" {
			t.Errorf("%s passed: a target would act on the page's origin", name)
		}
	}
	if h.Get("Access-Control-Allow-Origin") != "null" || h.Get("Content-Type") != "text/html" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "sandbox allow-scripts") {
		t.Fatalf("headers: %v", h)
	}
}

func TestPathPreflightAnsweredHere(t *testing.T) {
	r := httptest.NewRequest("OPTIONS", "/api/x", nil)
	r.Header.Set("Origin", "null")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	r.Header.Set("Access-Control-Request-Headers", "content-type, x-token")
	w := httptest.NewRecorder()
	if !pathPreflight(w, r) || w.Code != 204 {
		t.Fatalf("preflight: %d", w.Code)
	}
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != "null" || h.Get("Access-Control-Allow-Credentials") != "true" ||
		h.Get("Access-Control-Allow-Methods") != "PUT" || h.Get("Access-Control-Allow-Headers") != "content-type, x-token" {
		t.Fatalf("headers: %v", h)
	}
	if pathPreflight(httptest.NewRecorder(), httptest.NewRequest("OPTIONS", "/x", nil)) {
		t.Fatal("a plain OPTIONS goes to the target")
	}
}

func TestPathSessionsByKey(t *testing.T) {
	t.Setenv("RUNTIME_APP_BASE_PATH", "/agents")
	s := pathOnlySlots()
	sess := &previewSession{sub: "u1", slot: 1, origin: "https://h.example/agents/__pv/1-" + testKey, app: "https://h.example"}
	s.byPath.put(testKey, sess)
	if s.byPath.get(testKey) != sess || s.byPath.get(strings.Repeat("f", 32)) != nil {
		t.Fatal("a slot by path is found by its key only")
	}
	reached := false
	h := s.mount(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	for _, path := range []string{"/agents/__pv/1-" + strings.Repeat("f", 32) + "/x", "/agents/__pv/2-" + testKey + "/x"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://h.example"+path, nil))
		if reached || !strings.Contains(w.Body.String(), "hl-preview-reload") {
			t.Fatalf("%s: an unknown key or another slot's gets the reopen page: %d %s", path, w.Code, w.Body.String())
		}
	}
	for i := 0; i < pathSessionsKept+10; i++ {
		s.byPath.put(newPathKey(), sess)
	}
	if n := len(s.byPath.open); n > pathSessionsKept {
		t.Fatalf("kept %d keys", n)
	}
}
