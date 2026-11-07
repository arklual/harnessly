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
const errorOf = (e) => {
  const err = new HarnesslyError(e?.data?.code || 'host_unavailable', e?.message);
  // A conflict says the version the key has now.
  if (typeof e?.data?.version === 'number') err.version = e.data.version;
  return err;
};

// The host speaks MCP Apps' host context (theme as the mode, styles.variables) and carries its own whole context under
// `harnessly`: that is what `hl.context` is.
const ctxOf = (host) => host?.harnessly ?? host ?? {};

const STORAGE_SCOPES = ['install', 'user', 'chat', 'space', 'doc'];

/**
 * Handshake with the host. Throws HarnesslyError('host_unavailable') when the view is not inside a Harnessly frame or
 * the host does not answer.
 */
// The view's errors and console.error lines go to the host's debug section (harnessly/debug.log): kept from the moment
// the SDK loads, sent once the view is connected. A few, short: it is a hint for the author, not a log store.
const debugQueue = [];
let debugSend = null;
const debugLog = (level, text) => {
  const line = { level, text: String(text).slice(0, 2000) };
  if (debugSend) debugSend(line);
  else if (debugQueue.length < 20) debugQueue.push(line);
};
const debugText = (v) => {
  if (v instanceof Error) return `${v.name}: ${v.message}`;
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
};
if (typeof window !== 'undefined') {
  window.addEventListener('error', (e) =>
    debugLog('error', `${e.message || 'error'}${e.filename ? ` (${e.filename}:${e.lineno})` : ''}`),
  );
  window.addEventListener('unhandledrejection', (e) => debugLog('error', `unhandled: ${debugText(e.reason)}`));
  window.addEventListener('securitypolicyviolation', (e) =>
    debugLog('error', `CSP blocked ${e.blockedURI || 'inline'} by ${e.violatedDirective}`),
  );
  for (const level of ['error', 'warn']) {
    const own = console[level];
    console[level] = (...args) => {
      debugLog(level, args.map(debugText).join(' '));
      own.apply(console, args);
    };
  }
}

