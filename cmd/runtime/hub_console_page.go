package main

import (
	"encoding/json"
	"html"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// consoleTexts are the page's words, in the runtime's language.
func consoleTexts() map[string]string {
	return map[string]string{
		"title":      locale.Pick("Sign in", "Вход"),
		"signinTo":   locale.Pick("Sign in to", "Вход в"),
		"onMachine":  locale.Pick("on machine", "на машине"),
		"preparing":  locale.Pick("Preparing the sign-in…", "Готовим вход…"),
		"preparingH": locale.Pick("The sign-in is starting on the machine. This takes a few seconds.", "Вход запускается на машине, это займёт несколько секунд."),
		"waiting":    locale.Pick("Waiting for the browser", "Ждём вход в браузере"),
		"waitingH":   locale.Pick("Open the sign-in page, sign in with your account and come back. This page notices the result by itself.", "Откройте страницу входа, войдите в свой аккаунт и вернитесь. Страница сама заметит результат."),
		"verifying":  locale.Pick("Verifying…", "Проверяем…"),
		"verifyingH": locale.Pick("The sign-in answer reached the machine. Checking that the account is connected.", "Ответ входа дошёл до машины. Проверяем, что аккаунт подключён."),
		"connected":  locale.Pick("Connected", "Подключено"),
		"connectedH": locale.Pick("The account is signed in on this machine; it passed the provider's own status check. You can close this page.", "Аккаунт вошёл на этой машине, проверка статуса у провайдера пройдена. Страницу можно закрыть."),
		"failed":     locale.Pick("Sign-in failed", "Вход не удался"),
		"failedH":    locale.Pick("The sign-in ended without a connected account. Start again from the machine's page, or open the troubleshooting below.", "Вход завершился без подключённого аккаунта. Начните заново со страницы машины или откройте «Если что-то не так» ниже."),
		"expired":    locale.Pick("Time is up", "Время вышло"),
		"expiredH":   locale.Pick("The sign-in was not finished in time. Start it again from the machine's page.", "Вход не завершён вовремя. Начните его заново со страницы машины."),
		"cancelled":  locale.Pick("Cancelled", "Отменено"),
		"cancelledH": locale.Pick("This sign-in was stopped; nothing from it keeps running on the machine.", "Этот вход остановлен, на машине от него ничего не осталось."),
		"open":       locale.Pick("Open sign-in page", "Открыть страницу входа"),
		"cancel":     locale.Pick("Cancel", "Отменить"),
		"left":       locale.Pick("{n} min left", "осталось {n} мин"),
		"trouble":    locale.Pick("Troubleshooting", "Если что-то не так"),
		"troubleH":   locale.Pick("The machine's terminal, for sign-ins that ask for more than a browser.", "Терминал машины — для входов, которым нужно больше, чем браузер."),
		"termOpenH":  locale.Pick("This sign-in may ask for something in the terminal below (a code, a choice).", "Этому входу может понадобиться ввод в терминале ниже (код, выбор)."),
		"input":      locale.Pick("Type here and press Enter", "Введите здесь и нажмите Enter"),
		"send":       locale.Pick("Send", "Отправить"),
		"ended":      locale.Pick("The terminal has ended.", "Сеанс завершён."),
		"offline":    locale.Pick("No connection to the machine…", "Нет связи с машиной…"),
		"cbTitle":    locale.Pick("The browser ended on a page that does not load?", "Браузер закончил на странице, которая не открывается?"),
		"cbHint": locale.Pick("Some sign-ins return to an address on the machine itself (http://localhost…), which your browser cannot reach. That is expected: copy the full address from the address bar and paste it here. It is delivered once, to this sign-in only, and is not stored.",
			"Некоторые входы возвращаются на адрес самой машины (http://localhost…), который ваш браузер открыть не может. Так и должно быть: скопируйте полный адрес из адресной строки и вставьте сюда. Он будет передан один раз и только этому входу, нигде не сохраняется."),
		"cbInput":     locale.Pick("http://localhost:…", "http://localhost:…"),
		"cbSend":      locale.Pick("Finish sign-in", "Завершить вход"),
		"cbDone":      locale.Pick("Delivered to the sign-in. Checking the account…", "Передано входу. Проверяем аккаунт…"),
		"cbFailed":    locale.Pick("Not delivered: ", "Не передано: "),
		"thisMachine": locale.Pick("this machine", "эта машина"),
		"theCLI":      locale.Pick("the account", "аккаунт"),
	}
}

// consolePage is the sign-in page of a console: a guided view (provider, machine, one action, where the attempt stands)
// with the terminal under "Troubleshooting". It holds no data of the machine: it asks for the attempt and the screen,
// and shows the screen as text, with the links made clickable node by node, never as markup.
//
// The state shown comes from the machine agent's note when there is one ("connected" only after the provider's status
// check); before that the page reads the screen: no link yet is "preparing", a link is "waiting for the browser", a
// delivered callback is "verifying".
func consolePage() string {
	t := consoleTexts()
	raw, _ := json.Marshal(t)
	texts := strings.ReplaceAll(string(raw), "</", `<\/`)
	e := html.EscapeString
	return `<!doctype html>
<html lang="` + locale.Pick("en", "ru") + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>` + e(t["title"]) + `</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#1c1c1e;--mute:#6b6b70;--line:#d9d9de;--panel:#f5f5f7;--accent:#2563eb;--ok:#15803d;--bad:#b91c1c;--warn:#b45309}
@media (prefers-color-scheme:dark){:root{--bg:#121214;--fg:#ececf0;--mute:#9a9aa3;--line:#2e2e34;--panel:#1b1b1f;--accent:#7aa2ff;--ok:#4ade80;--bad:#f87171;--warn:#fbbf24}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:640px;margin:0 auto;padding:28px 16px;display:grid;gap:16px}
h1{margin:0;font-size:18px;font-weight:600}
.sub{margin:2px 0 0;color:var(--mute);font-size:13px}
.card{border:1px solid var(--line);border-radius:12px;padding:16px;display:grid;gap:10px;background:var(--panel)}
.status{display:flex;align-items:center;gap:10px;font-weight:600}
.dot{width:10px;height:10px;border-radius:50%;background:var(--mute);flex:none}
[data-state=waiting] .dot,[data-state=verifying] .dot,[data-state=preparing] .dot{background:var(--accent);animation:pulse 1.2s ease-in-out infinite}
[data-state=connected] .dot{background:var(--ok)}
[data-state=failed] .dot,[data-state=expired] .dot{background:var(--bad)}
[data-state=cancelled] .dot{background:var(--warn)}
@keyframes pulse{50%{opacity:.35}}
@media (prefers-reduced-motion:reduce){.dot{animation:none!important}}
.card p{margin:0;color:var(--mute)}
.row{display:flex;flex-wrap:wrap;gap:8px;align-items:center}
button,a.btn{font:inherit;padding:8px 14px;border:1px solid var(--line);border-radius:8px;background:var(--bg);color:var(--fg);cursor:pointer;text-decoration:none}
a.btn.primary,button.primary{background:var(--accent);border-color:var(--accent);color:#fff}
[hidden]{display:none!important}
#left{color:var(--mute);font-size:13px}
details{border:1px solid var(--line);border-radius:12px;overflow:hidden}
summary{padding:10px 14px;cursor:pointer;font-weight:600;font-size:13px}
.inner{border-top:1px solid var(--line);display:grid;gap:10px;padding:12px 14px}
.inner p{margin:0;color:var(--mute);font-size:13px}
#screen{margin:0;padding:10px 12px;max-height:320px;overflow:auto;font:13px/1.4 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere;background:var(--panel);border-radius:8px}
#screen a{color:var(--accent)}
form{display:flex;gap:8px}
input{flex:1;min-width:0;font:inherit;padding:8px 10px;border:1px solid var(--line);border-radius:8px;background:var(--bg);color:var(--fg)}
.keys{display:flex;flex-wrap:wrap;gap:6px}
.keys button{padding:4px 10px;font-size:13px}
#state{color:var(--mute);font-size:13px;min-height:1.2em}
</style>
</head>
<body>
<main>
<header><h1 id="head">` + e(t["title"]) + `</h1><p class="sub" id="sub"></p></header>
<section class="card" id="card" data-state="preparing" aria-live="polite">
<div class="status"><span class="dot"></span><span id="stateTitle"></span></div>
<p id="stateHint"></p>
<div class="row"><a class="btn primary" id="openBtn" href="#" target="_blank" rel="noopener noreferrer" hidden>` + e(t["open"]) + `</a><button type="button" id="cancelBtn">` + e(t["cancel"]) + `</button><span id="left"></span></div>
</section>
<details id="cb" hidden><summary>` + e(t["cbTitle"]) + `</summary><div class="inner"><p>` + e(t["cbHint"]) + `</p><form id="cbform" autocomplete="off"><input id="cburl" type="url" spellcheck="false" autocapitalize="off" autocorrect="off" placeholder="` + e(t["cbInput"]) + `" aria-label="` + e(t["cbInput"]) + `"><button type="submit">` + e(t["cbSend"]) + `</button></form><p id="cbstate"></p></div></details>
<details id="term"><summary>` + e(t["trouble"]) + `</summary><div class="inner"><p id="termHint">` + e(t["troubleH"]) + `</p>
<pre id="screen" aria-live="off"></pre>
<form id="form" autocomplete="off"><input id="text" type="text" spellcheck="false" autocapitalize="off" autocorrect="off" placeholder="` + e(t["input"]) + `" aria-label="` + e(t["input"]) + `"><button type="submit">` + e(t["send"]) + `</button></form>
<div class="keys" id="keys"></div><div id="state"></div></div></details>
</main>
<script>
(function () {
  var T = ` + texts + `;
  var m = location.pathname.match(/^(.*)\/console\/([0-9a-f]{16})\/?$/);
  if (!m) return;
  var api = m[1] + '/api/console/' + m[2];
  var $ = function (id) { return document.getElementById(id); };
  var screen = $('screen'), state = $('state'), text = $('text'), last = null, ended = false, sent = false, link = '', final = '';
  var FINAL = { connected: 1, failed: 1, expired: 1, cancelled: 1 };
  function linkOf(t) {
    var re = /https:\/\/[^\s"'<>]+/g, hit, first = '';
    while ((hit = re.exec(t))) { if (!first) first = hit[0]; }
    return first;
  }
  function show(t) {
    if (t === last) return;
    last = t;
    var stick = screen.scrollHeight - screen.scrollTop - screen.clientHeight < 40;
    screen.textContent = '';
    var re = /https?:\/\/[^\s]+/g, pos = 0, hit;
    while ((hit = re.exec(t))) {
      screen.append(t.slice(pos, hit.index));
      var a = document.createElement('a');
      a.href = hit[0]; a.textContent = hit[0]; a.target = '_blank'; a.rel = 'noopener noreferrer';
      screen.append(a);
      pos = hit.index + hit[0].length;
    }
    screen.append(t.slice(pos));
    if (stick) screen.scrollTop = screen.scrollHeight;
  }
  var termTouched = false;
  $('term').addEventListener('toggle', function () { termTouched = true; });
  function render(d) {
    var at = d.attempt || {}, cbk = d.callback || {};
    var ports = cbk.ports || [];
    var s;
    if (FINAL[at.state]) s = at.state;
    else if (cbk.used || (ended && !at.state)) s = 'verifying';
    else if (link) s = 'waiting';
    else s = 'preparing';
    if (s === 'preparing' && ended) s = 'failed';
    final = FINAL[s] ? s : '';
    var who = at.label || T.theCLI;
    $('head').textContent = T.signinTo + ' ' + who;
    $('sub').textContent = T.onMachine + ' ' + (at.machine || T.thisMachine);
    document.title = T.signinTo + ' ' + who;
    $('card').setAttribute('data-state', s);
    $('stateTitle').textContent = T[s];
    $('stateHint').textContent = T[s + 'H'];
    var ob = $('openBtn');
    ob.hidden = s !== 'waiting' || !link;
    if (link) ob.href = link;
    $('cancelBtn').hidden = !!final;
    var ms = at.expiresAt ? at.expiresAt - Date.now() : 0;
    $('left').textContent = !final && ms > 0 ? T.left.replace('{n}', Math.ceil(ms / 60000)) : '';
    // The manual way is offered only while a loopback listener waits and nothing was delivered yet.
    $('cb').hidden = !(s === 'waiting' && ports.length > 0 && !cbk.used);
    // A sign-in that waits on no loopback port may want something typed: show the terminal then, or when it failed.
    var need = (s === 'waiting' && ports.length === 0) || s === 'failed';
    $('termHint').textContent = need && s === 'waiting' ? T.termOpenH : T.troubleH;
    if (!termTouched) $('term').open = need;
  }
  function poll() {
    if ((ended && final) || document.hidden) return;
    fetch(api + '/screen', { credentials: 'same-origin', cache: 'no-store' }).then(function (r) {
      if (!r.ok) throw new Error(r.status);
      return r.json();
    }).then(function (d) {
      show(d.screen || '');
      link = linkOf(d.screen || '') || link;
      ended = !d.alive;
      render(d);
      state.textContent = ended ? T.ended : '';
    }).catch(function () { state.textContent = T.offline; });
  }
  $('cancelBtn').addEventListener('click', function () {
    fetch(api + '/cancel', { method: 'POST', credentials: 'same-origin' }).then(function () { setTimeout(poll, 100); });
  });
  $('cbform').addEventListener('submit', function (e) {
    e.preventDefault();
    var u = $('cburl').value.trim();
    if (!u || ended) return;
    fetch(api + '/callback', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url: u }) })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, d: d }; }); })
      .then(function (x) {
        if (x.ok) { sent = true; $('cburl').value = ''; $('cbstate').textContent = T.cbDone; setTimeout(poll, 300); }
        else $('cbstate').textContent = T.cbFailed + ((x.d && x.d.error) || '');
      }).catch(function () { $('cbstate').textContent = T.offline; });
  });
  function send(body) {
    if (ended) return;
    fetch(api + '/keys', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
      .then(function () { setTimeout(poll, 150); }).catch(function () { state.textContent = T.offline; });
  }
  $('form').addEventListener('submit', function (e) {
    e.preventDefault();
    send({ text: text.value, keys: ['Enter'] });
    text.value = '';
  });
  [['Enter', 'Enter'], ['Esc', 'Escape'], ['Tab', 'Tab'], ['↑', 'Up'], ['↓', 'Down'], ['←', 'Left'], ['→', 'Right'], ['Ctrl+C', 'C-c']].forEach(function (k) {
    var b = document.createElement('button');
    b.type = 'button'; b.textContent = k[0];
    b.addEventListener('click', function () { send({ keys: [k[1]] }); text.focus(); });
    $('keys').append(b);
  });
  document.addEventListener('visibilitychange', poll);
  setInterval(poll, 800);
  poll();
})();
</script>
</body>
</html>
`
}
