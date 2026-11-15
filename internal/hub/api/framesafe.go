package api

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// A frame of the hub's own page (a board in Sinopia, a preview, an app's view) navigates to the hub's address. Whatever
// the hub answers there, the person must never see the browser's «hub refused to connect»: that is what a page of the
// hub (sign-in, 404, an error page, the UI itself) gives in a frame, since each carries X-Frame-Options: DENY or
// frame-ancestors 'none'. A short break between the hub and the machine, an expired sign-in or a key of a board's files
// that ran out turned the board on screen into that refusal. frameSafe swaps such an answer for a page of its own that
// the hub's page may frame: it says what happened, tries again by itself and tells the page above (hlViewError, as a
// refused slot does), which opens the frame anew with a fresh address.

// frameNavigation: the request is a frame's navigation whose page above is the hub's own. Frames of other sites stay
// refused (clickjacking), and so does anything the person opens as a whole page.
func frameNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		r.Header.Get("Sec-Fetch-Dest") == "iframe" && r.Header.Get("Sec-Fetch-Mode") == "navigate" &&
		r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// frameSafe wraps next for frame navigations of the hub's own pages.
func frameSafe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !frameNavigation(r) {
			next.ServeHTTP(w, r)
			return
		}
		fw := &frameSafeWriter{ResponseWriter: w, r: r}
		next.ServeHTTP(fw, r)
		if !fw.decided {
			fw.WriteHeader(http.StatusOK)
		}
	})
}

// unframeable says the answer would not show in the hub's frame, or would show something that is not the frame's page:
// a refusal to be framed, a sign-in, an error the page can not draw (JSON, an empty body).
func unframeable(status int, h http.Header) bool {
	if strings.EqualFold(strings.TrimSpace(h.Get("X-Frame-Options")), "DENY") {
		return true
	}
	for _, csp := range h.Values("Content-Security-Policy") {
		for _, part := range strings.Split(csp, ";") {
			f := strings.Fields(strings.ToLower(part))
			if len(f) >= 2 && f[0] == "frame-ancestors" && f[1] == "'none'" {
				return true
			}
		}
	}
	if status >= 300 && status < 400 && strings.HasPrefix(h.Get("Location"), "/signin") {
		return true
	}
	if status >= 400 && !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		return true
	}
	return false
}

// frameSafeWriter holds the decision until the status is known: the answer goes through as it is, or the frame's page
// goes instead of it and the answer's body is dropped.
type frameSafeWriter struct {
	http.ResponseWriter
	r       *http.Request
	decided bool
	swapped bool
}

func (w *frameSafeWriter) WriteHeader(code int) {
	if w.decided {
		return
	}
	w.decided = true
	h := w.ResponseWriter.Header()
	if !unframeable(code, h) {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.swapped = true
	if code >= 300 && code < 400 {
		code = http.StatusUnauthorized // the sign-in a frame was sent to
	}
	for k := range h {
		delete(h, k)
	}
	serveFrameFailure(w.ResponseWriter, w.r, code)
}

func (w *frameSafeWriter) Write(b []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.swapped {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *frameSafeWriter) Flush() {
	if w.swapped {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *frameSafeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("the response cannot be hijacked")
	}
	w.decided = true
	return h.Hijack()
}

func (w *frameSafeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// frameFailureScript tells the page above and tries again by itself: soon after a break of the link (5xx), later after
// anything else. The page above usually opens the frame anew before that, with a fresh address.
const frameFailureScript = `(function(){var d=document.documentElement.dataset,s=Number(d.status);` +
	`try{parent.postMessage({hlViewError:{kind:"refused",status:s,code:d.code,text:d.reason,src:""}},"*")}catch(e){}` +
	`var again=function(){location.reload()};document.getElementById("again").onclick=again;` +
	`setTimeout(again,s>=500?4000:15000)})()`

var frameFailureCSP = func() string {
	sum := sha256.Sum256([]byte(frameFailureScript))
	return "default-src 'none'; style-src 'unsafe-inline'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; base-uri 'none'; form-action 'none'; sandbox allow-scripts; frame-ancestors 'self'"
}()

// frameFailureReason is the line under the title: what happened, in the person's words.
func frameFailureReason(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "Вход устарел — обновляем доступ."
	case status == http.StatusForbidden:
		return "Нет доступа к этой странице."
	case status == http.StatusNotFound:
		return "Страница не найдена."
	case status >= 500:
		return "Связь с машиной прервалась — переподключаемся."
	}
	return "Хаб ответил ошибкой " + strconv.Itoa(status) + "."
}

func serveFrameFailure(w http.ResponseWriter, r *http.Request, status int) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", frameFailureCSP)
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	reason := frameFailureReason(status)
	_, _ = w.Write([]byte(`<!doctype html><html lang="ru" data-status="` + strconv.Itoa(status) + `" data-code="hub-frame" data-reason="` +
		html.EscapeString(reason) + `"><head><meta charset="utf-8"><meta name="color-scheme" content="light dark">` +
		`<title>Не удалось загрузить доску</title><style>` +
		`html,body{height:100%;margin:0}body{display:grid;place-items:center;font:14px/1.5 system-ui,sans-serif;color:#8a8f98}` +
		`main{display:grid;gap:8px;justify-items:center;text-align:center;padding:24px;animation:in .3s ease-out}` +
		`b{font-weight:500;color:CanvasText}button{font:inherit;color:CanvasText;background:none;border:1px solid #8a8f9866;` +
		`border-radius:8px;padding:4px 12px;cursor:pointer;transition:background .15s ease-out}button:hover{background:#8a8f9822}` +
		`@keyframes in{from{opacity:0;transform:translateY(4px)}}</style></head><body><main><b>Не удалось загрузить доску</b>` +
		`<span>` + html.EscapeString(reason) + `</span><button id="again" type="button">Повторить</button></main>` +
		`<script>` + frameFailureScript + `</script></body></html>`))
}