export async function connect(options = {}) {
  const parent = options.parent ?? window.parent;
  if (!parent || parent === window) throw new HarnesslyError('host_unavailable', 'the view is open outside Harnessly');

  const pending = new Map();
  const listeners = new Map();
  let hostOrigin = null;
  // The view's page tools (hl.tools.register) by name, and how a call of the agent runs one: the answer, or the error in
  // words, goes back to the host.
  let pageTools = new Map();
  const runPageTool = async (p) => {
    const id = p?.id;
    if (typeof id !== 'string') return;
    let body;
    try {
      const tool = pageTools.get(p.name);
      if (!tool) throw new Error(`the view offers no tool ${p.name}`);
      const result = await tool.run(p.arguments ?? {});
      body = { id, result: result === undefined ? null : result };
    } catch (e) {
      body = { id, error: e instanceof Error ? e.message : String(e) };
    }
    request('harnessly/tools.result', body).catch(() => {});
  };
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
      case 'harnessly/notifications/data':
      case 'harnessly/notifications/selection':
      case 'harnessly/notifications/storage':
      case 'harnessly/notifications/visibility':
      case 'harnessly/notifications/agents':
        emit(m.method.slice('harnessly/notifications/'.length), m.params);
        break;
      case 'harnessly/notifications/tool-call':
        void runPageTool(m.params);
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
  debugSend = (line) => notify('harnessly/debug.log', line);
  for (const line of debugQueue.splice(0)) debugSend(line);

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
  hl.agents = { list: () => request('harnessly/agents.list', {}).then((r) => r.agents) };
  hl.pane = { open: (target) => request('harnessly/pane.open', { target }) };
  hl.tools = { call: (name, args = {}) => request('tools/call', { name, arguments: args }) };
  // Tools of the page (right tools.page): the agent of the chat lists them with page_tools and calls one with page_call;
  // the view runs it here, in the person's page, and the answer goes back. The host is told the whole list each time.
  const announce = () =>
    request('harnessly/tools.register', {
      tools: [...pageTools.values()].map(({ name, description, inputSchema, confirm }) => ({ name, description, inputSchema, confirm })),
    });
  hl.tools.register = (tool, run) => {
    if (!tool || typeof tool.name !== 'string' || typeof run !== 'function')
      return Promise.reject(new HarnesslyError('invalid_params', 'register({name, description, inputSchema}, run)'));
    // A tool that changes something asks the person first: confirm, or WebMCP's destructiveHint.
    const confirm = tool.confirm === true || tool.annotations?.destructiveHint === true;
    pageTools.set(tool.name, { name: tool.name, description: tool.description ?? '', inputSchema: tool.inputSchema, confirm, run });
    const ready = announce();
    const unregister = () => {
      if (pageTools.get(tool.name)?.run !== run) return Promise.resolve();
      pageTools.delete(tool.name);
      return announce();
    };
    return Object.assign(ready.then(() => ({ unregister })), { unregister });
  };
  // What the view shows, for the agent to read with page_tools (right context.publish): the last one stands.
  hl.publish = (data) => request('harnessly/context.publish', { data }).then(() => undefined);
  // The standard WebMCP API: a view that registers its tools with navigator.modelContext offers them to the chat's agent
  // the same way. Where the browser has its own, a tool goes to both.
  const own = navigator.modelContext;
  const unregisters = new Map();
  const modelContext = {
    registerTool: (tool) => {
      const run = (args) => tool.execute(args ?? {}, { requestUserInteraction: (cb) => cb() });
      const handle = hl.tools.register(tool, run);
      unregisters.set(tool.name, handle.unregister);
      let native;
      try {
        native = own?.registerTool?.(tool);
      } catch {
        native = undefined;
      }
      return {
        unregister: () => {
          unregisters.delete(tool.name);
          native?.unregister?.();
          return handle.unregister();
        },
      };
    },
    unregisterTool: (name) => {
      unregisters.get(name)?.();
      unregisters.delete(name);
      try {
        own?.unregisterTool?.(name);
      } catch {
        // the browser's own may not know it
      }
    },
  };
  try {
    Object.defineProperty(navigator, 'modelContext', { value: modelContext, configurable: true });
  } catch {
    // a frozen navigator: hl.tools.register still works
  }
  // hl.flow and hl.data were never connected in the host (every call answered host_unavailable) and are gone. A view
  // that still calls them gets the reason, not a silent hang: use hl.storage (state) and hl.on('data') (a chat card).
  const removed = (name, instead) =>
    new Proxy(
      {},
      {
        get: (_, method) => () =>
          Promise.reject(new HarnesslyError('invalid_params', `hl.${name}.${String(method)} was removed from the SDK: ${instead}`)),
      },
    );
  hl.flow = removed('flow', 'keep the steps in hl.storage');
  hl.data = removed('data', 'keep the state in hl.storage, and read a card\'s data from hl.context.data');
  hl.storage = {};
  for (const scope of STORAGE_SCOPES) {
    hl.storage[scope] = {
      // get answers the value (null if none); getVersioned also the key's version, to hand back as set's ifVersion.
      get: (key) => request('harnessly/storage.get', { scope, key }).then((r) => r.value),
      getVersioned: (key) => request('harnessly/storage.get', { scope, key }).then((r) => ({ value: r.value, version: r.version })),
      // set answers the key's new version; with { ifVersion } it writes only if the key is still at that version,
      // else it rejects with code 'conflict' (error.version is the current one) and nothing is written.
      set: (key, value, options = {}) =>
        request('harnessly/storage.set', { scope, key, value, ...(options.ifVersion === undefined ? {} : { ifVersion: options.ifVersion }) }).then(
          (r) => r.version,
        ),
      delete: (key) => request('harnessly/storage.delete', { scope, key }).then(() => undefined),
      keys: () => request('harnessly/storage.keys', { scope }).then((r) => r.keys),
    };
  }
  // The values the person saved in the app's settings form (the manifest's `settings` schema), over its defaults. A
  // secret is not among them: the host fills it in on the machine, into the app's MCP servers, and nowhere else.
  hl.settings = {
    get: () => request('harnessly/settings.get', {}).then((r) => r.value),
  };
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
  // An onboarding step tells the host it is finished; other slots ignore it.
  hl.slot = { done: () => request('harnessly/slot.done', {}).then(() => undefined) };
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

  // A card and a slot view are as tall as their content.
  if (['chat', 'settings', 'onboarding', 'object-action'].includes(hl.context?.target) && typeof ResizeObserver === 'function') {
    new ResizeObserver(() => hl.resize(document.documentElement.scrollHeight)).observe(document.documentElement);
  }
  return hl;
}
