package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/proxy"
)

func frameRequest(site string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/agents/api/m1/design/store/p/f/Board.dc.html", nil)
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Site", site)
	return r
}

func framedBy(t *testing.T, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Every answer the hub gives a frame of its own page shows in that frame: never DENY, never frame-ancestors 'none'.
func TestFrameSafeSwapsEveryUnframeableAnswer(t *testing.T) {
	cases := map[string]struct {
		h      http.HandlerFunc
		status int
	}{
		"hub page with DENY (404)": {func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<p>Не найдено</p>"))
		}, http.StatusNotFound},
		"machine broke off (502 page, frame-ancestors none)": {func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
		}, http.StatusBadGateway},
		"sign-in redirect": {func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/signin?next=%2Fx", http.StatusFound)
		}, http.StatusUnauthorized},
		"expired key (JSON 401)": {func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
		}, http.StatusUnauthorized},
		"no machine (JSON 502)": {func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
		}, http.StatusBadGateway},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := framedBy(t, frameSafe(c.h), frameRequest("same-origin"))
			if w.Code != c.status {
				t.Fatalf("status %d, want %d", w.Code, c.status)
			}
			if xfo := w.Header().Get("X-Frame-Options"); xfo != "" {
				t.Fatalf("X-Frame-Options %q left on the frame's page", xfo)
			}
			csp := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "frame-ancestors 'self'") || !strings.Contains(csp, "sandbox allow-scripts") {
				t.Fatalf("csp %q", csp)
			}
			body := w.Body.String()
			for _, want := range []string{"Не удалось загрузить доску", "Повторить", "hlViewError", "location.reload"} {
				if !strings.Contains(body, want) {
					t.Fatalf("body has no %q: %s", want, body)
				}
			}
			if strings.Contains(body, "Не найдено") || strings.Contains(body, "unauthenticated") {
				t.Fatalf("the original body leaked into the frame's page: %s", body)
			}
		})
	}
}

func TestFrameSafeLeavesFramesThatShow(t *testing.T) {
	board := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; frame-ancestors 'self'")
		_, _ = w.Write([]byte("<x-dc>board</x-dc>"))
	})
	w := framedBy(t, frameSafe(board), frameRequest("same-origin"))
	if w.Code != http.StatusOK || w.Body.String() != "<x-dc>board</x-dc>" {
		t.Fatalf("board changed: %d %q", w.Code, w.Body.String())
	}
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/agents/api/m1/design/store/p/k/abc/f/Board.dc.html", http.StatusFound)
	})
	if w := framedBy(t, frameSafe(redirect), frameRequest("same-origin")); w.Code != http.StatusFound {
		t.Fatalf("the keyed redirect was swapped: %d", w.Code)
	}
	// A frame's own refusal page (a slot by path) already shows and tells the page above.
	refusal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; frame-ancestors 'self'")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("slot"))
	})
	if w := framedBy(t, frameSafe(refusal), frameRequest("same-origin")); w.Body.String() != "slot" {
		t.Fatalf("the slot's refusal was swapped: %q", w.Body.String())
	}
}

// A frame of another site, and the page opened as a whole, keep the hub's refusal: framing them is clickjacking.
func TestFrameSafeKeepsOtherSitesRefused(t *testing.T) {
	deny := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.WriteHeader(http.StatusNotFound)
	})
	if w := framedBy(t, frameSafe(deny), frameRequest("cross-site")); w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("a frame of another site lost the hub's DENY")
	}
	whole := frameRequest("same-origin")
	whole.Header.Set("Sec-Fetch-Dest", "document")
	if w := framedBy(t, frameSafe(deny), whole); w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("a whole page lost the hub's DENY")
	}
}

// A board's page in its sandbox went to the library's address past its slot (/a/<project>/p/<board>): a cross-site
// frame's navigation, answered with the page that asks the pane to open it again. The hub's frame ban on the path
// outside /agents/ must not ride on it, or the browser shows «hub refused to connect» (show_board → reload).
func TestReloadPageOfABoardPastItsSlotShowsInTheFrame(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/a/harnessly/p/%D0%93%D0%BE%D0%BB%D0%BE%D1%81.dc.html", nil)
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if !proxy.ReloadAsked(r) {
		t.Fatal("the navigation past the slot is not taken for a reload")
	}
	h := frameSafe(securityHeaders(true, http.HandlerFunc(proxy.ServeReload)))
	w := framedBy(t, h, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("status %d, X-Frame-Options %q", w.Code, w.Header().Get("X-Frame-Options"))
	}
	if !strings.Contains(w.Body.String(), "hl-preview-reload") {
		t.Fatalf("body %q", w.Body.String())
	}
}
