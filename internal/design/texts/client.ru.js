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
  // A change is applied in place, the way Claude Design streams edits into an open board: the board's own
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
  var ICONS = {"search":"<svg viewBox=\"0 0 24 24\"><circle cx=\"11\" cy=\"11\" r=\"7\"/><path d=\"M20 20l-3.5-3.5\"/></svg>","check":"<svg viewBox=\"0 0 24 24\"><path d=\"M5 12.5l4.5 4.5L19 7\"/></svg>","desktop":"<svg viewBox=\"0 0 24 24\"><rect x=\"3\" y=\"4\" width=\"18\" height=\"12\" rx=\"2\"/><path d=\"M8 20h8M12 16v4\"/></svg>","phone":"<svg viewBox=\"0 0 24 24\"><rect x=\"7\" y=\"2.5\" width=\"10\" height=\"19\" rx=\"2.5\"/><path d=\"M11 18.5h2\"/></svg>","prev":"<svg viewBox=\"0 0 24 24\"><path d=\"M15 18l-6-6 6-6\"/></svg>","next":"<svg viewBox=\"0 0 24 24\"><path d=\"M9 18l6-6-6-6\"/></svg>","down":"<svg viewBox=\"0 0 24 24\"><path d=\"M6 9l6 6 6-6\"/></svg>","sun":"<svg viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"4\"/><path d=\"M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4\"/></svg>","moon":"<svg viewBox=\"0 0 24 24\"><path d=\"M20 14.5A8 8 0 019.5 4a8 8 0 1010.5 10.5z\"/></svg>","hide":"<svg viewBox=\"0 0 24 24\"><path d=\"M5 12h14\"/></svg>","close":"<svg viewBox=\"0 0 24 24\"><path d=\"M6 6l12 12M18 6L6 18\"/></svg>","sliders":"<svg viewBox=\"0 0 24 24\"><path d=\"M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0\"/><circle cx=\"16\" cy=\"6\" r=\"2\"/><circle cx=\"10\" cy=\"12\" r=\"2\"/><circle cx=\"18\" cy=\"18\" r=\"2\"/></svg>","boards":"<svg viewBox=\"0 0 24 24\"><rect x=\"3\" y=\"3\" width=\"7\" height=\"7\" rx=\"1.5\"/><rect x=\"14\" y=\"3\" width=\"7\" height=\"7\" rx=\"1.5\"/><rect x=\"3\" y=\"14\" width=\"7\" height=\"7\" rx=\"1.5\"/><rect x=\"14\" y=\"14\" width=\"7\" height=\"7\" rx=\"1.5\"/></svg>"};
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
  style.textContent = ":host([data-hosted]) .bar,:host([data-hosted]) .pill{display:none}:host{all:initial;position:fixed;left:0;right:0;bottom:14px;z-index:2147483646;display:flex;justify-content:center;pointer-events:none;font:500 12.5px/1 system-ui,-apple-system,\"Segoe UI\",Roboto,sans-serif;color-scheme:dark;-webkit-font-smoothing:antialiased}.bar,.pill{pointer-events:auto;display:flex;align-items:center;gap:2px;height:36px;max-width:calc(100vw - 24px);padding:0 4px;box-sizing:border-box;border-radius:10px;background:rgba(24,24,30,.9);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);box-shadow:0 8px 28px rgba(0,0,0,.38);backdrop-filter:blur(10px) saturate(1.4)}.pill{position:fixed;right:14px;bottom:14px;width:36px;padding:0;justify-content:center}button{all:unset;box-sizing:border-box;display:inline-flex;align-items:center;justify-content:center;gap:6px;height:28px;min-width:28px;padding:0 8px;border-radius:7px;cursor:pointer;color:inherit;white-space:nowrap}button.icon{padding:0}button:hover{background:rgba(255,255,255,.09)}button:focus-visible,input:focus-visible{outline:2px solid #a08bff;outline-offset:1px}button[aria-current=\"true\"]{background:rgba(160,139,255,.22);color:#fff}svg{width:16px;height:16px;flex:none;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}button[aria-pressed=\"true\"]{background:rgba(160,139,255,.22);color:#fff}.toast{position:fixed;top:12px;left:50%;transform:translateX(-50%);padding:6px 12px;border-radius:999px;background:rgba(24,24,30,.9);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);font-size:12px;pointer-events:none}.toast[hidden],.tweaks[hidden]{display:none}.tweaks{pointer-events:auto;position:fixed;width:300px;max-width:calc(100vw - 16px);max-height:calc(100vh - 80px);display:flex;flex-direction:column;border-radius:14px;background:rgba(24,24,30,.96);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);box-shadow:0 16px 44px rgba(0,0,0,.5);backdrop-filter:blur(12px) saturate(1.4);font-weight:400}.tweaks-head{display:flex;align-items:center;gap:2px;height:40px;padding:0 6px 0 14px;cursor:grab;user-select:none;touch-action:none;border-bottom:1px solid rgba(255,255,255,.07)}.tweaks[data-folded] .tweaks-head{border-bottom:0}.tweaks-title{flex:1;font-weight:600;font-size:13px}.tweaks-body{overflow:auto;padding:6px 14px 14px;display:flex;flex-direction:column;gap:14px}.section h3{margin:8px 0 8px;font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:#8e8e9a}.control{display:flex;flex-direction:column;gap:6px;margin-bottom:10px}.control-label{font-size:12px;color:#b6b6c0}.chips{display:flex;flex-wrap:wrap;gap:4px}.chips button{height:28px;padding:0 10px;background:rgba(255,255,255,.06);font-size:12px}.chips button[aria-pressed=\"true\"]{background:rgba(160,139,255,.3);color:#fff}.switch{width:36px;min-width:36px;height:20px;padding:0;border-radius:999px;background:rgba(255,255,255,.16);position:relative}.switch::after{content:\"\";position:absolute;left:3px;top:3px;width:14px;height:14px;border-radius:50%;background:#fff;transition:transform .15s}.switch[aria-checked=\"true\"]{background:#7c5cff}.switch[aria-checked=\"true\"]::after{transform:translateX(16px)}.tweaks input{height:30px}.finder{width:min(380px,calc(100vw - 24px));max-height:min(64vh,520px);padding:0;gap:0;bottom:48px;overflow:hidden;border-radius:14px;background:rgba(22,22,28,.97);box-shadow:0 0 0 .5px rgba(255,255,255,.04),0 18px 48px rgba(0,0,0,.5),0 4px 12px rgba(0,0,0,.25);opacity:0;pointer-events:none;transform:translateX(-50%) translateY(6px) scale(.97);transform-origin:50% 100%;transition:opacity .14s ease,transform .14s cubic-bezier(.16,1,.3,1)}.finder[data-open]{opacity:1;pointer-events:auto;transform:translateX(-50%) scale(1)}.finder .search{position:relative;flex:none;padding:8px 8px 4px}.finder .search svg{position:absolute;left:19px;top:17px;width:16px;height:16px;color:#7f7f8e;pointer-events:none}.finder input{height:34px;padding:0 10px 0 32px;border-radius:9px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);font-size:13px;transition:border-color .12s,box-shadow .12s,background .12s}.finder input::placeholder{color:#7f7f8e}.finder input:hover{border-color:rgba(255,255,255,.14)}.finder input:focus,.finder input:focus-visible{outline:0;background:rgba(255,255,255,.06);border-color:rgba(160,139,255,.5);box-shadow:0 0 0 3px rgba(160,139,255,.12)}.finder .list{gap:0;padding:2px 6px 8px;overscroll-behavior:contain;scrollbar-width:thin;scrollbar-color:rgba(255,255,255,.16) transparent}.finder .group{position:static;padding:12px 10px 4px;font-size:11.5px;font-weight:500;letter-spacing:0;text-transform:none;color:#8a8a9a;background:none}.finder .list>.group:first-child{padding-top:6px}.finder .item{width:100%;height:36px;gap:10px;padding:0 10px 0 8px;justify-content:flex-start;border-radius:9px;font-weight:400;font-size:13px;transition:background .1s}.finder .item:hover{background:rgba(255,255,255,.06)}.finder .item:active{background:rgba(255,255,255,.09)}.finder .item:focus-visible{outline:0;background:rgba(255,255,255,.07);box-shadow:inset 0 0 0 1.5px rgba(160,139,255,.55)}.finder .thumb{display:inline-flex;align-items:center;justify-content:center;flex:none;width:24px;height:24px;border-radius:7px;background:rgba(255,255,255,.06);color:#a3a3b2}.finder .thumb svg{width:14px;height:14px}.finder .name{flex:0 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis;color:#ececf1}.finder .dev{flex:none;margin-left:-5px;font-size:12px;color:#7f7f8e}.finder .tick{flex:none;margin-left:auto;display:inline-flex;color:#b9a8ff}.finder .tick svg{width:15px;height:15px;stroke-width:2.2}.finder .item[aria-current=\"true\"]{background:rgba(160,139,255,.12);color:#fff}.finder .item[aria-current=\"true\"]:hover{background:rgba(160,139,255,.18)}.finder .item[aria-current=\"true\"] .thumb{background:rgba(160,139,255,.22);color:#d3c9ff}.finder .item[aria-current=\"true\"] .name{color:#fff;font-weight:500}.finder .empty{padding:18px 10px;text-align:center;color:#8a8a9a}@media (prefers-reduced-motion:reduce){.finder,.finder input,.finder .item{transition:none}}.sep{width:1px;height:18px;margin:0 3px;background:rgba(255,255,255,.12)}.board{max-width:220px;overflow:hidden;text-overflow:ellipsis}.count{color:#8e8e9a;font-variant-numeric:tabular-nums}.muted{color:#8e8e9a}.panel{pointer-events:auto;position:absolute;bottom:46px;left:50%;transform:translateX(-50%);display:flex;flex-direction:column;gap:6px;width:min(400px,calc(100vw - 24px));max-height:min(60vh,500px);padding:6px;box-sizing:border-box;border-radius:12px;background:rgba(24,24,30,.97);color:#e9e9ee;border:1px solid rgba(255,255,255,.1);box-shadow:0 12px 36px rgba(0,0,0,.45)}.panel[hidden],.bar[hidden],.pill[hidden],.error[hidden]{display:none}input{all:unset;box-sizing:border-box;width:100%;height:32px;padding:0 10px;border-radius:8px;background:rgba(255,255,255,.07);color:#fff;font-weight:400}.list{display:flex;flex-direction:column;gap:1px;overflow:auto}.list button{width:100%;flex:none;justify-content:flex-start;font-weight:400;overflow:hidden;text-overflow:ellipsis}.empty{padding:8px 10px;color:#8e8e9a;font-weight:400}.error{pointer-events:auto;position:fixed;left:50%;top:50%;transform:translate(-50%,-50%);width:min(520px,calc(100vw - 32px));box-sizing:border-box;padding:18px 18px 14px;border-radius:14px;background:#1c1c22;color:#e9e9ee;border:1px solid rgba(255,107,107,.35);box-shadow:0 16px 48px rgba(0,0,0,.5);font-weight:400;line-height:1.45}.error h2{margin:0 0 6px;font-size:15px;font-weight:600;color:#ff8f8f}.error p{margin:0 0 10px;color:#b6b6c0}.error pre{margin:0 0 12px;max-height:180px;overflow:auto;padding:10px;border-radius:8px;background:rgba(255,255,255,.05);color:#f1f1f4;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;word-break:break-word}.error .actions{display:flex;gap:6px;flex-wrap:wrap}.error .actions button{background:rgba(255,255,255,.08)}.error .actions button.primary{background:#7c5cff;color:#fff}@media (max-width:560px){.board{max-width:130px}.count{display:none}.finder{left:8px;right:8px;width:auto;max-height:min(68vh,520px);border-radius:16px;transform:translateY(10px) scale(.98)}.finder[data-open]{transform:none}.finder input{font-size:16px;height:38px}.finder .search svg{top:19px}.finder .item{height:42px}}";
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
  // The panel puts the error in the composer by itself: the person presses send, the agent fixes the board.
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

  // "States": a floating panel with the board's settings — screen state, theme, desktop or phone, and
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
          min: item.min, max: item.max, step: item.step,
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
      setTimeout(function () { renderTweaks(); reportState(); tellHost(false); }, 0);
      return;
    }
    if (item.name === 'variant') go(withParam('v', String(value)));
    if (item.name === 'theme') go(withParam('theme', String(value)));
    setTimeout(function () { tellHost(false); }, 0);
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
    tweaks.hidden = hosted || !tweaksOpen || !boards.length;
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
  // A host that frames the board (the Designly site) draws these settings itself: it says so, the page then tells it
  // its controls and takes its changes, and the panel above stays shut. Messages are read only from the framing window.
  var parentWin = window.parent !== window ? window.parent : null;
  // The host that frames the board: the settings go only to it and are taken only from it.
  var hostOrigin = (function () {
    try {
      if (location.ancestorOrigins && location.ancestorOrigins.length) return location.ancestorOrigins[0];
      return document.referrer ? new URL(document.referrer).origin : null;
    } catch (e) { return null; }
  })();
  if (!hostOrigin) parentWin = null;
  // Only a platform page is talked to. On this machine's address the board is framed by the platform on another loopback
  // port, by a configured platform host or by the library itself (the server's frame-ancestors says the same); shown
  // through a preview slot (any other address), the slot's own frame-ancestors lets only the apps that opened it frame it.
  var PLATFORM_HOSTS = {{hosts}};
  var LOOPBACK = /^http:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/;
  if (hostOrigin && hostOrigin !== location.origin && LOOPBACK.test(location.origin) && !LOOPBACK.test(hostOrigin) && PLATFORM_HOSTS.indexOf(hostOrigin) < 0) parentWin = null;
  var hosted = false;
  var told = '';
  var HOST_TEXT_MAX = 2000;
  function plain(value) { return value === null || value === undefined || typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean'; }
  function tellHost(force) {
    if (!hosted || !parentWin) return;
    var items = readControls().filter(function (item) { return plain(item.value); }).map(function (item) {
      return {
        name: String(item.name), label: String(item.label), section: String(item.section), editor: String(item.editor),
        value: item.value === undefined ? null : item.value,
        options: (item.options || []).filter(function (option) { return plain(option.value); }).map(function (option) { return { value: option.value, text: String(option.text) }; }),
        min: typeof item.min === 'number' ? item.min : null, max: typeof item.max === 'number' ? item.max : null, step: typeof item.step === 'number' ? item.step : null
      };
    });
    var signature = JSON.stringify(items);
    if (!force && signature === told) return;
    told = signature;
    try { parentWin.postMessage({ type: 'tk:tweaks', controls: items }, hostOrigin); } catch (e) {}
  }
  function acceptable(item, value) {
    if (item.options && item.options.length) return item.options.some(function (option) { return option.value === value; });
    if (item.editor === 'boolean') return typeof value === 'boolean';
    if (item.editor === 'number') return typeof value === 'number' && isFinite(value);
    return typeof value === 'string' && value.length <= HOST_TEXT_MAX;
  }
  window.addEventListener('message', function (event) {
    var data = event.data;
    if (!parentWin || event.source !== parentWin || event.origin !== hostOrigin || !data || typeof data !== 'object') return;
    if (data.type === 'tk:tweaks-host') {
      hosted = data.on !== false;
      tweaksOpen = hosted && data.open === true;
      // The host draws its own boards bar over the frame: ours would be a second one.
      if (hosted) host.setAttribute('data-hosted', ''); else host.removeAttribute('data-hosted');
      renderTweaks();
      reportState();
      tellHost(true);
    }
    if (data.type === 'tk:tweak' && hosted && typeof data.name === 'string') {
      readControls().forEach(function (item) { if (item.name === data.name && acceptable(item, data.value)) setControl(item, data.value); });
    }
  });
  // What the board has to offer grows after its first paint (the runtime draws it, the list of boards arrives): look again.
  setInterval(function () { tellHost(false); }, 1000);
  if (parentWin) { try { parentWin.postMessage({ type: 'tk:lib-ready' }, hostOrigin); } catch (e) {} }

  // The canvas's zoom lives in the host: a pinch (a wheel with ctrl on a trackpad), ⌘/Ctrl + wheel and Safari's gesture
  // over the board would zoom the browser's page or nothing. They are told to the host as a wheel's delta at the cursor
  // (`tk:wheel`); the plain wheel stays the board's own scroll.
  if (parentWin) {
    var tellCanvas = function (message) { try { parentWin.postMessage(message, hostOrigin); } catch (e) {} };
    var wheelOf = function (event, dy) { tellCanvas({ type: 'tk:wheel', x: Number(event.clientX) || 0, y: Number(event.clientY) || 0, dy: dy }); };
    window.addEventListener('wheel', function (event) {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      wheelOf(event, Number(event.deltaY) || 0);
    }, { capture: true, passive: false });
    var gestureScale = 1;
    window.addEventListener('gesturestart', function (event) { event.preventDefault(); gestureScale = 1; }, { capture: true, passive: false });
    window.addEventListener('gesturechange', function (event) {
      event.preventDefault();
      var scale = Number(event.scale) || 1;
      wheelOf(event, -Math.log(scale / gestureScale) / 0.01);
      gestureScale = scale;
    }, { capture: true, passive: false });

    // On a canvas (`tk:canvas` from the host), what moves the canvas is told to the host instead of being the board's, as
    // the slot's bridge does: Space held (the canvas's hand; not in a field being typed in) and a drag with the middle
    // button, in the screen's pixels. The wheel stays the board's own scroll.
    var onCanvas = false;
    var spaceDown = false;
    var middle = null;
    var typing = function (target) {
      return !!target && typeof target.closest === 'function' &&
        target.closest('input:not([type="button"]):not([type="checkbox"]):not([type="radio"]):not([type="submit"]), textarea, select, [contenteditable]:not([contenteditable="false"])') !== null;
    };
    var tellSpace = function (down) {
      if (spaceDown === down) return;
      spaceDown = down;
      tellCanvas({ type: 'tk:space', down: down });
    };
    window.addEventListener('message', function (event) {
      var data = event.data;
      if (event.source !== parentWin || event.origin !== hostOrigin || !data || data.type !== 'tk:canvas' || typeof data.on !== 'boolean') return;
      onCanvas = data.on;
      if (!onCanvas) { tellSpace(false); middle = null; }
    });
    window.addEventListener('keydown', function (event) {
      if (!onCanvas || event.code !== 'Space' || event.ctrlKey || event.metaKey || event.altKey || typing(event.target)) return;
      event.preventDefault();
      if (!event.repeat) tellSpace(true);
    }, true);
    window.addEventListener('keyup', function (event) {
      if (event.code !== 'Space' || !spaceDown) return;
      event.preventDefault();
      tellSpace(false);
    }, true);
    window.addEventListener('blur', function () { tellSpace(false); });
    var middleDown = function (event) {
      if (!onCanvas || event.button !== 1) return;
      // No autoscroll and no paste of the selection: the press is the canvas's.
      event.preventDefault();
      if (event.type === 'pointerdown') middle = { x: Number(event.screenX) || 0, y: Number(event.screenY) || 0 };
    };
    window.addEventListener('pointerdown', middleDown, true);
    window.addEventListener('mousedown', middleDown, true);
    window.addEventListener('pointermove', function (event) {
      if (!middle) return;
      var x = Number(event.screenX) || 0, y = Number(event.screenY) || 0;
      tellCanvas({ type: 'tk:pan', dx: x - middle.x, dy: y - middle.y, by: 'drag' });
      middle = { x: x, y: y };
    }, true);
    var middleUp = function (event) { if (middle && (event.type !== 'pointerup' || event.button === 1)) middle = null; };
    window.addEventListener('pointerup', middleUp, true);
    window.addEventListener('pointercancel', middleUp, true);
  }

  // The host's DevTools read the page's DOM through this bridge: a branch of the tree asked for by a path of element
  // indices (bounded in depth and size), a node's box and computed styles, a frame drawn over a node, and one click that
  // picks a node. Only the host that framed the page is answered (the origin check above), and what goes back is tags,
  // ids, classes, a few words of a node's own text and numbers: never attribute values, scripts or styles' text.
  var DOM_SKIP = { SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, TEMPLATE: 1 };
  var DOM_PATH_MAX = 64;
  var DOM_DEPTH_MAX = 6;
  var DOM_NODES_MAX = 400;
  var DOM_KIDS_MAX = 120;
  var DOM_STYLES = ['display', 'position', 'width', 'height', 'margin', 'padding', 'color', 'background-color', 'font-family', 'font-size', 'font-weight', 'line-height', 'border-radius', 'opacity', 'z-index', 'overflow'];
  var domMarkPath = null;
  var domInspecting = false;
  var domWatching = false;
  var domMark = null;
  function tellHostDom(message) {
    if (!parentWin) return;
    try { parentWin.postMessage(message, hostOrigin); } catch (e) {}
  }
  function domOurs(node) {
    return !!(node && node.nodeType === 1 && (node === domMark || node === host || (node.closest && node.closest('[data-tk-library-bar],[data-tk-library-mark]')) || node.hasAttribute('data-artifact')));
  }
  function domKids(el) {
    return Array.prototype.filter.call(el.children, function (child) { return !domOurs(child); });
  }
  function domPathOf(el) {
    var path = [];
    while (el && el !== document.documentElement) {
      var parent = el.parentElement;
      if (!parent) return null;
      path.unshift(domKids(parent).indexOf(el));
      if (path[0] < 0) return null;
      el = parent;
    }
    return path.length > DOM_PATH_MAX ? null : path;
  }
  function domAt(path) {
    var el = document.documentElement;
    for (var i = 0; i < path.length; i += 1) {
      el = domKids(el)[path[i]];
      if (!el) return null;
    }
    return el;
  }
  function domReadPath(value) {
    if (!Array.isArray(value) || value.length > DOM_PATH_MAX) return null;
    for (var i = 0; i < value.length; i += 1) if (typeof value[i] !== 'number' || value[i] % 1 !== 0 || value[i] < 0 || value[i] > 100000) return null;
    return value;
  }
  function domWords(el) {
    if (DOM_SKIP[el.tagName]) return '';
    var own = Array.prototype.filter.call(el.childNodes, function (child) { return child.nodeType === 3; }).map(function (child) { return child.nodeValue; }).join(' ').replace(/\s+/g, ' ').trim();
    return own.slice(0, 60);
  }
  function domNode(el, depth, budget) {
    var kids = domKids(el);
    var node = { tag: String(el.localName).slice(0, 32), n: kids.length };
    if (el.id) node.id = String(el.id).slice(0, 64);
    var classes = typeof el.className === 'string' ? el.className.trim() : '';
    if (classes) node.cls = classes.replace(/\s+/g, ' ').slice(0, 120);
    var words = domWords(el);
    if (words) node.text = words;
    budget.left -= 1;
    if (depth > 0 && kids.length) {
      node.kids = [];
      for (var i = 0; i < kids.length && i < DOM_KIDS_MAX && budget.left > 0; i += 1) node.kids.push(domNode(kids[i], depth - 1, budget));
      if (node.kids.length < kids.length) node.more = kids.length - node.kids.length;
    }
    return node;
  }
  function domBox(style, name) {
    var sides = ['top', 'right', 'bottom', 'left'].map(function (side) { return style.getPropertyValue(name + '-' + side); });
    return sides[0] === sides[1] && sides[1] === sides[2] && sides[2] === sides[3] ? sides[0] : sides.join(' ');
  }
  function domStyles(el) {
    var style = getComputedStyle(el);
    var out = {};
    DOM_STYLES.forEach(function (name) {
      var value = name === 'margin' || name === 'padding' ? domBox(style, name) : style.getPropertyValue(name);
      out[name] = String(value).slice(0, 120);
    });
    return out;
  }
  function domPlaceMark() {
    var el = domMarkPath ? domAt(domMarkPath) : null;
    if (!el || !domMark) { if (domMark) domMark.style.setProperty('display', 'none'); return; }
    var rect = el.getBoundingClientRect();
    var set = function (name, value) { domMark.style.setProperty(name, value); };
    set('display', 'block');
    set('left', rect.left + 'px');
    set('top', rect.top + 'px');
    set('width', rect.width + 'px');
    set('height', rect.height + 'px');
    var label = domMark.firstChild;
    var classes = typeof el.className === 'string' && el.className.trim() ? '.' + el.className.trim().split(/\s+/).slice(0, 3).join('.') : '';
    label.firstChild.textContent = String(el.localName) + (el.id ? '#' + el.id : '') + classes;
    label.lastChild.textContent = Math.round(rect.width) + ' × ' + Math.round(rect.height);
    // The label stands above the box; a box at the top edge takes it inside.
    label.style.setProperty('top', rect.top < 28 ? '6px' : 'auto');
    label.style.setProperty('bottom', rect.top < 28 ? 'auto' : 'calc(100% + 6px)');
    label.style.setProperty('left', rect.top < 28 ? '6px' : '0');
  }
  function domEnsureMark() {
    if (domMark) return;
    domMark = make('div', { 'data-tk-library-mark': '' });
    var label = make('span');
    label.appendChild(make('span'));
    label.appendChild(make('span'));
    domMark.appendChild(label);
    var box = { position: 'fixed', display: 'none', 'z-index': '2147483645', 'pointer-events': 'none', 'box-sizing': 'border-box', background: 'rgba(111, 168, 220, .36)', outline: '1px solid rgba(111, 168, 220, .95)' };
    Object.keys(box).forEach(function (name) { domMark.style.setProperty(name, box[name]); });
    var tag = { position: 'absolute', display: 'flex', gap: '8px', padding: '4px 8px', 'border-radius': '4px', background: '#1f2430', color: '#7dd3fc', font: '12px/16px ui-monospace, SFMono-Regular, Menlo, monospace', 'white-space': 'nowrap' };
    Object.keys(tag).forEach(function (name) { label.style.setProperty(name, tag[name]); });
    label.lastChild.style.setProperty('color', 'rgba(255, 255, 255, .7)');
    document.documentElement.appendChild(domMark);
  }
  function domMarkNode(path, reveal) {
    domMarkPath = path;
    if (path) {
      domEnsureMark();
      var el = domAt(path);
      if (el && reveal) { try { el.scrollIntoView({ block: 'nearest', inline: 'nearest' }); } catch (e) {} }
    }
    domPlaceMark();
  }
  var domFrame = 0;
  function domReplace() {
    if (!domMarkPath || domFrame) return;
    domFrame = requestAnimationFrame(function () { domFrame = 0; domPlaceMark(); });
  }
  window.addEventListener('scroll', domReplace, true);
  window.addEventListener('resize', domReplace);
  var domChangeTimer = 0;
  var domObserver = null;
  function domChanged() {
    clearTimeout(domChangeTimer);
    domChangeTimer = setTimeout(function () { tellHostDom({ type: 'tk:dom-changed' }); }, 400);
  }
  function domWatch(on) {
    domWatching = on;
    window[on ? 'addEventListener' : 'removeEventListener']('resize', domChanged);
    if (!on) { if (domObserver) domObserver.disconnect(); domObserver = null; return; }
    if (domObserver) return;
    domObserver = new MutationObserver(function (records) {
      var real = records.some(function (record) {
        if (domOurs(record.target)) return false;
        if (record.type !== 'childList') return true;
        return !Array.prototype.concat.call([], Array.prototype.slice.call(record.addedNodes), Array.prototype.slice.call(record.removedNodes)).every(domOurs);
      });
      if (real) domChanged();
    });
    domObserver.observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'id'] });
  }
  function domInspect(on) {
    if (domInspecting === on) return;
    domInspecting = on;
    var listen = on ? 'addEventListener' : 'removeEventListener';
    window[listen]('mousemove', domOver, true);
    ['click', 'mousedown', 'mouseup', 'pointerdown', 'pointerup', 'touchstart', 'touchend'].forEach(function (name) { window[listen](name, domSwallow, true); });
    window[listen]('keydown', domEscape, true);
    if (!on) domMarkNode(null, false);
  }
  function domTarget(event) {
    var el = event.target;
    return el && el.nodeType === 1 && !domOurs(el) ? el : null;
  }
  function domOver(event) {
    var el = domTarget(event);
    if (!el) return;
    domMarkNode(domPathOf(el), false);
  }
  function domSwallow(event) {
    if (!domTarget(event)) return;
    event.preventDefault();
    event.stopPropagation();
    if (event.type !== 'click') return;
    var path = domPathOf(event.target);
    domInspect(false);
    if (path) tellHostDom({ type: 'tk:dom-picked', path: path });
    else tellHostDom({ type: 'tk:dom-inspect', on: false });
  }
  function domEscape(event) {
    if (event.key !== 'Escape') return;
    domInspect(false);
    tellHostDom({ type: 'tk:dom-inspect', on: false });
  }
  window.addEventListener('message', function (event) {
    var data = event.data;
    if (!parentWin || event.source !== parentWin || event.origin !== hostOrigin || !data || typeof data !== 'object') return;
    if (data.type === 'tk:dom-tree') {
      var path = domReadPath(data.path);
      var req = typeof data.req === 'number' ? data.req : 0;
      if (!path) return;
      var el = domAt(path);
      var depth = Math.max(0, Math.min(DOM_DEPTH_MAX, typeof data.depth === 'number' ? Math.floor(data.depth) : 1));
      if (!el) { tellHostDom({ type: 'tk:dom-tree', req: req, path: path, gone: true }); return; }
      tellHostDom({ type: 'tk:dom-tree', req: req, path: path, node: domNode(el, depth, { left: DOM_NODES_MAX }) });
    }
    if (data.type === 'tk:dom-node') {
      var at = domReadPath(data.path);
      var ask = typeof data.req === 'number' ? data.req : 0;
      if (!at) return;
      var node = domAt(at);
      if (!node) { tellHostDom({ type: 'tk:dom-node', req: ask, path: at, gone: true }); return; }
      var rect = node.getBoundingClientRect();
      tellHostDom({
        type: 'tk:dom-node', req: ask, path: at, tag: String(node.localName).slice(0, 32), id: node.id ? String(node.id).slice(0, 64) : '',
        cls: typeof node.className === 'string' ? node.className.trim().replace(/\s+/g, ' ').slice(0, 120) : '', text: domWords(node),
        rect: { x: Math.round(rect.left + window.scrollX), y: Math.round(rect.top + window.scrollY), w: Math.round(rect.width), h: Math.round(rect.height) },
        viewport: { w: window.innerWidth, h: window.innerHeight }, styles: domStyles(node)
      });
    }
    if (data.type === 'tk:dom-mark') domMarkNode(data.path === null ? null : domReadPath(data.path), data.reveal === true);
    if (data.type === 'tk:dom-inspect' && typeof data.on === 'boolean') domInspect(data.on);
    if (data.type === 'tk:dom-watch' && typeof data.on === 'boolean') domWatch(data.on);
  });
  // The blocks the host's notes are pinned to (`tk:anchors`, by selector): the board tells where each shows (`tk:anchored`)
  // whenever something may have moved it, as the slot's bridge does (lib/hl-preview/bridge-core.ts): the whole box, the
  // part every clipping ancestor and the viewport leave, cut by what the board lays over a whole side, and whether
  // something covers all of it. Only to and from the host that framed the board; one look per frame, sent on a change.
  // A board shown through a preview slot has the slot's bridge too: it picks and follows the blocks, this script does not.
  var slotBridge = !!document.querySelector('script[src*="/__tk/bridge.js"]');
  var ANCHORS_MAX = 64;
  var anchorItems = [];
  var anchorFound = {};
  var anchorFrame = 0;
  var anchorLast = '';
  var anchorRunning = 0;
  var anchorBusyUntil = 0;
  var anchorSlow = 0;
  var anchorSizes = window.ResizeObserver ? new ResizeObserver(function () { anchorQueue(); }) : null;
  var anchorChanges = window.MutationObserver ? new MutationObserver(function () { anchorQueue(); }) : null;
  function anchorMeet(a, b) {
    var x = Math.max(a.x, b.x), y = Math.max(a.y, b.y);
    var right = Math.min(a.x + a.w, b.x + b.w), bottom = Math.min(a.y + a.h, b.y + b.h);
    return right > x && bottom > y ? { x: x, y: y, w: right - x, h: bottom - y } : null;
  }
  function anchorTrim(a, b) {
    var x = a.x, y = a.y, right = a.x + a.w, bottom = a.y + a.h;
    if (b.x <= x + 0.5 && b.x + b.w >= right - 0.5) {
      if (b.y <= y + 0.5 && b.y + b.h > y) y = b.y + b.h;
      else if (b.y + b.h >= bottom - 0.5 && b.y < bottom) bottom = b.y;
    } else if (b.y <= y + 0.5 && b.y + b.h >= bottom - 0.5) {
      if (b.x <= x + 0.5 && b.x + b.w > x) x = b.x + b.w;
      else if (b.x + b.w >= right - 0.5 && b.x < right) right = b.x;
    }
    return right > x && bottom > y ? { x: x, y: y, w: right - x, h: bottom - y } : null;
  }
  function anchorParent(node) { return node.assignedSlot || node.parentElement || (node.parentNode && node.parentNode.host) || null; }
  function anchorHolds(outer, inner) {
    for (var node = inner; node; node = anchorParent(node)) if (node === outer) return true;
    return false;
  }
  function anchorHoldsFixed(style) {
    return (style.transform && style.transform !== 'none') || (style.filter && style.filter !== 'none') ||
      (style.perspective && style.perspective !== 'none') || /paint|layout|strict|content/.test(String(style.contain || ''));
  }
  function anchorClip(el, box) {
    var far = 1e7, left = box, escape = getComputedStyle(el).position;
    for (var node = anchorParent(el); node && left; node = anchorParent(node)) {
      if (node === document.documentElement || node === document.body || node.nodeType !== 1) continue;
      var style = getComputedStyle(node);
      if (escape === 'fixed' && !anchorHoldsFixed(style)) continue;
      if (escape === 'absolute' && style.position === 'static' && !anchorHoldsFixed(style)) continue;
      escape = style.position;
      var cutX = style.overflowX !== 'visible', cutY = style.overflowY !== 'visible';
      if (!cutX && !cutY) continue;
      var rect = node.getBoundingClientRect();
      var kx = node.offsetWidth > 0 ? rect.width / node.offsetWidth : 1;
      var ky = node.offsetHeight > 0 ? rect.height / node.offsetHeight : 1;
      left = anchorMeet(left, {
        x: cutX ? rect.left + node.clientLeft * kx : -far, y: cutY ? rect.top + node.clientTop * ky : -far,
        w: cutX ? node.clientWidth * kx : 2 * far, h: cutY ? node.clientHeight * ky : 2 * far
      });
    }
    return left;
  }
  function anchorUncover(el, shown) {
    function own(hit) { return !hit || anchorHolds(el, hit) || anchorHolds(hit, el); }
    function layer(hit) {
      var top = hit;
      for (var node = anchorParent(hit); node; node = anchorParent(node)) {
        if (node === document.body || node === document.documentElement || anchorHolds(node, el)) break;
        if (getComputedStyle(node).pointerEvents === 'none') break;
        top = node;
      }
      return top;
    }
    if (getComputedStyle(el).pointerEvents === 'none') return { shown: shown, covered: false };
    var left = shown;
    for (var round = 0; round < 4; round += 1) {
      var xs = left.w < 3 ? [left.x + left.w / 2] : [left.x + 1, left.x + left.w / 2, left.x + left.w - 1];
      var ys = left.h < 3 ? [left.y + left.h / 2] : [left.y + 1, left.y + left.h / 2, left.y + left.h - 1];
      var hits = [];
      ys.forEach(function (y) { xs.forEach(function (x) { hits.push(document.elementFromPoint(x, y)); }); });
      var others = hits.filter(function (hit, i) { return !own(hit) && hits.indexOf(hit) === i; });
      if (!others.length) return { shown: left, covered: false };
      var cut = false;
      for (var i = 0; i < others.length; i += 1) {
        var r = layer(others[i]).getBoundingClientRect();
        var next = anchorTrim(left, { x: r.left, y: r.top, w: r.width, h: r.height });
        if (!next) return { shown: null, covered: true };
        if (next.x !== left.x || next.y !== left.y || next.w !== left.w || next.h !== left.h) cut = true;
        left = next;
      }
      if (!cut) return hits.every(function (hit) { return !own(hit); }) ? { shown: null, covered: true } : { shown: left, covered: false };
    }
    return { shown: left, covered: false };
  }
  function anchorPack(b) {
    function round(v) { return Math.round(v * 100) / 100; }
    return b ? [round(b.x), round(b.y), round(b.w), round(b.h)] : null;
  }
  function anchorQuery(selector) {
    try { return selector ? document.querySelector(selector) : null; } catch (e) { return null; }
  }
  // By the number of its pick while it is the block its selector names (a number from a page loaded before may name
  // another block now), else the first that the selector finds.
  function anchorFind(item) {
    var el = item.pick === null ? null : pickedBlocks.get(item.pick);
    if (!el || !el.isConnected) return anchorQuery(item.selector);
    var fits = true;
    try { fits = !item.selector || el.matches(item.selector); } catch (e) { fits = true; }
    return fits ? el : anchorQuery(item.selector) || el;
  }
  function anchorLook() {
    anchorFrame = 0;
    if (!anchorItems.length || !parentWin) return;
    var root = document.documentElement;
    var view = { x: 0, y: 0, w: root.clientWidth || innerWidth, h: root.clientHeight || innerHeight };
    var out = anchorItems.map(function (item) {
      var el = anchorFound[item.key];
      if (!el || !el.isConnected) {
        el = anchorFind(item);
        if (el) { anchorFound[item.key] = el; if (anchorSizes) anchorSizes.observe(el); } else delete anchorFound[item.key];
      }
      if (!el) return { key: item.key, box: null, shown: null, covered: false };
      try {
        var r = el.getBoundingClientRect();
        var box = { x: r.left, y: r.top, w: r.width, h: r.height };
        var reach = anchorClip(el, box);
        var inView = reach ? anchorMeet(reach, view) : null;
        var seen = inView ? anchorUncover(el, inView) : { shown: null, covered: false };
        return { key: item.key, box: anchorPack(box), shown: anchorPack(seen.shown), covered: seen.covered };
      } catch (e) {
        return { key: item.key, box: null, shown: null, covered: false };
      }
    });
    var message = { type: 'tk:anchored', vw: view.w, vh: view.h, items: out };
    var text = JSON.stringify(message);
    if (text !== anchorLast) { anchorLast = text; try { parentWin.postMessage(message, hostOrigin); } catch (e) {} }
    // While something animates, every frame is looked at; a lost end event stops it after a while.
    if (anchorRunning > 0 && Date.now() < anchorBusyUntil) anchorQueue(); else anchorRunning = 0;
  }
  function anchorQueue() { if (!anchorFrame && anchorItems.length) anchorFrame = requestAnimationFrame(anchorLook); }
  window.addEventListener('scroll', anchorQueue, { capture: true, passive: true });
  window.addEventListener('resize', anchorQueue);
  ['transitionrun', 'animationstart'].forEach(function (type) {
    window.addEventListener(type, function () { anchorRunning += 1; anchorBusyUntil = Date.now() + 3000; anchorQueue(); }, true);
  });
  ['transitionend', 'transitioncancel', 'animationend', 'animationcancel'].forEach(function (type) {
    window.addEventListener(type, function () { anchorRunning = Math.max(0, anchorRunning - 1); anchorQueue(); }, true);
  });
  function setAnchors(raw) {
    if (!Array.isArray(raw) || raw.length > ANCHORS_MAX) return;
    var items = [];
    for (var i = 0; i < raw.length; i += 1) {
      var item = raw[i] || {};
      if (typeof item.key !== 'string' || !/^[\w.:~-]{1,64}$/.test(item.key) || typeof item.selector !== 'string' || item.selector.length > 512) return;
      if (item.pick !== null && !(typeof item.pick === 'number' && item.pick % 1 === 0 && item.pick >= 1)) return;
      items.push({ key: item.key, pick: item.pick === undefined ? null : item.pick, selector: item.selector });
    }
    var had = anchorItems.length > 0;
    anchorItems = items;
    anchorFound = {};
    anchorLast = '';
    if (anchorSizes) anchorSizes.disconnect();
    if (anchorChanges) anchorChanges.disconnect();
    clearInterval(anchorSlow);
    anchorSlow = 0;
    if (!items.length) {
      if (had && parentWin) { try { parentWin.postMessage({ type: 'tk:anchored', vw: 0, vh: 0, items: [] }, hostOrigin); } catch (e) {} }
      return;
    }
    if (anchorSizes) anchorSizes.observe(document.documentElement);
    if (anchorChanges) anchorChanges.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
    // What moves without an event or an observer seeing it is caught by a slow look.
    anchorSlow = setInterval(anchorQueue, 500);
    anchorQueue();
  }
  window.addEventListener('message', function (event) {
    var data = event.data;
    if (!parentWin || event.source !== parentWin || event.origin !== hostOrigin || !data || typeof data !== 'object') return;
    if (data.type === 'tk:anchors' && !slotBridge) setAnchors(data.items);
  });
  // Picking a block for a note, as the slot's bridge does it (lib/hl-preview/bridge-core.ts), for a board shown without
  // that bridge: `tk:pick {on}` arms one pick, the block under the pointer is outlined, a click answers `tk:picked` with
  // the block (its number, selector, words, box and look) and ends the pick (`tk:pick {on: false}`), as Esc does; while
  // picking, presses and clicks are the pick's, so no link or control of the board acts. `tk:styles` tries the notes'
  // looks on the picked blocks by their numbers and answers `tk:styled` with the numbers it no longer has.
  var PICKED_MAX = 64;
  var picking = false;
  var pickBox = null;
  var pickLabel = null;
  var pickSeq = 0;
  var pickedBlocks = new Map();
  var lookKept = new Map();
  var LOOK_PROPS = { background: ['background-color', 'background-image'], radius: ['border-radius'], opacity: ['opacity'] };
  function pickHide() {
    if (pickBox) { pickBox.remove(); pickBox = null; pickLabel = null; }
  }
  function setPicking(on, report) {
    if (picking === on) return;
    picking = on;
    if (!on) pickHide();
    if (report && !on) tellHostDom({ type: 'tk:pick', on: false });
  }
  function pickHighlight(el) {
    if (!pickBox) {
      pickBox = document.createElement('div');
      pickBox.style.cssText = 'position:absolute;pointer-events:none;z-index:2147483647;box-sizing:border-box;border:2px dashed #8b5cf6;background:rgba(139,92,246,.12);border-radius:2px';
      pickLabel = document.createElement('div');
      pickLabel.style.cssText = 'position:absolute;left:-2px;bottom:100%;margin-bottom:2px;padding:1px 6px;font:12px/18px system-ui,sans-serif;color:#fff;background:#8b5cf6;border-radius:3px;white-space:nowrap';
      pickBox.appendChild(pickLabel);
      document.documentElement.appendChild(pickBox);
    }
    var rect = el.getBoundingClientRect();
    pickBox.style.left = rect.left + scrollX + 'px';
    pickBox.style.top = rect.top + scrollY + 'px';
    pickBox.style.width = rect.width + 'px';
    pickBox.style.height = rect.height + 'px';
    pickLabel.textContent = String(el.tagName).toLowerCase();
  }
  function pickable(target) {
    return !!target && target.nodeType === 1 && typeof target.getBoundingClientRect === 'function' && target !== pickBox && target !== pickLabel;
  }
  // A web component keeps its content in a shadow root: the first node of the composed path is the one under the pointer.
  function pickTarget(event) {
    var first = typeof event.composedPath === 'function' ? event.composedPath()[0] : null;
    return first && first.nodeType === 1 ? first : event.target;
  }
  /** `#id` when unique; otherwise up to four levels of `tag.class.class:nth-of-type(n)`, skipping long or hashed classes. */
  function pickSelector(el) {
    function escape(value) { return value.replace(/[^\w-]/g, function (c) { return '\\' + c; }); }
    function uniqueId(node) {
      var nodeId = node.id;
      if (typeof nodeId !== 'string' || !nodeId) return null;
      return document.querySelectorAll('[id="' + nodeId.replace(/["\\]/g, '\\$&') + '"]').length === 1 ? '#' + escape(nodeId) : null;
    }
    function hashed(name) {
      return name.length > 20 || /^\d/.test(name) || name.split(/[-_]/).some(function (part) { return part.length >= 5 && /\d/.test(part) && /[a-z]/i.test(part); });
    }
    function step(node) {
      var tag = String(node.tagName).toLowerCase();
      var classes = Array.prototype.filter.call(node.classList || [], function (name) { return !hashed(name); }).slice(0, 2);
      var parent = node.parentElement;
      var same = parent ? Array.prototype.filter.call(parent.children, function (sibling) { return sibling.tagName === node.tagName; }) : [];
      var nth = same.length > 1 ? ':nth-of-type(' + (same.indexOf(node) + 1) + ')' : '';
      return tag + classes.map(function (name) { return '.' + escape(name); }).join('') + nth;
    }
    var own = uniqueId(el);
    if (own) return own;
    var steps = [];
    for (var node = el; node && steps.length < 4; node = node.parentElement) {
      var tag = String(node.tagName).toLowerCase();
      if (tag === 'body' || tag === 'html') break;
      var nodeId = steps.length ? uniqueId(node) : null;
      if (nodeId) { steps.unshift(nodeId); break; }
      steps.unshift(step(node));
    }
    return steps.join(' > ');
  }
  function colorHex(value) {
    var context = document.createElement('canvas').getContext('2d');
    if (!context) return null;
    context.fillStyle = '#000000';
    context.fillStyle = value;
    var read = String(context.fillStyle);
    if (/^#[0-9a-f]{6}$/i.test(read)) return read.toLowerCase();
    var rgba = /^rgba\((\d+), (\d+), (\d+), ([\d.]+)\)$/.exec(read);
    if (!rgba || Number(rgba[4]) === 0) return null;
    return '#' + [rgba[1], rgba[2], rgba[3]].map(function (part) { return ('0' + Number(part).toString(16)).slice(-2); }).join('');
  }
  function readLook(el) {
    try {
      var style = getComputedStyle(el);
      var radius = /^(\d+(?:\.\d+)?)px$/.exec(String(style.borderTopLeftRadius));
      var opacity = parseFloat(String(style.opacity));
      return { background: colorHex(String(style.backgroundColor)), radius: radius ? Number(radius[1]) : null, opacity: isFinite(opacity) ? opacity : 1 };
    } catch (e) { return null; }
  }
  /** Puts a look on the block (`!important`), keeping what the block had inline the first time a property is touched. */
  function applyLook(el, item, kept) {
    var values = {
      background: item.background === null ? null : [item.background, 'none'],
      radius: item.radius === null ? null : [item.radius + 'px'],
      opacity: item.opacity === null ? null : [String(item.opacity)]
    };
    ['background', 'radius', 'opacity'].forEach(function (group) {
      var wanted = values[group];
      LOOK_PROPS[group].forEach(function (property, index) {
        if (wanted === null) {
          if (!(property in kept)) return;
          var before = kept[property];
          if (before.value === '') el.style.removeProperty(property); else el.style.setProperty(property, before.value, before.priority);
          delete kept[property];
          return;
        }
        if (!(property in kept)) kept[property] = { value: el.style.getPropertyValue(property), priority: el.style.getPropertyPriority(property) };
        el.style.setProperty(property, wanted[index], 'important');
      });
    });
  }
  function restoreLook(el, kept) { applyLook(el, { background: null, radius: null, opacity: null }, kept); }
  function rememberPicked(el) {
    pickSeq += 1;
    pickedBlocks.set(pickSeq, el);
    if (pickedBlocks.size > PICKED_MAX) {
      var oldest = pickedBlocks.keys().next().value;
      var gone = pickedBlocks.get(oldest);
      if (lookKept.has(gone)) restoreLook(gone, lookKept.get(gone));
      lookKept.delete(gone);
      pickedBlocks.delete(oldest);
    }
    return pickSeq;
  }
  /** The set of looks as the host sent it: one item that is not right drops the whole set. */
  function readStyles(raw) {
    if (!Array.isArray(raw) || raw.length > PICKED_MAX) return null;
    for (var i = 0; i < raw.length; i += 1) {
      var item = raw[i];
      if (!item || typeof item !== 'object' || typeof item.id !== 'number' || item.id % 1 !== 0 || item.id < 1) return null;
      if (item.background !== null && !(typeof item.background === 'string' && /^#[0-9a-f]{6}$/i.test(item.background))) return null;
      if (item.radius !== null && !(typeof item.radius === 'number' && item.radius >= 0 && item.radius <= 1000)) return null;
      if (item.opacity !== null && !(typeof item.opacity === 'number' && item.opacity >= 0 && item.opacity <= 1)) return null;
    }
    return raw;
  }
  function setStyles(items) {
    var wanted = new Set();
    var missing = [];
    items.forEach(function (item) {
      var el = pickedBlocks.get(item.id);
      if (!el || !el.isConnected) { missing.push(item.id); return; }
      wanted.add(el);
      if (!lookKept.has(el)) lookKept.set(el, {});
      applyLook(el, item, lookKept.get(el));
    });
    Array.from(lookKept.keys()).forEach(function (el) {
      if (wanted.has(el)) return;
      restoreLook(el, lookKept.get(el));
      lookKept.delete(el);
    });
    tellHostDom({ type: 'tk:styled', gone: missing });
  }
  if (parentWin && !slotBridge) {
    var swallowPick = function (event) {
      if (!picking) return;
      event.preventDefault();
      event.stopPropagation();
      if (event.stopImmediatePropagation) event.stopImmediatePropagation();
    };
    ['pointerdown', 'pointerup', 'mousedown', 'mouseup', 'dblclick', 'auxclick', 'contextmenu'].forEach(function (type) {
      window.addEventListener(type, swallowPick, true);
    });
    window.addEventListener('pointermove', function (event) {
      if (!picking) return;
      var target = pickTarget(event);
      if (pickable(target)) pickHighlight(target);
    }, true);
    window.addEventListener('click', function (event) {
      if (!picking) return;
      swallowPick(event);
      var el = pickTarget(event);
      if (!pickable(el)) return;
      var box = el.getBoundingClientRect();
      tellHostDom({
        type: 'tk:picked', pick: rememberPicked(el), look: readLook(el), selector: pickSelector(el), tag: String(el.tagName).toLowerCase(),
        text: String(el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 120),
        rect: { x: Math.round(box.left), y: Math.round(box.top), width: Math.round(box.width), height: Math.round(box.height) },
        outerHTML: String(el.outerHTML || '').slice(0, 2048), component: null, url: location.href, viewport: { w: innerWidth, h: innerHeight }
      });
      setPicking(false, true);
    }, true);
    window.addEventListener('keydown', function (event) {
      if (!picking || event.key !== 'Escape') return;
      swallowPick(event);
      setPicking(false, true);
    }, true);
    // Another board or address ends the pick, as the bridge's navigation does.
    window.addEventListener('popstate', function () { setPicking(false, true); });
    window.addEventListener('message', function (event) {
      var data = event.data;
      if (event.source !== parentWin || event.origin !== hostOrigin || !data || typeof data !== 'object') return;
      if (data.type === 'tk:pick' && typeof data.on === 'boolean') setPicking(data.on, false);
      if (data.type === 'tk:styles') {
        var items = readStyles(data.items);
        if (items) setStyles(items);
      }
    });
  }
  function showTweaks(open) {
    if (hosted) { try { parentWin.postMessage({ type: 'tk:tweaks-panel', open: open }, hostOrigin); } catch (e) {} return; }
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
      var panel = make('div', { class: 'panel finder', role: 'dialog', 'aria-label': 'Доски проекта', hidden: '' });
      var searchable = boards.length > 6;
      var search = null;
      var rows = make('div', { class: 'list' });
      if (searchable) {
        var box = make('div', { class: 'search' });
        box.innerHTML = ICONS.search;
        search = make('input', { type: 'search', placeholder: 'Найти доску', 'aria-label': 'Найти доску', autocomplete: 'off', spellcheck: 'false' });
        box.appendChild(search);
        panel.appendChild(box);
      }
      panel.appendChild(rows);
      var shown = false;
      var timer = 0;
      var items = function () { return Array.prototype.slice.call(rows.querySelectorAll('.item')); };
      var render = function () {
        rows.textContent = '';
        var groups = boardGroups(search ? search.value.trim().toLowerCase() : '');
        groups.forEach(function (group) {
          rows.appendChild(make('div', { class: 'group' }, group.name));
          group.screens.forEach(function (screen) {
            var both = !!(screen.desktop && screen.phone);
            [['desktop', 'Десктоп'], ['phone', 'Телефон']].forEach(function (pair) {
              var item = screen[pair[0]];
              if (!item) return;
              var row = make('button', { type: 'button', class: 'item', title: item.title, 'aria-current': String(item === board) });
              var thumb = make('span', { class: 'thumb' });
              thumb.innerHTML = ICONS[pair[0]];
              row.appendChild(thumb);
              row.appendChild(make('span', { class: 'name' }, screen.name));
              if (both || pair[0] === 'phone') row.appendChild(make('span', { class: 'dev' }, '· ' + pair[1]));
              if (item === board) {
                var tick = make('span', { class: 'tick' });
                tick.innerHTML = ICONS.check;
                row.appendChild(tick);
              }
              row.addEventListener('click', function () { closeAll(); go(item.href); });
              rows.appendChild(row);
            });
          });
        });
        if (!groups.length) rows.appendChild(make('div', { class: 'empty' }, 'Такой доски нет'));
      };
      var entry = {
        close: function () {
          if (!shown) return;
          shown = false;
          panel.removeAttribute('data-open');
          trigger.setAttribute('aria-expanded', 'false');
          clearTimeout(timer);
          timer = setTimeout(function () { panel.hidden = true; }, 150);
        },
        open: function () {
          closeAll();
          clearTimeout(timer);
          shown = true;
          panel.hidden = false;
          trigger.setAttribute('aria-expanded', 'true');
          if (search) search.value = '';
          render();
          void panel.offsetWidth;
          panel.setAttribute('data-open', '');
          var mark = rows.querySelector('.item[aria-current="true"]');
          if (mark) mark.scrollIntoView({ block: 'center' });
          (search || mark || items()[0] || trigger).focus({ preventScroll: true });
        }
      };
      trigger.setAttribute('aria-haspopup', 'dialog');
      trigger.setAttribute('aria-expanded', 'false');
      trigger.addEventListener('click', function () { if (shown) entry.close(); else entry.open(); });
      var focusAt = function (list, at) {
        var target = list[Math.max(0, Math.min(list.length - 1, at))];
        if (!target) return;
        if (target === list[0]) rows.scrollTop = 0;
        target.focus();
      };
      panel.addEventListener('keydown', function (event) {
        if (event.key === 'Escape') { event.stopPropagation(); entry.close(); trigger.focus(); return; }
        var from = event.composedPath()[0];
        var inSearch = !!search && from === search;
        var list = items();
        var at = list.indexOf(from);
        if (event.key === 'Enter' && inSearch) { event.preventDefault(); if (list[0]) list[0].click(); return; }
        if (event.key === 'ArrowDown') { event.preventDefault(); focusAt(list, inSearch ? 0 : at + 1); return; }
        if (event.key === 'ArrowUp') {
          event.preventDefault();
          if (inSearch) focusAt(list, list.length - 1);
          else if (at <= 0 && search) { rows.scrollTop = 0; search.focus(); }
          else focusAt(list, at - 1);
          return;
        }
        if (inSearch) return;
        if (event.key === 'Home') { event.preventDefault(); focusAt(list, 0); return; }
        if (event.key === 'End') { event.preventDefault(); focusAt(list, list.length - 1); return; }
        if (search && event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) search.focus();
      });
      if (search) search.addEventListener('input', render);
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
