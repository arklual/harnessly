// The host of a standalone app opened by link (/apps/guest/<token>/). A guest has no account, no chat and no agent: the
// page is the app's view in a sandboxed frame and this script, which does the little of the bridge a guest is given:
// the handshake, the link's own storage (read-only unless the owner made the link writable), links out, the theme.
// Everything else a view may ask of a host (chat, tools, panes, flows, documents) is refused with permission_denied.
//
// Written like the page's bridge (web/app/next/features/app-views/bridge.ts) so a view cannot tell the two apart: the
// view's shim sends ui/initialize with the nonce the page carries, the answer hands over one end of a MessageChannel,
// and everything after goes over that port.
(function () {
  'use strict';
  var meta = document.querySelector('meta[name="hl-guest"]');
  var cfg = {};
  try {
    cfg = JSON.parse((meta && meta.getAttribute('content')) || '{}');
  } catch (e) {
    cfg = {};
  }
  var frame = document.getElementById('view');
  if (!frame || !cfg.nonce) return;

  var PROTOCOL = '2025-11-21';
  var STORAGE = {
    'harnessly/storage.get': 'get',
    'harnessly/storage.set': 'set',
    'harnessly/storage.delete': 'delete',
    'harnessly/storage.keys': 'keys',
  };
  var TOKENS = {
    light: { '--t-bg': '#ffffff', '--t-surface': '#f6f6f7', '--t-surface-hover': '#ececee', '--t-text': '#17171a', '--t-text-secondary': '#4a4a52', '--t-text-muted': '#76767f', '--t-border': '#e2e2e6', '--t-border-strong': '#c9c9cf', '--t-accent': '#3b5bdb', '--t-danger': '#d6336c', '--t-success': '#2f9e44' },
    dark: { '--t-bg': '#141416', '--t-surface': '#1c1c1f', '--t-surface-hover': '#26262a', '--t-text': '#ececf0', '--t-text-secondary': '#b4b4bc', '--t-text-muted': '#8a8a94', '--t-border': '#2e2e33', '--t-border-strong': '#44444b', '--t-accent': '#748ffc', '--t-danger': '#f06595', '--t-success': '#69db7c' },
  };

  var mode = function () {
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  };
  var context = function () {
    var m = mode();
    return {
      theme: m,
      styles: { variables: TOKENS[m] },
      locale: cfg.locale || 'en',
      displayMode: 'inline',
      platform: 'web',
      harnessly: {
        app: { id: cfg.app, version: cfg.version },
        target: 'app',
        view: cfg.view,
        theme: { mode: m, tokens: TOKENS[m] },
        locale: cfg.locale || 'en',
        chat: null,
        user: null,
        guest: true,
      },
    };
  };

  var port = null;

  var refuse = function (id, code, message) {
    port.postMessage({ jsonrpc: '2.0', id: id, error: { code: code === 'invalid_params' ? -32602 : -32000, message: message, data: { code: code } } });
  };

  // The storage calls go to the link's own route; the runtime checks the link, the app's right and the write flag again.
  var storage = function (id, op, params) {
    if (typeof params.key !== 'string' && op !== 'keys') return refuse(id, 'invalid_params', 'key is a text');
    fetch('__guest/storage', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ op: op, key: params.key || '', value: params.value }),
    })
      .then(function (res) {
        return res.json().then(function (body) {
          return { ok: res.ok, body: body };
        });
      })
      .then(function (r) {
        if (!r.ok) return refuse(id, (r.body && r.body.error) || 'host_unavailable', (r.body && r.body.message) || 'refused');
        if (op === 'get') return port.postMessage({ jsonrpc: '2.0', id: id, result: { value: r.body.value === undefined ? null : r.body.value } });
        if (op === 'keys') return port.postMessage({ jsonrpc: '2.0', id: id, result: { keys: r.body.keys || [] } });
        port.postMessage({ jsonrpc: '2.0', id: id, result: {} });
      })
      .catch(function () {
        refuse(id, 'host_unavailable', 'the machine did not answer');
      });
  };

  var onPort = function (event) {
    var m = event.data;
    if (!m || m.jsonrpc !== '2.0' || typeof m.method !== 'string') return;
    var id = m.id;
    var params = m.params && typeof m.params === 'object' ? m.params : {};
    var reply = function (result) {
      if (id !== undefined) port.postMessage({ jsonrpc: '2.0', id: id, result: result });
    };
    var deny = function (code, message) {
      if (id !== undefined) refuse(id, code, message);
    };
    if (m.method === 'ui/notifications/initialized' || m.method === 'ui/notifications/size-changed' || m.method === 'harnessly/ui.toast') {
      return reply({});
    }
    if (m.method === 'ui/open-link') {
      if (typeof params.url !== 'string' || !/^https:\/\//i.test(params.url)) return deny('invalid_params', 'url is an https address');
      window.open(params.url, '_blank', 'noopener,noreferrer');
      return reply({});
    }
    if (Object.prototype.hasOwnProperty.call(STORAGE, m.method)) {
      if (id === undefined) return;
      return storage(id, STORAGE[m.method], params);
    }
    deny('permission_denied', 'a guest of a shared app has no chat, no agent and no tools');
  };

  window.addEventListener('message', function (event) {
    // Only the frame this page made, and only until it holds a port: after that the frame's window is not heard.
    if (port !== null || event.source !== frame.contentWindow || event.origin !== 'null') return;
    var m = event.data;
    if (!m || m.jsonrpc !== '2.0' || m.method !== 'ui/initialize' || m.id === undefined) return;
    if (!m.params || m.params.nonce !== cfg.nonce) return;
    var channel = new MessageChannel();
    port = channel.port1;
    port.onmessage = onPort;
    port.start();
    frame.contentWindow.postMessage(
      {
        jsonrpc: '2.0',
        id: m.id,
        result: {
          protocolVersion: PROTOCOL,
          hostInfo: { name: 'harnessly', version: 'guest' },
          hostCapabilities: { openLinks: {} },
          hostContext: context(),
        },
      },
      '*',
      [channel.port2],
    );
  });
})();
