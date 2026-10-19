package preview

import (
	"strconv"
	"strings"

	"harnessly/internal/preview/weburl"
)

// Pages the runtime answers inside a slot frame by itself: errors, the cross-origin redirect offer and the stand
// sign-in.

func escapeHTML(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '&', '<', '>', '"', '\'':
			b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

const pageStyle = "body{margin:0;padding:16px;box-sizing:border-box;text-align:center;min-height:100vh;display:grid;place-items:center;font:15px/1.5 system-ui,sans-serif;background:#f7f7f8;color:#1c1c1f}a,button{font:inherit;color:inherit}button{margin-right:12px;padding:6px 14px;border-radius:6px;border:1px solid currentColor;background:none;cursor:pointer}@media (prefers-color-scheme:dark){body{background:#121214;color:#ececef}}"

func layout(title, body string) string {
	return `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="robots" content="noindex"><title>` + escapeHTML(title) +
		`</title><style>` + pageStyle + `</style></head><body>` + body + `</body></html>`
}

func statusPage(status int, text string) string {
	return layout("Превью · "+strconv.Itoa(status), "<p>"+escapeHTML(text)+"</p>")
}

// offerPage: the site redirects to another origin. The runtime never follows it: the page asks the preview island to
// open that origin as a new target, which goes through the same checks again.
func offerPage(u *weburl.URL, parentOrigin, nonce string) string {
	button, script := "", ""
	if parentOrigin != "" {
		button = `<button type="button" id="tk-open" hidden data-url="` + escapeHTML(u.Href()) + `" data-parent="` + escapeHTML(parentOrigin) + `">Открыть в превью</button>`
		script = `<script nonce="` + nonce + `">(function () {
  var button = document.getElementById('tk-open');
  var parentOrigin = button.getAttribute('data-parent');
  var ancestors = location.ancestorOrigins;
  if (window.parent === window || (ancestors && ancestors.length && ancestors[0] !== parentOrigin)) return;
  button.hidden = false;
  button.addEventListener('click', function () { window.parent.postMessage({ type: 'tk:open', url: button.getAttribute('data-url') }, parentOrigin); });
})();</script>`
	}
	return layout("Превью · переход", `<div><p>Сайт перенаправляет на `+escapeHTML(u.Origin())+`</p><p>`+button+`<a href="`+escapeHTML(u.Href())+
		`" target="_blank" rel="noopener noreferrer">Открыть в новой вкладке</a></p></div>`+script)
}

func offerPolicy(nonce string) string {
	return "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-" + nonce + "'; base-uri 'none'; form-action 'none'"
}

// signInPage: a stand answered with the way to its sign-in, which the slot never follows; the island signs the person
// in in their browser.
func signInPage(signInURL, parentOrigin, nonce string) string {
	parent, hint, script := "", "", ""
	if parentOrigin != "" {
		parent = ` data-parent="` + escapeHTML(parentOrigin) + `"`
		hint = `<p id="tk-sign-in-hint" hidden>Нажмите «Войти» над страницей: после входа она обновится.</p>`
		script = `<script nonce="` + nonce + `">(function () {
  var link = document.getElementById('tk-sign-in');
  var parentOrigin = link.getAttribute('data-parent');
  var ancestors = location.ancestorOrigins;
  if (window.parent === window || (ancestors && ancestors.length && ancestors[0] !== parentOrigin)) return;
  link.hidden = true;
  document.getElementById('tk-sign-in-hint').hidden = false;
  window.addEventListener('message', function (event) {
    if (event.source !== window.parent || event.origin !== parentOrigin || !event.data || event.data.type !== 'tk:ping') return;
    window.parent.postMessage({ type: 'tk:sign-in' }, parentOrigin);
  });
})();</script>`
	}
	return layout("Превью · вход", `<div><p>Стенд закрыт входом через Keycloak</p>`+hint+`<p><a id="tk-sign-in"`+parent+` href="`+escapeHTML(signInURL)+
		`" target="_blank" rel="noopener noreferrer">Войти</a></p></div>`+script)
}

// bridgeTag is the script element of the page's bridge: the parent it talks to, the target's cookie tag, and the
// DevTools conditions as JSON so it applies them from the first request on.
func bridgeTag(parentOrigin, cookieTag string, devtoolsJSON string, hasDevtools bool) string {
	conditions := ""
	if hasDevtools {
		conditions = ` data-devtools="` + escapeHTML(devtoolsJSON) + `"`
	}
	return `<script src="` + BridgePath + `" data-parent="` + escapeHTML(parentOrigin) + `" data-tag="` + escapeHTML(cookieTag) + `"` + conditions + `></script>`
}
