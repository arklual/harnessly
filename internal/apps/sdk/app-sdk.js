// @harnessly/app-sdk: what a view of a Harnessly app uses to talk to the host. One file, no dependencies, served by the
// runtime at /__hl/app-sdk.js to every view. The wire is MCP Apps (JSON-RPC 2.0 over postMessage: ui/initialize,
// tools/call, ui/message, notifications) plus the host's own methods under harnessly/.
//
//   import { connect } from '/__hl/app-sdk.js'
//   const hl = await connect()
//
// The view server puts /__hl/bridge-shim.js first in every page it serves. The shim makes the handshake with the nonce of
// the page and takes the host's MessageChannel port; it replaces window.parent with a stand-in that speaks on the port,
// so this file (like any standard MCP Apps view) just posts to window.parent and listens to `message`.

const PROTOCOL = '2025-11-21';
const TIMEOUT_MS = 30_000;

/** The error every failed call throws; `code` says why (see HarnesslyErrorCode in app-sdk.d.ts). */
export class HarnesslyError extends Error {
  constructor(code, message) {
    super(message || code);
    this.name = 'HarnesslyError';
    this.code = code;
  }
}

// The host answers with a JSON-RPC error whose data.code is ours; an error without one is the host being unavailable.
const errorOf = (e) => new HarnesslyError(e?.data?.code || 'host_unavailable', e?.message);

// The host speaks MCP Apps' host context (theme as the mode, styles.variables) and carries its own whole context under
// `harnessly`: that is what `hl.context` is.
const ctxOf = (host) => host?.harnessly ?? host ?? {};

const STORAGE_SCOPES = ['install', 'user', 'chat', 'space', 'doc'];

/**
 * Handshake with the host. Throws HarnesslyError('host_unavailable') when the view is not inside a Harnessly frame or
 * the host does not answer.
 */
