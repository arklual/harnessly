import { describe, expect, it, vi } from 'vitest';
import {
  AppBridge,
  BridgeError,
  type IBridgePort,
  METHOD_PERMISSION,
  RATE_LIMIT,
  type IBridgeApp,
  type IBridgeHandlers,
  type IViewContext,
} from '@/next/features/app-views/bridge';

const ORIGIN = 'https://slot6.example.org';

const context: IViewContext = {
  app: { id: 'acme.demo', version: '1.0.0' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'light', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

type TLoose = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

interface ISent {
  message: TLoose;
  origin: string;
}

const rig = (
  permissions: string[],
  handlers: IBridgeHandlers = {},
  over: Partial<IViewContext> = {},
  nonce?: string,
  targets: string[] = ['pane', 'chat'],
) => {
  const sent: ISent[] = [];
  const transfers: unknown[][] = [];
  const frame = {
    postMessage: (message: unknown, origin: string, transfer?: unknown[]) => {
      sent.push({ message: message as TLoose, origin });
      if (transfer) transfers.push(transfer);
    },
  };
  // The channel the bridge makes at the handshake: port1 is the host's end, the view's end is a marker the frame is handed.
  let host: IBridgePort | null = null;
  let closed = false;
  const channel = () => {
    const port1: IBridgePort = {
      onmessage: null,
      postMessage: (message) => void sent.push({ message: message as TLoose, origin: 'port' }),
      close: () => void (closed = true),
    };
    host = port1;
    return { port1, port2: { view: true } };
  };
  let t = 1000;
  const app: IBridgeApp = { id: 'acme.demo', version: '1.0.0', permissions, targets };
  const bridge = new AppBridge({
    frame: () => frame,
    origin: ORIGIN,
    app,
    context: { ...context, ...over },
    handlers,
    hostVersion: '0.17.0',
    nonce,
    channel,
    now: () => t,
  });
  const call = async (
    method: string,
    params: unknown = {},
    id: number | string = 1,
    from: { source?: unknown; origin?: string } = {},
  ) => {
    const data = { jsonrpc: '2.0', id, method, params };
    // Once the handshake handed the port over, the view's calls come on it.
    const viaPort = host !== null && !('source' in from) && from.origin === undefined;
    if (viaPort) (host as IBridgePort | null)?.onmessage?.({ data });
    const heard =
      viaPort ||
      bridge.onMessage({
        source: 'source' in from ? from.source : frame,
        origin: from.origin ?? ORIGIN,
        data,
      });
    await vi
      .waitFor(() => (heard ? expect(sent.length).toBeGreaterThan(0) : undefined), { timeout: 200 })
      .catch(() => undefined);
    await Promise.resolve();
    return heard;
  };
  const last = () => sent.at(-1)?.message;
  const start = async () => {
    await call('ui/initialize', {}, 'init');
    (host as IBridgePort | null)?.onmessage?.({
      data: { jsonrpc: '2.0', method: 'ui/notifications/initialized', params: {} },
    });
    await Promise.resolve();
    sent.length = 0;
  };
  return {
    bridge,
    frame,
    sent,
    call,
    last,
    start,
    transfers,
    port: () => host,
    isClosed: () => closed,
    tick: (ms: number) => (t += ms),
  };
};

describe('AppBridge: the nonce of the loaded document', () => {
  it('refuses ui/initialize without the nonce or with another', async () => {
    const r = rig([], {}, {}, 'n-1');
    await r.call('ui/initialize', {}, 1);
    expect(r.last()?.error.data.code).toBe('host_unavailable');
    r.sent.length = 0;
    await r.call('ui/initialize', { nonce: 'n-2' }, 2);
    expect(r.last()?.error.data.code).toBe('host_unavailable');
    expect(r.last()?.result).toBeUndefined();
  });

  it('does not let a document skip the handshake', async () => {
    const r = rig(['storage'], { storage: async () => 'secret' }, {}, 'n-1');
    await r.call('ui/initialize', { nonce: 'wrong' }, 1);
    await r.call('ui/notifications/initialized', {}, 2);
    r.sent.length = 0;
    await r.call('harnessly/storage.get', { scope: 'install', key: 'k' }, 3);
    expect(r.last()?.result).toBeUndefined();
    expect(r.last()?.error.data.code).toBe('host_unavailable');
  });

  it('answers the document that presents the nonce', async () => {
    const r = rig(['storage'], { storage: async () => ({ value: 'ok', version: 3 }) }, {}, 'n-1');
    await r.call('ui/initialize', { nonce: 'n-1' }, 1);
    expect(r.last()?.result.hostInfo.name).toBe('harnessly');
    r.port()?.onmessage?.({ data: { jsonrpc: '2.0', method: 'ui/notifications/initialized', params: {} } });
    r.sent.length = 0;
    await r.call('harnessly/storage.get', { scope: 'install', key: 'k' }, 2);
    expect(r.last()?.result).toEqual({ value: 'ok', version: 3 });
  });
});

describe('AppBridge: the port handed over at the handshake', () => {
  it('hands the port to the document that passed the handshake, with the answer, and to nobody else', async () => {
    const r = rig(['storage'], { storage: async () => 'ok' }, {}, 'n-1');
    await r.call('ui/initialize', { nonce: 'wrong' }, 1);
    expect(r.transfers).toEqual([]);
    expect(r.port()).toBeNull();
    await r.call('ui/initialize', { nonce: 'n-1' }, 2);
    expect(r.transfers).toEqual([[{ view: true }]]);
  });

  it('ignores a call on the window after the handshake, a navigated document included', async () => {
    const storage = vi.fn(async () => 'secret');
    const r = rig(['storage'], { storage });
    await r.start();
    const heard = r.bridge.onMessage({
      source: r.frame,
      origin: ORIGIN,
      data: {
        jsonrpc: '2.0',
        id: 9,
        method: 'harnessly/storage.get',
        params: { scope: 'install', key: 'k' },
      },
    });
    await Promise.resolve();
    expect(heard).toBe(false);
    expect(storage).not.toHaveBeenCalled();
    expect(r.sent).toEqual([]);
    // Not even another handshake on the window gets the port.
    expect(
      r.bridge.onMessage({
        source: r.frame,
        origin: ORIGIN,
        data: { jsonrpc: '2.0', id: 10, method: 'ui/initialize', params: {} },
      }),
    ).toBe(false);
    expect(r.transfers).toHaveLength(1);
  });

  it('closes the port when disposed, and hears nothing on it', async () => {
    const storage = vi.fn(async () => 'x');
    const r = rig(['storage'], { storage });
    await r.start();
    const port = r.port();
    r.bridge.dispose();
    expect(r.isClosed()).toBe(true);
    port?.onmessage?.({
      data: {
        jsonrpc: '2.0',
        id: 3,
        method: 'harnessly/storage.get',
        params: { scope: 'install', key: 'k' },
      },
    });
    await Promise.resolve();
    expect(storage).not.toHaveBeenCalled();
  });
});

describe('AppBridge: who is heard', () => {
  it('hears the frame on its origin, and nobody else', async () => {
    const r = rig([]);
    expect(await r.call('ui/initialize')).toBe(true);
    expect(r.last()?.result.hostInfo.name).toBe('harnessly');
    r.sent.length = 0;
    expect(await r.call('ui/initialize', {}, 2, { source: { postMessage: () => undefined } })).toBe(false);
    expect(await r.call('ui/initialize', {}, 3, { origin: 'https://evil.example.org' })).toBe(false);
    expect(await r.call('ui/initialize', {}, 4, { origin: 'null' })).toBe(false);
    expect(await r.call('ui/initialize', {}, 5, { source: null })).toBe(false);
    expect(r.sent).toEqual([]);
  });

  it('answers only to the slot origin', async () => {
    const r = rig([]);
    await r.call('ui/initialize');
    expect(r.sent.every((s) => s.origin === ORIGIN)).toBe(true);
  });

  it('ignores what is not JSON-RPC', () => {
    const r = rig([]);
    for (const data of [
      null,
      'x',
      7,
      [],
      { method: 'ui/initialize' },
      { jsonrpc: '2.0' },
      { jsonrpc: '1.0', method: 'x' },
    ])
      expect(r.bridge.onMessage({ source: r.frame, origin: ORIGIN, data })).toBe(false);
  });

  it('hears nothing once disposed', () => {
    const r = rig([]);
    r.bridge.dispose();
    expect(
      r.bridge.onMessage({
        source: r.frame,
        origin: ORIGIN,
        data: { jsonrpc: '2.0', id: 1, method: 'ui/initialize' },
      }),
    ).toBe(false);
  });
});

describe('AppBridge: what the manifest grants', () => {
  it('refuses every call before the handshake is done', async () => {
    const r = rig(['storage'], { storage: async () => 1 });
    await r.call('harnessly/storage.get', { scope: 'install', key: 'k' });
    expect(r.last()?.error.data.code).toBe('host_unavailable');
  });

  it('storage carries versions: get answers {value, version}, set takes ifVersion and answers the version', async () => {
    const storage = vi.fn(async (...args: unknown[]) =>
      args[0] === 'get' ? { value: { a: 1 }, version: 4 } : args[0] === 'set' ? { version: 5 } : undefined,
    );
    const r = rig(['storage'], { storage });
    await r.start();
    await r.call('harnessly/storage.get', { scope: 'chat', key: 'k' }, 2);
    expect(r.last()?.result).toEqual({ value: { a: 1 }, version: 4 });
    await r.call('harnessly/storage.set', { scope: 'chat', key: 'k', value: 2, ifVersion: 4 }, 3);
    expect(storage).toHaveBeenLastCalledWith('set', 'chat', 'k', 2, 4);
    expect(r.last()?.result).toEqual({ version: 5 });
    await r.call('harnessly/storage.set', { scope: 'chat', key: 'k', value: 2 }, 4);
    expect(storage).toHaveBeenLastCalledWith('set', 'chat', 'k', 2, undefined);
    await r.call('harnessly/storage.set', { scope: 'chat', key: 'k', value: 2, ifVersion: -1 }, 5);
    expect(r.last()?.error.data.code).toBe('invalid_params');
    await r.call('harnessly/storage.set', { scope: 'chat', key: 'k', value: 2, ifVersion: 'x' }, 6);
    expect(r.last()?.error.data.code).toBe('invalid_params');
  });

  it('a conflict reaches the view with its code and the current version', async () => {
    const storage = vi.fn(async () => {
      throw new BridgeError('conflict', 'the key changed', { version: 7 });
    });
    const r = rig(['storage'], { storage });
    await r.start();
    await r.call('harnessly/storage.set', { scope: 'user', key: 'k', value: 1, ifVersion: 2 }, 2);
    expect(r.last()?.error.data).toEqual({ code: 'conflict', version: 7 });
  });

  it('no longer has the flow and data methods', async () => {
    const r = rig([]);
    await r.start();
    for (const method of [
      'harnessly/flow.status',
      'harnessly/flow.go',
      'harnessly/data.get',
      'harnessly/data.set',
    ]) {
      await r.call(method, {}, method);
      expect(r.last()?.error.code, method).toBe(-32601);
    }
  });

  it('gates each method by its permission', async () => {
    const chatSend = vi.fn(async () => undefined);
    const chatRead = vi.fn(async () => []);
    const r = rig(['chat.prompt'], { chatSend, chatRead });
    await r.start();
    await r.call('ui/message', { content: [{ type: 'text', text: 'hi' }] });
    expect(r.last()?.result).toEqual({});
    expect(chatSend).toHaveBeenCalledWith('hi');
    await r.call('harnessly/chat.read', { last: 5 }, 2);
    expect(r.last()?.error.data.code).toBe('permission_denied');
    expect(chatRead).not.toHaveBeenCalled();
  });

  it('agents.list answers the rows with agents.read and permission_denied without it', async () => {
    const agents = vi.fn(async () => [
      { id: 'tab-a', title: 'A', status: 'free', provider: 'claude', machine: 'M' },
    ]);
    const without = rig(['chat.read'], { agents });
    await without.start();
    await without.call('harnessly/agents.list', {}, 2);
    expect(without.last()?.error.data.code).toBe('permission_denied');
    expect(agents).not.toHaveBeenCalled();
    const r = rig(['agents.read'], { agents });
    await r.start();
    await r.call('harnessly/agents.list', {}, 2);
    expect(r.last()?.result).toEqual({
      agents: [{ id: 'tab-a', title: 'A', status: 'free', provider: 'claude', machine: 'M' }],
    });
    expect(JSON.stringify(r.last()?.result)).not.toContain('text');
  });

  it('pane.open of chat:<id> needs agents.read besides pane.open', async () => {
    const paneOpen = vi.fn(async () => undefined);
    const only = rig(['pane.open'], { paneOpen });
    await only.start();
    await only.call('harnessly/pane.open', { target: 'chat:tab-a' }, 2);
    expect(only.last()?.error.data.code).toBe('permission_denied');
    expect(paneOpen).not.toHaveBeenCalled();
    await only.call('harnessly/pane.open', { target: 'app:acme.demo' }, 3);
    expect(paneOpen).toHaveBeenCalledWith('app:acme.demo');
    const both = rig(['pane.open', 'agents.read'], { paneOpen });
    await both.start();
    await both.call('harnessly/pane.open', { target: 'chat:tab-a' }, 2);
    expect(paneOpen).toHaveBeenCalledWith('chat:tab-a');
  });

  it('every method that needs a permission is refused without it', async () => {
    const noop = async () => undefined;
    const handlers: IBridgeHandlers = {
      storage: noop,
      chatSend: noop,
      chatRead: async () => [],
      chatCard: noop,
      paneOpen: noop,
      agents: async () => [],
      notify: noop,
    };
    for (const [method, need] of Object.entries(METHOD_PERMISSION)) {
      if (need === '') continue;
      const r = rig([], handlers);
      await r.start();
      await r.call(method, {
        scope: 'install',
        key: 'k',
        view: 'v',
        target: 'files',
        text: 'x',
        content: [{ type: 'text', text: 'x' }],
      });
      expect(r.last()?.error?.data?.code, method).toBe('permission_denied');
    }
  });

  it('files are reached with a files mask, and the runtime gets the path as the view wrote it', async () => {
    const files = vi.fn(async (op: string) => (op === 'read' ? 'alpha' : undefined));
    const none = rig(['storage'], { files });
    await none.start();
    await none.call('harnessly/files.read', { path: 'docs/a.md' });
    expect(none.last()?.error.data.code).toBe('permission_denied');
    const r = rig(['files.read:docs/**'], { files });
    await r.start();
    await r.call('harnessly/files.read', { path: 'docs/a.md' }, 2);
    expect(r.last()?.result).toEqual({ text: 'alpha' });
    expect(files).toHaveBeenCalledWith('read', 'docs/a.md');
    // A write is a right of its own: a read mask does not give it.
    await r.call('harnessly/files.write', { path: 'docs/a.md', text: 'x' }, 3);
    expect(r.last()?.error.data.code).toBe('permission_denied');
    const w = rig(['files.write:docs/*.md'], { files });
    await w.start();
    await w.call('harnessly/files.write', { path: 'docs/a.md', text: 'x' }, 2);
    expect(files).toHaveBeenCalledWith('write', 'docs/a.md', 'x');
    await w.call('harnessly/files.write', { path: 'docs/a.md' }, 3);
    expect(w.last()?.error.data.code).toBe('invalid_params');
  });

  it('the space is written only with storage.space.write', async () => {
    const storage = vi.fn(async () => undefined);
    const r = rig(['storage'], { storage });
    await r.start();
    await r.call('harnessly/storage.set', { scope: 'space', key: 'k', value: 1 });
    expect(r.last()?.error.data.code).toBe('permission_denied');
    await r.call('harnessly/storage.get', { scope: 'space', key: 'k' }, 2);
    expect(r.last()?.result).toEqual({ value: null, version: 0 });
    const w = rig(['storage', 'storage.space.write'], { storage });
    await w.start();
    await w.call('harnessly/storage.set', { scope: 'space', key: 'k', value: 1 });
    expect(w.last()?.result).toEqual({ version: 0 });
    expect(storage).toHaveBeenCalledWith('set', 'space', 'k', 1, undefined);
  });

  it('a document open for reading is read, not changed: its storage and boards refuse writes, the rest works', async () => {
    const storage = vi.fn(async () => ({ value: 'v', version: 0 }));
    const sinopia = vi.fn(async () => ({}));
    const r = rig(
      ['storage', 'sinopia.read', 'sinopia.write'],
      { storage, sinopia },
      { readOnly: true, target: 'sinopia.acme.moodboard' },
      undefined,
      ['sinopia.acme.moodboard'],
    );
    await r.start();
    await r.call('harnessly/storage.set', { scope: 'doc', key: 'board', value: 1 });
    expect(r.last()?.error.data.code).toBe('permission_denied');
    await r.call('harnessly/storage.delete', { scope: 'doc', key: 'board' }, 2);
    expect(r.last()?.error.data.code).toBe('permission_denied');
    await r.call('harnessly/sinopia.patch', { ops: [] }, 3);
    expect(r.last()?.error.data.code).toBe('permission_denied');
    expect(storage).not.toHaveBeenCalled();
    expect(sinopia).not.toHaveBeenCalled();
    // Reading goes on, and the view's other storage is its own.
    await r.call('harnessly/storage.get', { scope: 'doc', key: 'board' }, 4);
    expect(r.last()?.result).toEqual({ value: 'v', version: 0 });
    await r.call('harnessly/storage.set', { scope: 'user', key: 'zoom', value: 2 }, 5);
    expect(r.last()?.result).toEqual({ version: 0 });
    // The view is told at the handshake.
    const fresh = rig([], {}, { readOnly: true });
    await fresh.call('ui/initialize', {}, 'init');
    expect(fresh.last()?.result.hostContext.harnessly.readOnly).toBe(true);
  });

  it('refuses a view shown where the app does not work', async () => {
    const r = rig(['storage'], { storage: async () => 1 }, { target: 'app' });
    await r.call('ui/initialize');
    expect(r.last()?.error.data.code).toBe('not_in_target');
  });

  it('answers an unknown method as one the host does not have', async () => {
    const r = rig(['storage']);
    await r.start();
    await r.call('harnessly/fs.read', { path: '/etc/passwd' });
    expect(r.last()?.error.code).toBe(-32601);
  });

  it('turns a handler failure into the code a view sees', async () => {
    const r = rig([], {
      tool: async () => {
        throw Object.assign(new Error('nope'), { code: 'policy_blocked' });
      },
    });
    await r.start();
    await r.call('tools/call', { name: 'bank.create' });
    expect(r.last()?.error.data.code).toBe('policy_blocked');
    expect(r.last()?.error.message).toBe('nope');
  });

  it('checks parameters', async () => {
    const r = rig(['storage', 'pane.open'], { storage: async () => 1, paneOpen: async () => undefined });
    await r.start();
    await r.call('harnessly/storage.get', { scope: 'root', key: 'k' });
    expect(r.last()?.error.data.code).toBe('invalid_params');
    await r.call('harnessly/pane.open', {}, 2);
    expect(r.last()?.error.data.code).toBe('invalid_params');
    await r.call('ui/open-link', { url: 'javascript:alert(1)' }, 3);
    expect(r.last()?.error.data.code).toBe('invalid_params');
  });

  it('page tools need tools.page, carry checked tools and answers, and the context needs context.publish', async () => {
    const got: { tools?: unknown; result?: unknown; data?: unknown } = {};
    const handlers: IBridgeHandlers = {
      pageTools: (tools) => void (got.tools = tools),
      pageToolResult: (id, answer) => void (got.result = { id, answer }),
      publish: (data) => void (got.data = data),
    };
    const none = rig([], handlers);
    await none.start();
    await none.call('harnessly/tools.register', { tools: [] }, 5);
    expect(none.last()?.error.data.code).toBe('permission_denied');
    const r = rig(['tools.page', 'context.publish'], handlers);
    await r.start();
    await r.call(
      'harnessly/tools.register',
      { tools: [{ name: 'fill', description: 'd', inputSchema: { type: 'object' } }] },
      6,
    );
    expect(got.tools).toEqual([{ name: 'fill', description: 'd', inputSchema: { type: 'object' } }]);
    await r.call('harnessly/tools.register', { tools: [{ name: '1 bad' }] }, 7);
    expect(r.last()?.error.data.code).toBe('invalid_params');
    await r.call('harnessly/tools.result', { id: 'c1', result: { ok: true } }, 8);
    expect(got.result).toEqual({ id: 'c1', answer: { result: { ok: true } } });
    await r.call('harnessly/context.publish', { data: { step: 2 } }, 9);
    expect(got.data).toEqual({ step: 2 });
  });

  it('limits a view to sixty calls a second', async () => {
    const r = rig([], { toast: () => undefined });
    await r.start();
    for (let i = 0; i < RATE_LIMIT; i++) await r.call('harnessly/ui.toast', { text: 'x' }, i + 10);
    await r.call('harnessly/ui.toast', { text: 'x' }, 999);
    expect(r.last()?.error.data.code).toBe('rate_limited');
    r.tick(1100);
    await r.call('harnessly/ui.toast', { text: 'x' }, 1000);
    expect(r.last()?.result).toEqual({});
  });
});

describe('AppBridge: toasts and keys (HRN-808)', () => {
  it('passes a tone and a second line with a toast, and drops a tone it does not know', async () => {
    const toast = vi.fn();
    const r = rig([], { toast });
    await r.start();
    await r.call('harnessly/ui.toast', { text: 'Готово', tone: 'success', description: 'всё' }, 20);
    expect(toast).toHaveBeenLastCalledWith('Готово', { tone: 'success', description: 'всё' });
    await r.call('harnessly/ui.toast', { text: 'x', tone: 'loud' }, 21);
    expect(toast).toHaveBeenLastCalledWith('x', {});
  });

  it('runs the commands of the screen it names and tells the view each press', async () => {
    let handlers: (() => Record<string, ((chord: string) => void | false) | undefined>) | null = null;
    const stop = vi.fn();
    const r = rig([], {
      keys: (_scope, h) => {
        handlers = h;
        return stop;
      },
    });
    await r.start();
    await r.call('harnessly/keys.on', { scope: 'deck', commands: ['deck.next'] }, 30);
    expect(r.last()?.result).toEqual({});
    handlers!()['deck.next']?.('ArrowRight');
    expect(r.last()).toMatchObject({
      method: 'harnessly/notifications/key',
      params: { command: 'deck.next', chord: 'ArrowRight' },
    });
    await r.call('harnessly/keys.on', { scope: 'deck', commands: ['global.newChat'] }, 31);
    expect(r.last()?.error.data.code).toBe('invalid_params');
    await r.call('harnessly/keys.on', { scope: 'global', commands: ['global.help'] }, 32);
    expect(r.last()?.error.data.code).toBe('invalid_params');
    await r.call('harnessly/keys.off', { scope: 'deck' }, 33);
    expect(stop).toHaveBeenCalledTimes(1);
  });

  it('answers whether a press ran, and how the host writes a key', async () => {
    const r = rig([], { keyPress: (chord) => chord === 'ArrowRight', keyHints: (c) => ({ [c[0]]: '→' }) });
    await r.start();
    await r.call('harnessly/keys.press', { chord: 'ArrowRight' }, 40);
    expect(r.last()?.result).toEqual({ ran: true });
    await r.call('harnessly/keys.hints', { commands: ['deck.next'] }, 41);
    expect(r.last()?.result).toEqual({ hints: { 'deck.next': '→' } });
  });
});

describe('AppBridge: notifications', () => {
  it('tells the view when its context changes, once it is up', async () => {
    const r = rig([]);
    r.bridge.setContext({ ...context, locale: 'en' });
    expect(r.sent).toEqual([]);
    await r.start();
    r.bridge.setContext({ ...context, locale: 'de' });
    expect(r.last()?.method).toBe('ui/notifications/host-context-changed');
    expect(r.last()?.params.locale).toBe('de');
  });
});

describe('AppBridge: a view on an opaque origin', () => {
  const opaque = () => {
    const sent: ISent[] = [];
    const frame = {
      postMessage: (message: unknown, origin: string) =>
        void sent.push({ message: message as TLoose, origin }),
    };
    const bridge = new AppBridge({
      frame: () => frame,
      origin: 'null',
      replyTo: '*',
      app: { id: 'acme.demo', version: '1', permissions: [], targets: ['pane'] },
      context,
      handlers: {},
      hostVersion: '1',
    });
    return { bridge, frame, sent };
  };

  it('hears its own frame on origin "null" and answers to the frame’s window', async () => {
    const r = opaque();
    expect(
      r.bridge.onMessage({
        source: r.frame,
        origin: 'null',
        data: { jsonrpc: '2.0', id: 1, method: 'ui/initialize' },
      }),
    ).toBe(true);
    await vi.waitFor(() => expect(r.sent).toHaveLength(1));
    expect(r.sent[0].origin).toBe('*');
  });

  it('does not hear another window, nor the frame on a real origin', () => {
    const r = opaque();
    const data = { jsonrpc: '2.0', id: 1, method: 'ui/initialize' };
    // A sibling frame is also "null": the window is what tells them apart.
    expect(r.bridge.onMessage({ source: { postMessage: () => undefined }, origin: 'null', data })).toBe(
      false,
    );
    expect(r.bridge.onMessage({ source: r.frame, origin: 'https://evil.example.org', data })).toBe(false);
    expect(r.sent).toEqual([]);
  });
});

describe('AppBridge: the Document API of Sinopia', () => {
  const slides = (permissions: string[], handlers: IBridgeHandlers, targets = ['sinopia.slides']) =>
    rig(permissions, handlers, { target: 'sinopia.slides' }, undefined, targets);

  it('does the work of a read with sinopia.read and gives the view what the runtime answered', async () => {
    const sinopia = vi.fn(async () => ({ nodes: [], count: 0 }));
    const r = slides(['sinopia.read'], { sinopia });
    await r.start();
    await r.call('harnessly/sinopia.query', { selector: 'h1' });
    expect(sinopia).toHaveBeenCalledWith('query', { selector: 'h1' });
    expect(r.last()?.result).toEqual({ result: { nodes: [], count: 0 } });
  });

  it('holds each method to its own right: a read does not write, a write does not comment or export', async () => {
    const sinopia = vi.fn(async () => ({}));
    const r = slides(['sinopia.read'], { sinopia });
    await r.start();
    for (const [method, need] of [
      ['patch', 'sinopia.write'],
      ['setFields', 'sinopia.write'],
      ['boards.add', 'sinopia.write'],
      ['comment', 'sinopia.comment'],
      ['render', 'sinopia.export'],
    ]) {
      await r.call(`harnessly/sinopia.${method}`, {});
      expect(r.last()?.error?.data?.code, method).toBe('permission_denied');
      expect(r.last()?.error?.message, method).toContain(need);
    }
    expect(sinopia).not.toHaveBeenCalled();
  });

  it('works in the kind of the target only: sinopia.slides is not sinopia.site, sinopia.* is every kind', async () => {
    const sinopia = vi.fn(async () => ({}));
    const site = rig(['sinopia.read'], { sinopia }, { target: 'sinopia.site' }, undefined, [
      'sinopia.slides',
    ]);
    await site.call('ui/initialize');
    expect(site.last()?.error.data.code).toBe('not_in_target');
    const any = rig(['sinopia.read'], { sinopia }, { target: 'sinopia.site' }, undefined, ['sinopia.*']);
    await any.start();
    await any.call('harnessly/sinopia.document', {});
    expect(any.last()?.result).toEqual({ result: {} });
  });

  it('passes the refusal of the runtime to the view with its code', async () => {
    const r = slides(['sinopia.write'], {
      sinopia: async () => {
        throw new BridgeError('policy_blocked', 'another chat is working there');
      },
    });
    await r.start();
    await r.call('harnessly/sinopia.patch', { ops: [] });
    expect(r.last()?.error.data.code).toBe('policy_blocked');
    expect(r.last()?.error.message).toBe('another chat is working there');
  });

  it('is host_unavailable where the host gives no Document API', async () => {
    const r = slides(['sinopia.read'], {});
    await r.start();
    await r.call('harnessly/sinopia.document', {});
    expect(r.last()?.error.data.code).toBe('host_unavailable');
  });
});
