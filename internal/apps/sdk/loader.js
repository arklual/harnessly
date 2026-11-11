// The loader for a site that exists already (an admin panel shown in a Harnessly frame). The site's shell
// carries one inline line (loader-inline.js) that decides, before any framework starts, whether the page is inside
// Harnessly: it is in a frame and its address's fragment holds the host's nonce (#hl-nonce=…, what the host gives a
// view on the app's own server). Outside, nothing is loaded and window.harnessly is a stand-in whose state(), action()
// and screen() do nothing, so the site's code never asks where it runs. Inside, the line loads this file from the
// folder of the SDK's major version, and this file loads the shim and the SDK from the same folder, replays what the
// site called meanwhile and connects.
//
// The fragment is not a proof: any page that frames the site can write it. So the line also names the origins of the
// Harnessly pages the site trusts, and only a handshake answered from one of them reaches the shim. A parent of another
// origin gets no port: it reads no state and calls no tool, whatever the site's frame-ancestors says.
//
// It also works as the only script (<script src=".../loader.js" data-hosts="https://harnessly.example">): then it
// makes the stand-in itself.
(function () {
  'use strict';
  var script = document.currentScript;
  var base = script.src.replace(/\/[^/]*$/, '');
  var KINDS = ['state', 'action', 'screen'];

  // The same stand-in the inline line makes: a call is kept in q (inside Harnessly) and its handle works once replayed.
  var standIn = function () {
    var found = window.parent !== window && /(?:^#|&)hl-nonce=([A-Za-z0-9_-]+)/.exec(location.hash);
    var q = found ? [] : null;
    var keep = function (kind) {
      return function () {
        var call = { k: kind, a: arguments };
        if (q) q.push(call);
        return {
          unregister: function () {
            call.off = 1;
            if (call.h) call.h.unregister();
          },
          refresh: function () {
            if (call.h) call.h.refresh();
          },
          clear: function () {
            call.off = 1;
            if (call.h) call.h.clear();
          },
        };
      };
    };
    var hosts = (script.getAttribute('data-hosts') || '').split(/\s+/).filter(Boolean);
    return { q: q, hosts: hosts, nonce: found && found[1], state: keep('state'), action: keep('action'), screen: keep('screen') };
  };

  var stub = window.harnessly;
  if (!stub || !('q' in stub)) {
    stub = standIn();
    window.harnessly = stub;
  }
  // Outside Harnessly: the stand-in stays, nothing else is loaded.
  if (!stub.q) return;

  // Only the Harnessly pages the site named are a host. This listener is on the window before the shim's, so an answer
  // of any other parent stops here and the shim never takes its port.
  var hosts = stub.hosts || [];
  if (hosts.length === 0) {
    stub.q = null;
    console.warn('harnessly: the loader line names no Harnessly origin, so nothing is loaded');
    return;
  }
  var framedBy = window.parent;
  window.addEventListener(
    'message',
    function (e) {
      if (e.source === framedBy && hosts.indexOf(e.origin) === -1) e.stopImmediatePropagation();
    },
    true,
  );

  // The shim reads the nonce from <meta name="hl-nonce">; the site's router may have rewritten the address by now, so
  // the nonce the stand-in took at the start is what it gets. It leaves the address, as the shim would take it out.
  var meta = document.createElement('meta');
  meta.name = 'hl-nonce';
  meta.content = stub.nonce;
  document.head.appendChild(meta);
  var hash = location.hash.replace(/^#/, '');
  var rest = hash.replace(/(?:^|&)hl-nonce=[A-Za-z0-9_-]+/, '').replace(/^&/, '');
  if (rest !== hash) {
    try {
      history.replaceState(history.state, '', location.pathname + location.search + (rest ? '#' + rest : ''));
    } catch (e) {
      // an address the page cannot rewrite: the nonce stays in it
    }
  }

  var load = function (name, then) {
    var s = document.createElement('script');
    s.src = base + '/' + name;
    s.onload = then;
    s.onerror = function () {
      console.warn('harnessly: ' + s.src + ' did not load');
    };
    document.head.appendChild(s);
  };

  // What the site called before the SDK was here, in its order; a handle the site has dropped already is dropped.
  var replay = function (sdk) {
    var calls = stub.q;
    stub.q = null;
    for (var i = 0; i < calls.length; i += 1) {
      var call = calls[i];
      if (call.off) continue;
      try {
        call.h = sdk[call.k].apply(sdk, call.a);
      } catch (e) {
        console.warn('harnessly: ' + call.k + ': ' + (e && e.message ? e.message : e));
      }
    }
  };

  load('bridge-shim.js', function () {
    load('app-sdk.classic.js', function () {
      // The classic SDK has put itself at window.harnessly; a reference to the stand-in the site kept calls it too.
      var sdk = window.harnessly;
      replay(sdk);
      for (var i = 0; i < KINDS.length; i += 1) stub[KINDS[i]] = sdk[KINDS[i]];
      sdk.connect().catch(function (e) {
        console.warn('harnessly: ' + (e && e.message ? e.message : e));
      });
    });
  });
})();
