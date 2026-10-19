(function () {
  'use strict';
  var script = document.currentScript;
  var id = script && script.getAttribute('data-artifact');
  if (!id || (window.parent !== window.top && window.parent !== window)) return;

  var errors = [];
  var onError = null;
  var onControl = function () {};
  // The live log an agent reads with `{{cli}} design logs`: a few dozen entries a page at most.
  var reported = 0;
  function report(kind, message) {
    if (reported >= 40) return;
    reported += 1;
    var page = location.pathname.replace(/^\/a\/[^/]+\/p\//, '') + location.search;
    try { fetch('/__lib/log?a=' + encodeURIComponent(id) + '&k=' + kind + '&p=' + encodeURIComponent(page) + '&m=' + encodeURIComponent(String(message).slice(0, 600)), { cache: 'no-store', keepalive: true }).catch(function () {}); } catch (e) {}
  }
  window.addEventListener('error', function (event) {
    var error = event.error || {};
    var message = String(error.message || event.message || 'Ошибка скрипта');
    errors.push({ message: message, stack: String(error.stack || '') });
    report('error', (error.stack || message) + (event.filename ? ' @ ' + event.filename + ':' + event.lineno : ''));
    if (onError) onError();
  });
  window.addEventListener('unhandledrejection', function (event) {
    var reason = event.reason || {};
    errors.push({ message: String(reason.message || reason), stack: String(reason.stack || '') });
    report('rejection', reason.stack || reason.message || reason);
    if (onError) onError();
  });
  try {
    new PerformanceObserver(function (list) {
      list.getEntries().forEach(function (entry) { if (entry.duration >= 200) report('longtask', 'Главный поток занят ' + Math.round(entry.duration) + ' мс'); });
    }).observe({ type: 'longtask', buffered: true });
  } catch (e) {}
  window.addEventListener('load', function () {
    setTimeout(function () {
      var nav = performance.getEntriesByType('navigation')[0];
      if (nav && nav.loadEventStart >= 3000) {
        var heavy = performance.getEntriesByType('resource').sort(function (a, b) { return b.duration - a.duration; }).slice(0, 3)
          .map(function (r) { return r.name.split('/').pop() + ' ' + Math.round(r.duration) + ' мс'; }).join(', ');
        report('slow', 'Загрузка ' + Math.round(nav.loadEventStart) + ' мс; дольше всех: ' + heavy);
      }
    }, 0);
  });

  var current = null;
  var scrollKey = function () { return 'tk-lib-scroll:' + location.pathname + location.search; };
  try {
    var saved = sessionStorage.getItem(scrollKey());
    if (saved) {
      sessionStorage.removeItem(scrollKey());
      window.addEventListener('load', function () { window.scrollTo(0, Number(saved) || 0); });
    }
  } catch (e) {}
  // TCK-2844. A change is applied in place, the way Claude Design streams edits into an open board: the board's own
  // template and logic go to the runtime (__dcUpdate), a stylesheet is swapped, a page script of a wrapper board is
  // run again and the page drawn anew. Only what cannot be applied reloads the page. Changes come as a stream;
  // a slow poll catches what a stream that a proxy holds back would miss.
  var PREFIX = '/a/' + encodeURIComponent(id) + '/p/';
  function projectPath(href) {
    try {
      var u = new URL(href, location.href);
      if (u.origin !== location.origin || u.pathname.indexOf(PREFIX) !== 0) return null;
      return decodeURIComponent(u.pathname.slice(PREFIX.length));
    } catch (e) { return null; }
  }
  function loadedFiles() {
    var seen = {};
    performance.getEntriesByType('resource').forEach(function (entry) { var file = projectPath(entry.name); if (file) seen[file] = true; });
    document.querySelectorAll('link[rel="stylesheet"][href],script[src]').forEach(function (node) { var file = projectPath(node.href || node.src); if (file) seen[file] = true; });
    return seen;
  }
  function boardParts(text) {
    var open = /<x-dc(?:\s[^>]*)?>/.exec(text);
    var close = text.lastIndexOf('</x-dc>');
    if (!open || close < open.index) return null;
    var script = new DOMParser().parseFromString(text, 'text/html').querySelector('script[data-dc-script]');
    return {
      outside: text.slice(0, open.index) + text.slice(close),
      template: text.slice(open.index + open[0].length, close),
      js: script ? script.textContent : '',
      props: script ? script.getAttribute('data-props') : null
    };
  }
  var lastBoard = null;
  function fetchBoard() {
    return fetch(location.pathname + location.search, { cache: 'no-store' }).then(function (response) {
      if (!response.ok) throw new Error('HTTP ' + response.status);
      return response.text();
    }).then(boardParts);
  }
  function isDesignBoard() { return typeof window.__dcUpdate === 'function' && typeof window.__dcRootName === 'function'; }
  window.addEventListener('load', function () {
    setTimeout(function () { if (isDesignBoard()) fetchBoard().then(function (parts) { lastBoard = parts; }, function () {}); }, 300);
  });
  function refreshBoard() {
    if (!isDesignBoard()) return Promise.resolve(false);
    return fetchBoard().then(function (next) {
      var prev = lastBoard;
      if (!next || !prev || prev.outside !== next.outside) return false;
      var name = window.__dcRootName();
      if (prev.props !== next.props && next.props !== null) window.__dcUpdate(name, 'props', next.props, false);
      if (prev.js !== next.js) window.__dcUpdate(name, 'js', next.js, false);
      if (prev.template !== next.template || prev.js !== next.js) window.__dcUpdate(name, 'html', next.template, false);
      lastBoard = next;
      return true;
    });
  }
  function swapStyles(file) {
    var swapped = false;
    document.querySelectorAll('link[rel="stylesheet"][href]').forEach(function (link) {
      if (projectPath(link.href) !== file) return;
      var url = new URL(link.href);
      url.searchParams.set('tkh', String(Date.now()));
      var fresh = link.cloneNode();
      fresh.href = url.href;
      fresh.addEventListener('load', function () { link.remove(); });
      fresh.addEventListener('error', function () { fresh.remove(); });
      link.after(fresh);
      swapped = true;
    });
    return swapped;
  }
  function rerunScripts(files) {
    return files.reduce(function (chain, file) {
      return chain.then(function () {
        return new Promise(function (resolve, reject) {
          var old = null;
          document.querySelectorAll('script[src]').forEach(function (node) { if (projectPath(node.src) === file) old = node; });
          if (!old) { reject(new Error('no script')); return; }
          var url = new URL(old.src);
          url.searchParams.set('tkh', String(Date.now()));
          var fresh = document.createElement('script');
          fresh.src = url.href;
          fresh.onload = function () { old.remove(); resolve(); };
          fresh.onerror = function () { fresh.remove(); reject(new Error('script failed')); };
          old.after(fresh);
        });
      });
    }, Promise.resolve());
  }
  function hardReload() {
    try { sessionStorage.setItem(scrollKey(), String(window.scrollY)); } catch (e) {}
    location.reload();
  }
  var applied = function () {};
  function apply(paths) {
    if (!paths.length) return;
    var seen = loadedFiles();
    var me = projectPath(location.href);
    var styles = [];
    var scripts = [];
    var boardChanged = false;
    var reload = false;
    paths.forEach(function (file) {
      if (file === me) boardChanged = true;
      else if (!seen[file]) return;
      else if (/\.css$/i.test(file)) styles.push(file);
      else if (/\.m?js$/i.test(file) && window.{{pages}} && document.getElementById('root')) scripts.push(file);
      else reload = true;
    });
    if (reload || (boardChanged && !isDesignBoard())) { hardReload(); return; }
    if (!boardChanged && !styles.length && !scripts.length) return;
    for (var i = 0; i < styles.length; i += 1) if (!swapStyles(styles[i])) { hardReload(); return; }
    errors = [];
    var chain = Promise.resolve(true);
    if (scripts.length) chain = chain.then(function () { return rerunScripts(scripts); }).then(function () { return drawInPlace(location.href); });
    if (boardChanged) chain = chain.then(function (ok) { return ok && refreshBoard(); });
    chain.then(function (ok) {
      if (ok === false) { hardReload(); return; }
      applied();
      setTimeout(showError, 0);
    }, hardReload);
  }
  function readVersion() {
    return fetch('/__lib/version?a=' + encodeURIComponent(id), { cache: 'no-store' })
      .then(function (response) { return response.ok ? response.json() : null; })
      .then(function (data) { return data && typeof data.version === 'number' ? data.version : null; });
  }
  function catchUp() {
    if (current === null) return readVersion().then(function (version) { current = version; });
    var since = current;
    return readVersion().then(function (version) {
      if (version === null || version === since) return;
      current = version;
      return fetch('/__lib/changes?a=' + encodeURIComponent(id) + '&since=' + since, { cache: 'no-store' })
        .then(function (response) { return response.ok ? response.json() : { paths: [] }; })
        .then(function (data) { apply(data.paths || []); });
    });
  }
  var streaming = false;
  function poll() {
    var wait = streaming ? 5000 : 1000;
    if (document.hidden) wait = 3000;
    setTimeout(function () { (document.hidden ? Promise.resolve() : catchUp()).then(poll, poll); }, wait);
  }
  function listen() {
    if (!window.EventSource) return;
    var source = new EventSource('/__lib/events?a=' + encodeURIComponent(id));
    var failures = 0;
    source.onopen = function () { streaming = true; failures = 0; };
    source.onmessage = function (event) {
      var data = null;
      try { data = JSON.parse(event.data); } catch (e) { return; }
      apply(data && data.paths || []);
      readVersion().then(function (version) { if (version !== null) current = version; }, function () {});
    };
    source.addEventListener('control', function (event) {
      try { onControl(JSON.parse(event.data)); } catch (e) {}
    });
    source.addEventListener('unwatchable', function () { streaming = false; });
    source.onerror = function () {
      streaming = false;
      failures += 1;
      if (failures >= 3) source.close();
    };
  }
  catchUp().then(poll, poll);
  listen();

  var LABELS = {
    Desktop: 'Экран', Mobile: 'Экран', Empty: 'Пусто', Loading: 'Загрузка', ErrorState: 'Ошибка', History: 'История', Blank: 'Пустая',
    Description: 'Описание', Cover: 'Обложка', Venue: 'Площадка', SalesSettings: 'Продажи', PageContent: 'Страница', Media: 'Медиа',
    EventLinks: 'Ссылки', Waves: 'Волны', PromoCodes: 'Промокоды', Blocks: 'Блоки', BlocksLoading: 'Блоки · загрузка',
    Highlights: 'Хайлайты', HighlightsEmpty: 'Хайлайты · пусто', HighlightsLoading: 'Хайлайты · загрузка', HighlightsError: 'Хайлайты · ошибка',
    Albums: 'Альбомы', AlbumsEmpty: 'Альбомы · пусто'
  };
  var ICONS = {"prev":"<svg viewBox=\"0 0 24 24\"><path d=\"M15 18l-6-6 6-6\"/></svg>","next":"<svg viewBox=\"0 0 24 24\"><path d=\"M9 18l6-6-6-6\"/></svg>","down":"<svg viewBox=\"0 0 24 24\"><path d=\"M6 9l6 6 6-6\"/></svg>","sun":"<svg viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"4\"/><path d=\"M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4\"/></svg>","moon":"<svg viewBox=\"0 0 24 24\"><path d=\"M20 14.5A8 8 0 019.5 4a8 8 0 1010.5 10.5z\"/></svg>","hide":"<svg viewBox=\"0 0 24 24\"><path d=\"M5 12h14\"/></svg>","close":"<svg viewBox=\"0 0 24 24\"><path d=\"M6 6l12 12M18 6L6 18\"/></svg>","sliders":"<svg viewBox=\"0 0 24 24\"><path d=\"M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0\"/><circle cx=\"16\" cy=\"6\" r=\"2\"/><circle cx=\"10\" cy=\"12\" r=\"2\"/><circle cx=\"18\" cy=\"18\" r=\"2\"/></svg>","boards":"<svg viewBox=\"0 0 24 24\"><rect x=\"3\" y=\"3\" width=\"7\" height=\"7\" rx=\"1.5\"/><rect x=\"14\" y=\"3\" width=\"7\" height=\"7\" rx=\"1.5\"/><rect x=\"3\" y=\"14\" width=\"7\" height=\"7\" rx=\"1.5\"/><rect x=\"14\" y=\"14\" width=\"7\" height=\"7\" rx=\"1.5\"/></svg>"};
  var COLLAPSED = 'tk-lib-bar-collapsed';
  function label(variant) { return LABELS[variant] || variant; }
  function make(tag, attrs, text) {
    var node = document.createElement(tag);
    for (var name in attrs) node.setAttribute(name, attrs[name]);
    if (text) node.textContent = text;
    return node;
  }
  function iconButton(icon, title) {
    var button = make('button', { type: 'button', class: 'icon', 'aria-label': title, title: title });
    button.innerHTML = ICONS[icon];
    return button;
  }
  function here() { return new URL(location.href); }
  function isBoard(href) {
    var board = new URL(href, location.origin);
    var now = here();
    if (board.pathname !== now.pathname) return false;
    var page = board.searchParams.get('id');
    return page === null || (page === now.searchParams.get('id') && (board.searchParams.get('m') || '') === (now.searchParams.get('m') || ''));
  }
  function carryTheme(href) {
    var target = new URL(href, location.origin);
    var theme = here().searchParams.get('theme');
    if (theme && target.searchParams.has('theme')) target.searchParams.set('theme', theme);
    return target.pathname + target.search;
  }
  function withParam(name, value) {
    var now = here();
    now.searchParams.set(name, value);
    return now.pathname + now.search;
  }
  function stored(key) { try { return localStorage.getItem(key); } catch (e) { return null; } }
  function store(key, value) { try { if (value) localStorage.setItem(key, value); else localStorage.removeItem(key); } catch (e) {} }

  /** A page of a wrapper board is drawn by {{pages}}: another board of the same kind is drawn in place. */
  function drawInPlace(href) {
    var target = new URL(href, location.origin);
    var page = target.searchParams.get('id');
    var root = document.getElementById('root');
    if (!page || !root || target.pathname !== location.pathname || !window.{{pages}} || typeof window.{{pages}}.render !== 'function') return false;
    var fresh = document.createElement('div');
    fresh.id = 'root';
    root.replaceWith(fresh);
    window.{{args}} = target.searchParams.get('m') === '1' ? { viewport: 'mobile' } : undefined;
    var dark = (target.searchParams.get('theme') || 'dark') === 'dark';
    document.documentElement.classList.toggle('dark', dark);
    if (dark) document.documentElement.dataset.theme = 'dark'; else delete document.documentElement.dataset.theme;
    errors = [];
    try {
      window.{{pages}}.render(page, target.searchParams.get('v') || 'Desktop', fresh);
    } catch (error) {
      errors.push({ message: String(error && error.message || error), stack: String(error && error.stack || '') });
    }
    window.scrollTo(0, 0);
    return true;
  }

  var host = make('div', { 'data-tk-library-bar': '' });
  var root = host.attachShadow({ mode: 'open' });
  var style = make('style');
  style.textContent = ":host{all:initial;position:fixed;left:0;right:0;bottom:14px;z-index:2147483646;display:flex;justify-content:center;pointer-events:none;font:500 12.5px/1 system-ui,-apple-system,\"Segoe UI\",Roboto,sans-serif;color-scheme:dark;-webkit-font-smoothing:antialiased}.bar,.pill{pointer-events:auto;display:flex;align-items:center;gap:2px;height:36px;max-width:calc(100vw - 24px);padding:0 4px;box-sizing:border-box;border-radius:10px;background:rgba(24,24,30,.9);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);box-shadow:0 8px 28px rgba(0,0,0,.38);backdrop-filter:blur(10px) saturate(1.4)}.pill{position:fixed;right:14px;bottom:14px;width:36px;padding:0;justify-content:center}button{all:unset;box-sizing:border-box;display:inline-flex;align-items:center;justify-content:center;gap:6px;height:28px;min-width:28px;padding:0 8px;border-radius:7px;cursor:pointer;color:inherit;white-space:nowrap}button.icon{padding:0}button:hover{background:rgba(255,255,255,.09)}button:focus-visible,input:focus-visible{outline:2px solid #a08bff;outline-offset:1px}button[aria-current=\"true\"]{background:rgba(160,139,255,.22);color:#fff}svg{width:16px;height:16px;flex:none;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}button[aria-pressed=\"true\"]{background:rgba(160,139,255,.22);color:#fff}.toast{position:fixed;top:12px;left:50%;transform:translateX(-50%);padding:6px 12px;border-radius:999px;background:rgba(24,24,30,.9);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);font-size:12px;pointer-events:none}.toast[hidden],.tweaks[hidden]{display:none}.tweaks{pointer-events:auto;position:fixed;width:300px;max-width:calc(100vw - 16px);max-height:calc(100vh - 80px);display:flex;flex-direction:column;border-radius:14px;background:rgba(24,24,30,.96);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);box-shadow:0 16px 44px rgba(0,0,0,.5);backdrop-filter:blur(12px) saturate(1.4);font-weight:400}.tweaks-head{display:flex;align-items:center;gap:2px;height:40px;padding:0 6px 0 14px;cursor:grab;user-select:none;touch-action:none;border-bottom:1px solid rgba(255,255,255,.07)}.tweaks[data-folded] .tweaks-head{border-bottom:0}.tweaks-title{flex:1;font-weight:600;font-size:13px}.tweaks-body{overflow:auto;padding:6px 14px 14px;display:flex;flex-direction:column;gap:14px}.section h3{margin:8px 0 8px;font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:#8e8e9a}.control{display:flex;flex-direction:column;gap:6px;margin-bottom:10px}.control-label{font-size:12px;color:#b6b6c0}.chips{display:flex;flex-wrap:wrap;gap:4px}.chips button{height:28px;padding:0 10px;background:rgba(255,255,255,.06);font-size:12px}.chips button[aria-pressed=\"true\"]{background:rgba(160,139,255,.3);color:#fff}.switch{width:36px;min-width:36px;height:20px;padding:0;border-radius:999px;background:rgba(255,255,255,.16);position:relative}.switch::after{content:\"\";position:absolute;left:3px;top:3px;width:14px;height:14px;border-radius:50%;background:#fff;transition:transform .15s}.switch[aria-checked=\"true\"]{background:#7c5cff}.switch[aria-checked=\"true\"]::after{transform:translateX(16px)}.tweaks input{height:30px}.finder{width:min(460px,calc(100vw - 24px));max-height:min(70vh,560px)}.group{position:sticky;top:0;z-index:1;padding:10px 10px 4px;font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:#8e8e9a;background:rgba(24,24,30,.98)}.screen{display:flex;align-items:center;gap:4px;padding:0 4px 0 0;border-radius:8px}.screen:hover{background:rgba(255,255,255,.04)}.list .screen-name{flex:1;width:auto;min-width:0;justify-content:flex-start;padding-left:14px}.list .view{width:auto;flex:none;height:24px;padding:0 8px;font-size:11.5px;color:#a9a9b4;background:rgba(255,255,255,.05)}.list .view[aria-current=\"true\"]{color:#fff}.sep{width:1px;height:18px;margin:0 3px;background:rgba(255,255,255,.12)}.board{max-width:220px;overflow:hidden;text-overflow:ellipsis}.count{color:#8e8e9a;font-variant-numeric:tabular-nums}.muted{color:#8e8e9a}.panel{pointer-events:auto;position:absolute;bottom:46px;left:50%;transform:translateX(-50%);display:flex;flex-direction:column;gap:6px;width:min(400px,calc(100vw - 24px));max-height:min(60vh,500px);padding:6px;box-sizing:border-box;border-radius:12px;background:rgba(24,24,30,.97);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);box-shadow:0 12px 36px rgba(0,0,0,.45)}.panel[hidden],.bar[hidden],.pill[hidden],.error[hidden]{display:none}input{all:unset;box-sizing:border-box;width:100%;height:32px;padding:0 10px;border-radius:8px;background:rgba(255,255,255,.07);color:#fff;font-weight:400}.list{display:flex;flex-direction:column;gap:1px;overflow:auto}.list button{width:100%;flex:none;justify-content:flex-start;font-weight:400;overflow:hidden;text-overflow:ellipsis}.empty{padding:8px 10px;color:#8e8e9a;font-weight:400}.error{pointer-events:auto;position:fixed;left:50%;top:50%;transform:translate(-50%,-50%);width:min(520px,calc(100vw - 32px));box-sizing:border-box;padding:18px 18px 14px;border-radius:14px;background:#1c1c22;color:#e9e9ee;border:1px solid rgba(255,107,107,.35);box-shadow:0 16px 48px rgba(0,0,0,.5);font-weight:400;line-height:1.45}.error h2{margin:0 0 6px;font-size:15px;font-weight:600;color:#ff8f8f}.error p{margin:0 0 10px;color:#b6b6c0}.error pre{margin:0 0 12px;max-height:180px;overflow:auto;padding:10px;border-radius:8px;background:rgba(255,255,255,.05);color:#f1f1f4;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;word-break:break-word}.error .actions{display:flex;gap:6px;flex-wrap:wrap}.error .actions button{background:rgba(255,255,255,.08)}.error .actions button.primary{background:#7c5cff;color:#fff}@media (max-width:560px){.board{max-width:130px}.count{display:none}}";
  root.appendChild(style);

  var toast = make('div', { class: 'toast', role: 'status', hidden: '' });
  root.appendChild(toast);
  var toastTimer = 0;
  applied = function () {
    toast.textContent = 'Обновлено';
    toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { toast.hidden = true; }, 1200);
    renderTweaks();
    reportState();
  };

  var card = make('div', { class: 'error', role: 'alert', hidden: '' });
  root.appendChild(card);
  var boards = [];
  var paint = function () {};
  // TCK-2845. The panel puts the error in the composer by itself: the person presses send, the agent fixes the board.
  var sentErrors = {};
  function sendToChat(error) {
    if (window.parent === window) return false;
    try { window.parent.postMessage({ type: 'tk:board-error', url: location.href, message: error.message, stack: error.stack }, '*'); } catch (e) { return false; }
    return true;
  }
  function showError() {
    if (!errors.length) { card.hidden = true; return; }
    var first = errors[0];
    var key = location.pathname + location.search + '\n' + first.message;
    if (!sentErrors[key]) sentErrors[key] = sendToChat(first) ? 'sent' : 'local';
    var inChat = sentErrors[key] === 'sent';
    card.textContent = '';
    card.appendChild(make('h2', {}, 'Доска не отрисовалась'));
    card.appendChild(make('p', {}, inChat
      ? 'Ошибка уже прикреплена к сообщению в чате: отправьте его — агент найдёт причину в файлах макета и починит.'
      : 'Ошибка в коде самого макета, а не панели: так он падает и в Claude Design.'));
    card.appendChild(make('pre', {}, first.message + (first.stack ? '\n\n' + first.stack.split('\n').slice(0, 6).join('\n') : '')));
    var actions = make('div', { class: 'actions' });
    if (window.parent !== window) {
      var chat = make('button', { type: 'button', class: 'primary' }, inChat ? 'В чате ✓' : 'Отправить в чат');
      chat.addEventListener('click', function () { if (sendToChat(first)) { sentErrors[key] = 'sent'; showError(); } });
      actions.appendChild(chat);
    }
    var copy = make('button', { type: 'button' }, 'Скопировать');
    copy.addEventListener('click', function () {
      var text = location.href + '\n' + first.message + '\n' + first.stack;
      if (navigator.clipboard) navigator.clipboard.writeText(text).then(function () { copy.textContent = 'Скопировано'; }, function () {});
    });
    var hide = make('button', { type: 'button' }, 'Скрыть');
    hide.addEventListener('click', function () { card.hidden = true; });
    actions.appendChild(copy);
    if (boards.length > 1) {
      var skip = make('button', { type: 'button' }, 'Следующая доска');
      skip.addEventListener('click', function () { step(1); });
      actions.appendChild(skip);
    }
    actions.appendChild(hide);
    card.appendChild(actions);
    card.hidden = false;
  }
  onError = function () { setTimeout(showError, 0); };

  function go(href, push) {
    var target = carryTheme(href);
    if (drawInPlace(target)) {
      if (push !== false) history.pushState(null, '', target);
      paint();
      setTimeout(showError, 0);
      return;
    }
    location.href = target;
  }
  function step(delta) {
    if (!boards.length) return;
    var index = -1;
    for (var i = 0; i < boards.length; i += 1) if (isBoard(boards[i].href)) { index = i; break; }
    go(boards[index < 0 ? 0 : (index + delta + boards.length) % boards.length].href);
  }
  window.addEventListener('popstate', function () {
    if (!drawInPlace(location.href)) { location.reload(); return; }
    paint();
    setTimeout(showError, 0);
  });

  fetch('/__lib/boards?a=' + encodeURIComponent(id), { cache: 'no-store' })
    .then(function (response) { return response.ok ? response.json() : null; })
    .then(function (data) { if (data && data.boards && data.boards.length) mount(data.boards); }, function () {});

  // TCK-2839. «Состояния»: a floating panel with the board's settings — screen state, theme, desktop or phone, and
  // every prop the board declares (data-props: flags like a skeleton, enums, text). It moves, folds and closes;
  // an agent drives it too ({{cli}} design panel), and the page tells the server what it shows.
  var PANEL_POS = 'tk-lib-tweaks-pos';
  var PANEL_OPEN = 'tk-lib-tweaks-open';
  var PANEL_FOLDED = 'tk-lib-tweaks-folded';
  var THEMES = [{ value: 'dark', text: 'Тёмная' }, { value: 'light', text: 'Светлая' }];
  var MOBILE_SUFFIX = ' · Мобильная';
  var PROP_LABELS = { variant: 'Состояние', theme: 'Тема', loading: 'Загрузка', skeleton: 'Скелетон', empty: 'Пусто', error: 'Ошибка' };
  var tweaks = make('section', { class: 'tweaks', 'aria-label': 'Состояния доски', hidden: '' });
  root.appendChild(tweaks);
  var tweaksOpen = stored(PANEL_OPEN) === '1';
  var tweaksFolded = stored(PANEL_FOLDED) === '1';
  function currentBoard() {
    for (var i = 0; i < boards.length; i += 1) if (isBoard(boards[i].href)) return boards[i];
    return null;
  }
  function siblingOf(board, phone) {
    if (!board) return null;
    var base = board.title.split(MOBILE_SUFFIX).join('');
    var wanted = phone ? base + MOBILE_SUFFIX : base;
    for (var i = 0; i < boards.length; i += 1) if (boards[i].title === wanted) return boards[i];
    return null;
  }
  function readControls() {
    var items = [];
    if (isDesignBoard()) {
      var name = window.__dcRootName();
      var entry = window.__dcRegistry && window.__dcRegistry[name];
      var meta = entry && entry.propsMeta || {};
      var overrides = entry && entry.propOverrides || {};
      Object.keys(meta).forEach(function (key) {
        var item = meta[key] || {};
        items.push({
          name: key,
          label: item.label || PROP_LABELS[key] || key,
          section: item.section || 'Свойства',
          editor: item.editor || typeof item.default,
          options: (item.options || []).map(function (option) { return { value: option, text: label(String(option)) }; }),
          value: Object.prototype.hasOwnProperty.call(overrides, key) ? overrides[key] : item.default,
          live: true
        });
      });
      return items;
    }
    var board = currentBoard();
    var tweak = board && board.tweaks;
    if (tweak && tweak.variants && tweak.variants.length > 1) {
      items.push({ name: 'variant', label: 'Состояние', section: 'Экран', editor: 'enum', value: here().searchParams.get('v') || tweak.variants[0],
        options: tweak.variants.map(function (variant) { return { value: variant, text: label(variant) }; }) });
    }
    if (tweak && tweak.themes) items.push({ name: 'theme', label: 'Тема', section: 'Экран', editor: 'enum', value: here().searchParams.get('theme') || 'dark', options: THEMES });
    return items;
  }
  function setControl(item, value) {
    if (item.live) {
      var name = window.__dcRootName();
      var entry = window.__dcRegistry && window.__dcRegistry[name];
      var next = {};
      var current = entry && entry.propOverrides || {};
      for (var key in current) next[key] = current[key];
      next[item.name] = value;
      window.__dcSetProps(name, next);
      setTimeout(function () { renderTweaks(); reportState(); }, 0);
      return;
    }
    if (item.name === 'variant') go(withParam('v', String(value)));
    if (item.name === 'theme') go(withParam('theme', String(value)));
  }
  function chips(options, isOn, pick) {
    var row = make('div', { class: 'chips', role: 'group' });
    options.forEach(function (option) {
      var chip = make('button', { type: 'button', 'aria-pressed': String(isOn(option)) }, option.text);
      chip.addEventListener('click', function () { pick(option); });
      row.appendChild(chip);
    });
    return row;
  }
  function controlRow(item) {
    var row = make('div', { class: 'control' });
    row.appendChild(make('span', { class: 'control-label' }, item.label));
    if (item.editor === 'boolean') {
      var toggle = make('button', { type: 'button', role: 'switch', class: 'switch', 'aria-checked': String(item.value === true), 'aria-label': item.label });
      toggle.addEventListener('click', function () { setControl(item, item.value !== true); });
      row.appendChild(toggle);
    } else if (item.options && item.options.length) {
      row.appendChild(chips(item.options, function (option) { return option.value === item.value; }, function (option) { setControl(item, option.value); }));
    } else {
      var field = make('input', { type: item.editor === 'number' ? 'number' : 'text', value: item.value === undefined || item.value === null ? '' : String(item.value), 'aria-label': item.label });
      var commit = function () { setControl(item, item.editor === 'number' ? Number(field.value) : field.value); };
      field.addEventListener('change', commit);
      row.appendChild(field);
    }
    return row;
  }
  function renderTweaks() {
    tweaks.hidden = !tweaksOpen || !boards.length;
    if (tweaks.hidden) return;
    tweaks.textContent = '';
    tweaks.toggleAttribute('data-folded', tweaksFolded);
    var head = make('header', { class: 'tweaks-head' });
    head.appendChild(make('span', { class: 'tweaks-title' }, 'Состояния'));
    var fold = iconButton(tweaksFolded ? 'down' : 'hide', tweaksFolded ? 'Развернуть' : 'Свернуть');
    fold.addEventListener('click', function () { tweaksFolded = !tweaksFolded; store(PANEL_FOLDED, tweaksFolded ? '1' : ''); renderTweaks(); reportState(); });
    var close = iconButton('close', 'Закрыть');
    close.addEventListener('click', function () { showTweaks(false); });
    head.appendChild(fold);
    head.appendChild(close);
    tweaks.appendChild(head);
    dragBy(head);
    if (tweaksFolded) return;
    var body = make('div', { class: 'tweaks-body' });
    var board = currentBoard();
    var desktop = siblingOf(board, false);
    var phone = siblingOf(board, true);
    if (board && desktop && phone) {
      var view = make('div', { class: 'section' });
      view.appendChild(make('h3', {}, 'Вид'));
      var onPhone = board === phone;
      view.appendChild(chips([{ value: 'desktop', text: 'Десктоп' }, { value: 'phone', text: 'Телефон' }],
        function (option) { return (option.value === 'phone') === onPhone; },
        function (option) { var target = option.value === 'phone' ? phone : desktop; if (target !== board) go(target.href); }));
      body.appendChild(view);
    }
    var sections = {};
    var order = [];
    readControls().forEach(function (item) {
      if (!sections[item.section]) { sections[item.section] = []; order.push(item.section); }
      sections[item.section].push(item);
    });
    order.forEach(function (name) {
      var section = make('div', { class: 'section' });
      section.appendChild(make('h3', {}, name));
      sections[name].forEach(function (item) { section.appendChild(controlRow(item)); });
      body.appendChild(section);
    });
    if (!order.length && !(desktop && phone)) body.appendChild(make('p', { class: 'muted' }, 'У этой доски нет настроек: попросите агента добавить свойства в data-props.'));
    tweaks.appendChild(body);
    placeTweaks();
  }
  function placeTweaks() {
    var saved = null;
    try { saved = JSON.parse(stored(PANEL_POS) || 'null'); } catch (e) {}
    var width = tweaks.offsetWidth || 300;
    var height = tweaks.offsetHeight || 200;
    var x = saved ? saved.x : window.innerWidth - width - 16;
    var y = saved ? saved.y : 56;
    tweaks.style.left = Math.max(8, Math.min(x, window.innerWidth - width - 8)) + 'px';
    tweaks.style.top = Math.max(8, Math.min(y, window.innerHeight - Math.min(height, 120) - 8)) + 'px';
  }
  function dragBy(handle) {
    handle.addEventListener('pointerdown', function (event) {
      if (event.button !== 0 || event.composedPath()[0].closest('button')) return;
      var startX = event.clientX - tweaks.offsetLeft;
      var startY = event.clientY - tweaks.offsetTop;
      handle.setPointerCapture(event.pointerId);
      var move = function (moved) { store(PANEL_POS, JSON.stringify({ x: moved.clientX - startX, y: moved.clientY - startY })); placeTweaks(); };
      var up = function () { handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', up); };
      handle.addEventListener('pointermove', move);
      handle.addEventListener('pointerup', up);
    });
  }
  window.addEventListener('resize', function () { if (!tweaks.hidden) placeTweaks(); });
  function showTweaks(open) {
    tweaksOpen = open;
    store(PANEL_OPEN, open ? '1' : '');
    renderTweaks();
    paint();
    reportState();
  }
  var stateTimer = 0;
  function reportState() {
    clearTimeout(stateTimer);
    stateTimer = setTimeout(function () {
      var board = currentBoard();
      var state = {
        board: board ? { title: board.title, path: projectPath(location.href) } : null,
        panel: tweaksOpen ? (tweaksFolded ? 'folded' : 'open') : 'closed',
        controls: readControls().map(function (item) { return { name: item.name, value: item.value, options: (item.options || []).map(function (option) { return option.value; }), editor: item.editor }; })
      };
      try { fetch('/__lib/state?a=' + encodeURIComponent(id) + '&s=' + encodeURIComponent(JSON.stringify(state).slice(0, 4000)), { cache: 'no-store' }).catch(function () {}); } catch (e) {}
    }, 250);
  }
  function findBoard(name) {
    for (var i = 0; i < boards.length; i += 1) {
      var item = boards[i];
      if (item.title === name || projectPath(new URL(item.href, location.href).href) === name) return item;
    }
    return null;
  }
  /** A command of an agent: { board, view, panel: show|hide|fold|unfold, props: { name: value } }. */
  function control(command) {
    if (!command || typeof command !== 'object') return;
    var board = currentBoard();
    if (command.board) { var target = findBoard(String(command.board)); if (target && target !== board) { go(target.href); board = target; } }
    if (command.view === 'phone' || command.view === 'desktop') {
      var other = siblingOf(board, command.view === 'phone');
      if (other && other !== board) go(other.href);
    }
    if (command.panel === 'show' || command.panel === 'unfold') { tweaksFolded = false; store(PANEL_FOLDED, ''); showTweaks(true); }
    if (command.panel === 'fold') { tweaksFolded = true; store(PANEL_FOLDED, '1'); showTweaks(true); }
    if (command.panel === 'hide') showTweaks(false);
    if (command.props && typeof command.props === 'object') {
      setTimeout(function () {
        readControls().forEach(function (item) {
          if (Object.prototype.hasOwnProperty.call(command.props, item.name)) setControl(item, command.props[item.name]);
        });
      }, 150);
    }
  }
  onControl = control;

  /** Boards grouped like the admin's menu: a section, its screens, and a desktop and a phone version of each. */
  function boardGroups(needle) {
    var groups = [];
    var bySection = {};
    boards.forEach(function (item) {
      if (needle && item.title.toLowerCase().indexOf(needle) < 0) return;
      var phone = item.title.indexOf(MOBILE_SUFFIX) >= 0;
      var base = item.title.split(MOBILE_SUFFIX).join('');
      var cut = base.indexOf(' - ');
      var section = cut < 0 ? base : base.slice(0, cut);
      var screen = cut < 0 ? 'Главная' : base.slice(cut + 3);
      if (!bySection[section]) { bySection[section] = { name: section, screens: [], byScreen: {} }; groups.push(bySection[section]); }
      var group = bySection[section];
      if (!group.byScreen[screen]) { group.byScreen[screen] = { name: screen, desktop: null, phone: null }; group.screens.push(group.byScreen[screen]); }
      group.byScreen[screen][phone ? 'phone' : 'desktop'] = item;
    });
    return groups;
  }
  function mount(list) {
    boards = list;
    var byWrapper = {};
    boards.forEach(function (item) { if (item.wrapper) byWrapper[new URL(item.wrapper, location.origin).pathname] = item.href; });
    document.addEventListener('click', function (event) {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      var link = event.target && event.target.closest ? event.target.closest('a[href]') : null;
      if (!link) return;
      var page = byWrapper[new URL(link.getAttribute('href'), location.href).pathname];
      if (!page) return;
      event.preventDefault();
      location.href = carryTheme(page);
    });

    var bar = make('div', { class: 'bar', role: 'toolbar', 'aria-label': 'Доски макета Claude Design' });
    var pill = make('div', { class: 'pill' });
    var expand = iconButton('boards', 'Показать панель досок');
    pill.appendChild(expand);
    var panels = [];
    var closeAll = function () { panels.forEach(function (item) { item.close(); }); };
    var popover = function (trigger, title, searchable, items) {
      var panel = make('div', { class: 'panel', role: 'dialog', 'aria-label': title, hidden: '' });
      var search = searchable ? make('input', { type: 'search', placeholder: 'Найти доску', 'aria-label': 'Найти доску', autocomplete: 'off' }) : null;
      var rows = make('div', { class: 'list' });
      if (search) panel.appendChild(search);
      panel.appendChild(rows);
      var render = function () {
        var needle = search ? search.value.trim().toLowerCase() : '';
        rows.textContent = '';
        var found = 0;
        items.forEach(function (item) {
          if (needle && item.text.toLowerCase().indexOf(needle) < 0) return;
          found += 1;
          var row = make('button', { type: 'button', 'aria-current': String(item.current), title: item.text }, item.text);
          row.addEventListener('click', function () { closeAll(); item.pick(); });
          rows.appendChild(row);
        });
        if (!found) rows.appendChild(make('div', { class: 'empty' }, 'Такой доски нет'));
      };
      var entry = {
        close: function () { panel.hidden = true; trigger.setAttribute('aria-expanded', 'false'); },
        open: function () {
          closeAll();
          panel.hidden = false;
          trigger.setAttribute('aria-expanded', 'true');
          if (search) search.value = '';
          render();
          var mark = rows.querySelector('[aria-current="true"]');
          if (mark) mark.scrollIntoView({ block: 'nearest' });
          (search || mark || rows.querySelector('button') || trigger).focus();
        }
      };
      trigger.setAttribute('aria-haspopup', 'dialog');
      trigger.setAttribute('aria-expanded', 'false');
      trigger.addEventListener('click', function () { if (panel.hidden) entry.open(); else entry.close(); });
      panel.addEventListener('keydown', function (event) {
        if (event.key === 'Escape') { entry.close(); trigger.focus(); }
        if (event.key === 'Enter' && search && event.composedPath()[0] === search) { var first = rows.querySelector('button'); if (first) first.click(); }
      });
      if (search) search.addEventListener('input', render);
      panels.push(entry);
      root.appendChild(panel);
      return entry;
    };

    var boardFinder = function (trigger, board) {
      var panel = make('div', { class: 'panel finder', role: 'dialog', 'aria-label': 'Доски макета', hidden: '' });
      var search = make('input', { type: 'search', placeholder: 'Найти доску', 'aria-label': 'Найти доску', autocomplete: 'off' });
      var rows = make('div', { class: 'list' });
      panel.appendChild(search);
      panel.appendChild(rows);
      var first = null;
      var render = function () {
        rows.textContent = '';
        first = null;
        var groups = boardGroups(search.value.trim().toLowerCase());
        groups.forEach(function (group) {
          rows.appendChild(make('div', { class: 'group' }, group.name));
          group.screens.forEach(function (screen) {
            var main = screen.desktop || screen.phone;
            var row = make('div', { class: 'screen' });
            var open = make('button', { type: 'button', class: 'screen-name', title: main.title, 'aria-current': String(board !== null && (screen.desktop === board || screen.phone === board)) }, screen.name);
            open.addEventListener('click', function () { closeAll(); go(main.href); });
            if (!first) first = open;
            row.appendChild(open);
            [['desktop', 'Десктоп'], ['phone', 'Телефон']].forEach(function (pair) {
              var item = screen[pair[0]];
              if (!item) return;
              var chip = make('button', { type: 'button', class: 'view', 'aria-current': String(item === board), title: item.title }, pair[1]);
              chip.addEventListener('click', function () { closeAll(); go(item.href); });
              row.appendChild(chip);
            });
            rows.appendChild(row);
          });
        });
        if (!groups.length) rows.appendChild(make('div', { class: 'empty' }, 'Такой доски нет'));
      };
      var entry = {
        close: function () { panel.hidden = true; trigger.setAttribute('aria-expanded', 'false'); },
        open: function () {
          closeAll();
          panel.hidden = false;
          trigger.setAttribute('aria-expanded', 'true');
          search.value = '';
          render();
          var mark = rows.querySelector('.screen-name[aria-current="true"]');
          if (mark) mark.scrollIntoView({ block: 'center' });
          search.focus();
        }
      };
      trigger.setAttribute('aria-haspopup', 'dialog');
      trigger.setAttribute('aria-expanded', 'false');
      trigger.addEventListener('click', function () { if (panel.hidden) entry.open(); else entry.close(); });
      panel.addEventListener('keydown', function (event) {
        if (event.key === 'Escape') { entry.close(); trigger.focus(); }
        if (event.key === 'Enter' && event.composedPath()[0] === search && first) first.click();
      });
      search.addEventListener('input', render);
      panels.push(entry);
      root.appendChild(panel);
      return entry;
    };

    paint = function () {
      panels.forEach(function (item) { item.close(); });
      Array.prototype.slice.call(root.querySelectorAll('.panel')).forEach(function (node) { node.remove(); });
      panels = [];
      bar.textContent = '';
      var index = -1;
      for (var i = 0; i < boards.length; i += 1) if (isBoard(boards[i].href)) { index = i; break; }
      var board = index >= 0 ? boards[index] : null;

      var prev = iconButton('prev', 'Предыдущая доска · [');
      prev.addEventListener('click', function () { step(-1); });
      var pick = make('button', { type: 'button', title: 'Все доски' });
      pick.appendChild(make('span', { class: 'board' }, board ? board.title : 'Доски'));
      if (index >= 0) pick.appendChild(make('span', { class: 'count' }, index + 1 + '/' + boards.length));
      var caret = make('span', {});
      caret.innerHTML = ICONS.down;
      pick.appendChild(caret);
      var next = iconButton('next', 'Следующая доска · ]');
      next.addEventListener('click', function () { step(1); });
      bar.appendChild(prev);
      bar.appendChild(pick);
      bar.appendChild(next);
      boardFinder(pick, board);

      bar.appendChild(make('span', { class: 'sep' }));
      var gear = iconButton('sliders', tweaksOpen ? 'Скрыть состояния' : 'Состояния доски: экран, тема, свойства');
      gear.setAttribute('aria-pressed', String(tweaksOpen));
      gear.addEventListener('click', function () { showTweaks(!tweaksOpen); });
      bar.appendChild(gear);
      bar.appendChild(make('span', { class: 'sep' }));
      var collapse = iconButton('hide', 'Свернуть панель досок');
      collapse.addEventListener('click', function () { setCollapsed(true); expand.focus(); });
      bar.appendChild(collapse);
      renderTweaks();
      reportState();
    };

    document.addEventListener('pointerdown', function (event) { if (event.composedPath().indexOf(host) < 0) closeAll(); }, true);
    var setCollapsed = function (on) {
      bar.hidden = on;
      pill.hidden = !on;
      if (on) closeAll();
      store(COLLAPSED, on ? '1' : '');
    };
    expand.addEventListener('click', function () { setCollapsed(false); });
    document.addEventListener('keydown', function (event) {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      var target = event.composedPath()[0];
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName || ''))) return;
      if (event.key === '[') { event.preventDefault(); step(-1); }
      if (event.key === ']') { event.preventDefault(); step(1); }
      if (event.key === 's' || event.key === 'ы') { event.preventDefault(); showTweaks(!tweaksOpen); }
    });

    root.appendChild(bar);
    root.appendChild(pill);
    paint();
    setCollapsed(stored(COLLAPSED) === '1');
    showError();
  }

  var attach = function () { document.documentElement.appendChild(host); showError(); };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', attach); else attach();
})();