export async function connect(options = {}) {
  const parent = options.parent ?? window.parent;
  if (!parent || parent === window) throw new HarnesslyError('host_unavailable', 'the view is open outside Harnessly');

  const pending = new Map();
  const listeners = new Map();
  let hostOrigin = null;
  let seq = 0;

  const send = (message) => parent.postMessage({ jsonrpc: '2.0', ...message }, hostOrigin ?? '*');

  const emit = (event, payload) => {
    for (const fn of listeners.get(event) ?? []) {
      try {
        fn(payload);
      } catch (e) {
        console.error(e);
      }
    }
  };

  const request = (method, params, { timeout = TIMEOUT_MS } = {}) =>
    new Promise((resolve, reject) => {
      const id = ++seq;
      const timer =
        timeout > 0 ? setTimeout(() => (pending.delete(id), reject(new HarnesslyError('host_unavailable', `${method}: no answer`))), timeout) : 0;
      pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      });
      send({ id, method, params });
    });
  const notify = (method, params) => send({ method, params });

  const hl = { context: null };

  const applyTheme = (ctx) => {
    const root = document.documentElement;
    if (ctx.theme?.mode) {
      root.dataset.theme = ctx.theme.mode;
      root.style.colorScheme = ctx.theme.mode;
    }
    for (const [name, value] of Object.entries(ctx.theme?.tokens ?? {})) root.style.setProperty(name, value);
    if (ctx.locale) root.lang = ctx.locale;
  };

  // Host → view: only what the host window sent, and from the origin it answered the handshake with.
  const onMessage = (event) => {
    if (event.source !== parent) return;
    if (hostOrigin !== null && event.origin !== hostOrigin) return;
    const m = event.data;
    if (!m || m.jsonrpc !== '2.0') return;
    if (m.method === undefined) {
      // The first answer fixes the host's origin; every later message must come from it.
      hostOrigin ??= event.origin;
      const wait = pending.get(m.id);
      if (!wait) return;
      pending.delete(m.id);
      if (m.error) wait.reject(errorOf(m.error));
      else wait.resolve(m.result);
      return;
    }
    switch (m.method) {
      case 'ui/notifications/host-context-changed': {
        const before = hl.context;
        const next = ctxOf(m.params);
        hl.context = { ...before, ...next };
        applyTheme(hl.context);
        emit('context', hl.context);
        if (next.theme && JSON.stringify(next.theme) !== JSON.stringify(before?.theme)) emit('theme', hl.context.theme);
        break;
      }
      case 'ui/notifications/tool-result':
        emit('tool-result', m.params);
        break;
      case 'ui/notifications/tool-input':
        emit('tool-input', m.params);
        break;
      case 'harnessly/notifications/flow':
      case 'harnessly/notifications/data':
      case 'harnessly/notifications/selection':
      case 'harnessly/notifications/visibility':
        emit(m.method.slice('harnessly/notifications/'.length), m.params);
        break;
      default:
        // A request the view does not know gets the standard answer, so the host does not wait.
        if (m.id !== undefined) send({ id: m.id, error: { code: -32601, message: 'method not found' } });
    }
  };
  window.addEventListener('message', onMessage);

  const init = await request('ui/initialize', {
    protocolVersion: PROTOCOL,
    appInfo: { name: options.name ?? document.title ?? 'view', version: options.version ?? '0' },
    appCapabilities: {},
  }).catch((e) => {
    window.removeEventListener('message', onMessage);
    throw e;
  });
  hl.context = ctxOf(init.hostContext);
  hl.host = init.hostInfo;
  applyTheme(hl.context);
  notify('ui/notifications/initialized', {});

  hl.on = (event, fn) => {
    if (!listeners.has(event)) listeners.set(event, new Set());
    listeners.get(event).add(fn);
    return () => listeners.get(event)?.delete(fn);
  };

  hl.chat = {
    // The chat shows the app's own name before the text; a view cannot choose another.
    send: (text) => request('ui/message', { role: 'user', content: [{ type: 'text', text }] }),
    read: ({ last = 20 } = {}) => request('harnessly/chat.read', { last }).then((r) => r.messages),
    card: (view, data) => request('harnessly/chat.card', { view, data }),
  };
  hl.pane = { open: (target) => request('harnessly/pane.open', { target }) };
  hl.tools = { call: (name, args = {}) => request('tools/call', { name, arguments: args }) };
  hl.flow = {
    status: () => request('harnessly/flow.status', {}),
    go: (state) => request('harnessly/flow.go', { state }),
  };
  hl.data = {
    get: () => request('harnessly/data.get', {}).then((r) => r.data),
    set: (patch) => request('harnessly/data.set', { patch }).then((r) => r.data),
  };
  hl.storage = {};
  for (const scope of STORAGE_SCOPES) {
    hl.storage[scope] = {
      get: (key) => request('harnessly/storage.get', { scope, key }).then((r) => r.value),
      set: (key, value) => request('harnessly/storage.set', { scope, key, value }).then(() => undefined),
      delete: (key) => request('harnessly/storage.delete', { scope, key }).then(() => undefined),
      keys: () => request('harnessly/storage.keys', { scope }).then((r) => r.keys),
    };
  }
  // Files of the chat's workspace, inside the masks of files.read:<mask> and files.write:<mask>. A path is relative to
  // the workspace; a text moves, up to 4 MB.
  hl.files = {
    read: (path) => request('harnessly/files.read', { path }).then((r) => r.text),
    write: (path, text) => request('harnessly/files.write', { path, text }).then(() => undefined),
  };
  hl.ui = {
    toast: (text) => request('harnessly/ui.toast', { text }).then(() => undefined),
    // A person decides in their own time.
    confirm: (text) => request('harnessly/ui.confirm', { text }, { timeout: 0 }).then((r) => r.ok),
  };
  // The Document API of Sinopia, for a view shown beside a document (target sinopia.<kind>): the host and the runtime
  // check the manifest's rights (sinopia.read/write/comment/export) on every call.
  const sinopia = (method, params = {}) => request(`harnessly/sinopia.${method}`, params).then((r) => r.result);
  hl.sinopia = {
    document: () => sinopia('document'),
    selection: () => sinopia('selection'),
    node: (id) => sinopia('node', { id }),
    query: (selector, options = {}) => sinopia('query', { selector, ...options }),
    patch: (ops) => sinopia('patch', { ops }),
    fields: () => sinopia('fields'),
    setFields: (patch) => sinopia('setFields', { patch }),
    mark: (nodes, options = {}) => sinopia('mark', { nodes, ...options }),
    comment: (node, text) => sinopia('comment', { node, text }),
    render: (options = {}) => sinopia('render', options),
    boards: { add: (options) => sinopia('boards.add', options) },
  };
  hl.notify = (text) => request('harnessly/notify', { text }).then(() => undefined);
  /** Tells the host the content's height (a card in the feed is as tall as its view asks). */
  hl.resize = (height) => notify('ui/notifications/size-changed', { height: Math.ceil(height) });
  hl.close = () => window.removeEventListener('message', onMessage);

  if (hl.context?.target === 'chat' && typeof ResizeObserver === 'function') {
    new ResizeObserver(() => hl.resize(document.documentElement.scrollHeight)).observe(document.documentElement);
  }
  return hl;
}
