package apps

import (
	"crypto/sha256"
	"encoding/base64"
	"html"
	"log/slog"
	"net/http"
	"strconv"
)

// A refused request of a view (no session, a file that is not there, a file of another app) is answered with a small
// page that says why, not with bare text: a person who opens the address, or a frame that shows it, reads the reason.
// The page tells the host the same (a message to the window above), so the frame can put its own words in place of
// this one; and the runtime's log gets a line, so a script that 404'd is not silent for the agent.

// viewRefusal is one refused request.
type viewRefusal struct {
	Status int
	// Code is the short name of the cause, for the log and the host: no-session, no-file, other-app, sdk-file, no-app, host.
	Code string
	// App and View are what the session names; empty when there was no session.
	App, View string
	// Path is the file asked for inside the app, never the session's token.
	Path string
	// Reason is the sentence the page says, in Russian, for the person.
	Reason string
}

// refusalScript tells the host above the frame what was refused. It reads the page's own data attributes; the CSP lets
// this one script run by its hash and nothing else.
const refusalScript = `try{var d=document.documentElement.dataset;parent.postMessage({hlViewError:{kind:"refused",status:Number(d.status),code:d.code,text:d.reason,src:d.path}},"*")}catch(e){}`

var refusalCSP = func() string {
	sum := sha256.Sum256([]byte(refusalScript))
	return "default-src 'none'; style-src 'unsafe-inline'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

func refuseView(w http.ResponseWriter, r *http.Request, f viewRefusal) {
	slog.Warn("apps view refused", "app", f.App, "view", f.View, "path", f.Path, "status", f.Status, "code", f.Code, "method", r.Method)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", refusalCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(f.Status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(`<!doctype html><html lang="ru" data-status="` + strconv.Itoa(f.Status) + `" data-code="` + html.EscapeString(f.Code) +
		`" data-path="` + html.EscapeString(f.Path) + `" data-reason="` + html.EscapeString(f.Reason) +
		`"><head><meta charset="utf-8"><meta name="color-scheme" content="light dark"><title>Вид не открылся</title><style>` +
		`body{margin:0;padding:24px;font:14px/1.5 system-ui,sans-serif}p{margin:0 0 6px}.s{opacity:.6;font-size:12px}` +
		`</style></head><body><p>` + html.EscapeString(f.Reason) + `</p><p class="s">Код ` + strconv.Itoa(f.Status) + ", " + html.EscapeString(f.Code) +
		`</p><script>` + refusalScript + `</script></body></html>`))
}
