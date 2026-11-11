// The agent's surface of a page: hl.state, hl.action and hl.screen of the SDK the runtime serves, against the
// real bridge through the real shim (frame-rig.ts), and the loader of a site that exists already.
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import {
  classicSdk,
  FrameRig,
  HOST,
  NONCE,
  settle as rest,
  SHIM_SRC,
  SLOT,
} from '@/next/features/app-views/frame-rig';

/** Long enough for a registration and its publication to cross the bridge on a busy runner. */
const settle = (ms = 150) => rest(ms);

const sdkDir = new URL('../../../../../internal/apps/sdk/', import.meta.url);
const LOADER_SRC = readFileSync(new URL('loader.js', sdkDir), 'utf8');
const INLINE_RAW = readFileSync(new URL('loader-inline.js', sdkDir), 'utf8');
/** The line as a site pastes it: with the origin of the Harnessly page it trusts. */
const VIEWS = 'https://views.example.org/__hl/v1';
const INLINE_AT = INLINE_RAW.replace('https://HARNESSLY_VIEWS/__hl/v1', VIEWS);
const inlineFor = (host: string) => INLINE_AT.replace("['https://HARNESSLY_HOST']", `['${host}']`);
const INLINE_SRC = inlineFor(HOST);

const context: IViewContext = {
  app: { id: 'acme.admin', version: '1' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'dark', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

interface IIssue {
  source: string;
  field: string;
  level: string;
  message: string;
  rule?: string;
}
interface IHandle {
  unregister: () => void;
  refresh: () => void;
  clear: () => void;
}
interface ISdk {
  connect: (o?: object) => Promise<{ publish: (data: unknown) => Promise<void>; close: () => void }>;
  state: (name: string, def: object) => IHandle;
  action: (name: string, def: object) => IHandle;
  screen: (def: object) => IHandle;
  q?: unknown[] | null;
}
interface ITool {
  name: string;
  description: string;
  inputSchema?: Record<string, unknown>;
  confirm?: boolean;
}

const host = (permissions = ['tools.page', 'context.publish']) => {
  const r = new FrameRig();
  const seen = { tools: [] as ITool[], published: [] as unknown[], answers: new Map<string, unknown>() };
  const handlers: IBridgeHandlers = {
    pageTools: (tools) => void (seen.tools = tools as ITool[]),
    pageToolResult: (id, answer) => void seen.answers.set(id, answer),
    publish: (data) => void seen.published.push(data),
    confirm: async () => true,
  };
  r.bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.admin', version: '1', permissions, targets: ['pane'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  let calls = 0;
  /** The agent calls a tool of the page; the answer is what the view sent back. */
  const call = async (name: string, args: object = {}) => {
    const id = `call-${++calls}`;
    r.bridge.agentCall(id);
    r.bridge.notify('harnessly/notifications/tool-call', { id, name, arguments: args });
    await vi.waitFor(() => expect(seen.answers.has(id)).toBe(true), { timeout: 3000 });
    return (seen.answers.get(id) as { result: Record<string, unknown> }).result;
  };
  const last = () => seen.published[seen.published.length - 1] as Record<string, unknown>;
  return { r, seen, call, last };
};

const view = async (permissions?: string[]) => {
  const h = host(permissions);
  const dom = h.r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const sdk = (dom.window as unknown as { harnessly: ISdk }).harnessly;
  return { ...h, dom, sdk };
};

const form = (initial: Record<string, unknown>) => {
  let value = { ...initial };
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    patch: (p: Record<string, unknown>) => {
      value = { ...value, ...p };
      for (const fn of listeners) fn();
    },
    listen: (fn: () => void) => {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  };
};

describe('hl.state, hl.action, hl.screen', () => {
  it('publishes the state and the screen in the context the runtime reads, and offers <name>_set', async () => {
    const { sdk, seen, last } = await view();
    await sdk.connect();
    const f = form({ title: '', price: 10 });
    const rules = [{ id: 'title-case', text: 'No capitals.', examples: { pass: ['Jazz'], fail: ['JAZZ'] } }];
    sdk.screen({ title: 'New event', route: '/events/new', prompt: 'Ask the date first.' });
    sdk.state('event', {
      description: 'The form of a new event.',
      schema: { type: 'object', properties: { title: { type: 'string' } }, required: ['title'] },
      get: f.get,
      set: f.patch,
      rules,
    });
    await vi.waitFor(() => expect(seen.published.length).toBeGreaterThan(0));
    expect(last()).toEqual({
      screen: { title: 'New event', route: '/events/new', prompt: 'Ask the date first.' },
      states: {
        event: {
          description: 'The form of a new event.',
          schema: { type: 'object', properties: { title: { type: 'string' } }, required: ['title'] },
          value: { title: '', price: 10 },
          rules,
        },
      },
    });
    expect(seen.tools.map((t) => t.name)).toEqual(['event_set']);
    // A patch: the state's fields, none required; the rules in words are in the description.
    expect(seen.tools[0].inputSchema).toEqual({ type: 'object', properties: { title: { type: 'string' } } });
    expect(seen.tools[0].description).toContain('[title-case] No capitals.');
    expect(seen.tools[0].confirm).toBeUndefined();
  });

  it('answers a set with the value read back and every issue at once, in one shape', async () => {
    const { sdk, call, last } = await view();
    await sdk.connect();
    const f = form({ title: '', price: 10 });
    sdk.state('event', {
      description: 'd',
      schema: { type: 'object' },
      get: f.get,
      set: (patch: Record<string, unknown>) => {
        f.patch(patch);
        return [{ field: 'price', message: 'The price cannot be negative' }, 'The form is not complete'];
      },
      check: (value: { title: string }) =>
        [
          { field: 'title', message: 'No capitals', rule: 'title-case', source: 'form' },
          { field: 'discount', level: 'warn', message: 'Tell the person' },
        ].filter((i) => i.level === 'warn' || value.title === value.title.toUpperCase()),
    });
    await settle();
    const answer = await call('event_set', { title: 'CONCERT', price: -5 });
    expect(answer).toEqual({
      ok: false,
      value: { title: 'CONCERT', price: -5 },
      issues: [
        { source: 'form', field: 'price', level: 'error', message: 'The price cannot be negative' },
        { source: 'form', field: '', level: 'error', message: 'The form is not complete' },
        // A check is always a rule, whatever it calls itself.
        { source: 'rule', field: 'title', level: 'error', message: 'No capitals', rule: 'title-case' },
        { source: 'rule', field: 'discount', level: 'warn', message: 'Tell the person' },
      ] satisfies IIssue[],
    });
    // The value is republished after the set.
    expect((last().states as Record<string, { value: unknown }>).event.value).toEqual({
      title: 'CONCERT',
      price: -5,
    });
  });

  it('is ok with warnings alone, and turns a throw of set into an issue', async () => {
    const { sdk, call } = await view();
    await sdk.connect();
    const f = form({ a: 1 });
    sdk.state('soft', {
      get: f.get,
      set: f.patch,
      check: () => ({ issues: [{ level: 'warn', message: 'careful' }] }),
    });
    sdk.state('hard', {
      get: f.get,
      set: () => {
        throw new Error('read only');
      },
    });
    await settle();
    expect(await call('soft_set', { a: 2 })).toMatchObject({
      ok: true,
      value: { a: 2 },
      issues: [{ level: 'warn' }],
    });
    expect(await call('hard_set', { a: 3 })).toEqual({
      ok: false,
      value: { a: 2 },
      issues: [{ source: 'form', field: '', level: 'error', message: 'read only' }],
    });
  });

  it('republishes a subscribed state after the changes settle, and one without subscribe on refresh', async () => {
    const { sdk, seen, last } = await view();
    await sdk.connect();
    const live = form({ n: 0 });
    const quiet = form({ n: 0 });
    sdk.state('live', { get: live.get, set: live.patch, subscribe: live.listen });
    const handle = sdk.state('quiet', { get: quiet.get, set: quiet.patch });
    await settle();
    const before = seen.published.length;
    live.patch({ n: 1 });
    live.patch({ n: 2 });
    quiet.patch({ n: 5 });
    await settle(100);
    expect(seen.published.length).toBe(before);
    await settle(500);
    // One publication for the burst; it reads every state as it is now.
    expect(seen.published.length).toBe(before + 1);
    expect(last().states).toMatchObject({ live: { value: { n: 2 } }, quiet: { value: { n: 5 } } });
    quiet.patch({ n: 6 });
    handle.refresh();
    await settle();
    expect(last().states).toMatchObject({ quiet: { value: { n: 6 } } });
  });

  it('takes a state back with its tool and its subscription', async () => {
    const { sdk, seen, last } = await view();
    await sdk.connect();
    const f = form({ n: 0 });
    const stop = vi.fn();
    const handle = sdk.state('event', { get: f.get, set: f.patch, subscribe: () => ({ unsubscribe: stop }) });
    sdk.screen({ title: 'Events' });
    await settle();
    expect(seen.tools.map((t) => t.name)).toEqual(['event_set']);
    handle.unregister();
    await settle();
    expect(stop).toHaveBeenCalledOnce();
    expect(seen.tools).toEqual([]);
    expect(last()).toEqual({ screen: { title: 'Events', route: '', prompt: '' }, states: {} });
  });

  it('keeps what the view publishes itself under data, and publishes it plain without a state or a screen', async () => {
    const { sdk, last } = await view();
    const hl = await sdk.connect();
    await hl.publish({ rows: 3 });
    expect(last()).toEqual({ rows: 3 });
    const screen = sdk.screen({ title: 'List', route: '/list', prompt: '' });
    await settle();
    expect(last()).toEqual({
      screen: { title: 'List', route: '/list', prompt: '' },
      states: {},
      data: { rows: 3 },
    });
    await hl.publish({ rows: 4 });
    expect(last()).toMatchObject({ screen: { title: 'List' }, data: { rows: 4 } });
    // A replaced screen's clear does nothing; the last one's takes the screen back.
    const next = sdk.screen({ title: 'Card' });
    screen.clear();
    await settle();
    expect(last()).toMatchObject({ screen: { title: 'Card' } });
    next.clear();
    await settle();
    expect(last()).toEqual({ rows: 4 });
  });

  it('asks the person before a mutating action unless the author takes the question off', async () => {
    const { sdk, seen } = await view();
    await sdk.connect();
    sdk.action('save', { description: 'Save.', mutates: true, run: () => null });
    sdk.action('save_default', { description: 'Save.', run: () => null });
    sdk.action('save_quiet', { description: 'Save.', mutates: true, confirm: false, run: () => null });
    sdk.action('open_card', { description: 'Open.', mutates: false, run: () => null });
    await settle();
    expect(Object.fromEntries(seen.tools.map((t) => [t.name, t.confirm === true]))).toEqual({
      save: true,
      save_default: true,
      save_quiet: false,
      open_card: false,
    });
  });

  it('says ok only when run finished: a refusal of the server, the form or a rule comes as issues', async () => {
    const { sdk, call } = await view();
    await sdk.connect();
    const ran = vi.fn();
    sdk.action('done', { confirm: false, run: (args: { id: number }) => ({ id: args.id }) });
    sdk.action('warned', {
      confirm: false,
      run: () => ({ result: 7, issues: [{ level: 'warn', message: 'slow' }] }),
    });
    sdk.action('refused', {
      confirm: false,
      run: () => {
        throw new Error('409 Conflict');
      },
    });
    sdk.action('invalid', {
      confirm: false,
      run: () => ({ issues: [{ source: 'form', field: 'date', message: 'The date is in the past' }] }),
    });
    sdk.action('ruled', {
      confirm: false,
      check: () => [{ message: 'Not on Friday', rule: 'no-friday' }],
      run: ran,
    });
    await settle();
    expect(await call('done', { id: 4 })).toEqual({ ok: true, result: { id: 4 }, issues: [] });
    expect(await call('warned')).toEqual({
      ok: true,
      result: 7,
      issues: [{ source: 'server', field: '', level: 'warn', message: 'slow' }],
    });
    expect(await call('refused')).toEqual({
      ok: false,
      result: null,
      issues: [{ source: 'server', field: '', level: 'error', message: '409 Conflict' }],
    });
    expect(await call('invalid')).toEqual({
      ok: false,
      result: null,
      issues: [{ source: 'form', field: 'date', level: 'error', message: 'The date is in the past' }],
    });
    expect(await call('ruled')).toEqual({
      ok: false,
      result: null,
      issues: [{ source: 'rule', field: '', level: 'error', message: 'Not on Friday', rule: 'no-friday' }],
    });
    expect(ran).not.toHaveBeenCalled();
  });

  it('keeps what was registered before connect and offers it after the handshake', async () => {
    const { sdk, seen, last } = await view();
    const f = form({ n: 1 });
    sdk.screen({ title: 'Early' });
    sdk.state('early', { get: f.get, set: f.patch });
    const dropped = sdk.action('dropped', { run: () => null });
    sdk.action('kept', { run: () => null });
    dropped.unregister();
    await settle();
    expect(seen.tools).toEqual([]);
    expect(seen.published).toEqual([]);
    await sdk.connect();
    await settle();
    expect(seen.tools.map((t) => t.name).sort()).toEqual(['early_set', 'kept']);
    expect(last()).toMatchObject({ screen: { title: 'Early' }, states: { early: { value: { n: 1 } } } });
  });

  it('does nothing outside Harnessly and refuses a bad name at once', async () => {
    const dom = new JSDOM('<html><head></head><body></body></html>', {
      runScripts: 'outside-only',
      url: `${SLOT}/page`,
    });
    dom.window.eval(classicSdk());
    const sdk = (dom.window as unknown as { harnessly: ISdk }).harnessly;
    const f = form({ n: 1 });
    const handle = sdk.state('event', { get: f.get, set: f.patch });
    sdk.action('save', { run: () => null });
    sdk.screen({ title: 'x' }).clear();
    handle.refresh();
    handle.unregister();
    await settle();
    await expect(sdk.connect()).rejects.toMatchObject({ code: 'host_unavailable' });
    expect(() => sdk.state('1 bad', { get: f.get, set: f.patch })).toThrow(/latin letters/);
    expect(() => sdk.state('event', { get: f.get })).toThrow();
  });
});

describe('the loader of a site that exists already', () => {
  const SOURCES: Record<string, () => string> = {
    'loader.js': () => LOADER_SRC,
    'bridge-shim.js': () => SHIM_SRC,
    'app-sdk.classic.js': classicSdk,
  };
  /** The browser's part: every script the page appended runs from its address and tells onload. */
  const runAppended = async (dom: JSDOM, loaded: string[]) => {
    for (let round = 0; round < 6; round += 1) {
      const scripts = [...dom.window.document.querySelectorAll<HTMLScriptElement>('script[src]')].filter(
        (s) => !loaded.includes(s.src),
      );
      for (const s of scripts) {
        loaded.push(s.src);
        const name = s.src.slice(s.src.lastIndexOf('/') + 1);
        Object.defineProperty(dom.window.document, 'currentScript', { value: s, configurable: true });
        dom.window.eval(SOURCES[name]());
        s.onload?.(new dom.window.Event('load'));
      }
      await settle(10);
    }
  };
  const site = (url: string, parent: unknown) => {
    const dom = new JSDOM('<html><head></head><body></body></html>', {
      runScripts: 'outside-only',
      pretendToBeVisual: true,
      url,
    });
    if (parent)
      Object.defineProperty(dom.window, 'parent', { value: parent, writable: true, configurable: true });
    return dom;
  };

  it('loads nothing outside Harnessly and leaves a stand-in whose calls do nothing', async () => {
    const pages = [
      site('https://admin.example.org/events', null),
      // In somebody's frame, but the host gave no nonce.
      site('https://admin.example.org/events#tab', {}),
      // The nonce in the address of a page nobody framed.
      site(`https://admin.example.org/events#hl-nonce=${NONCE}`, null),
    ];
    for (const dom of pages) {
      dom.window.eval(INLINE_SRC);
      const stub = (dom.window as unknown as { harnessly: ISdk }).harnessly;
      expect(stub.q).toBeNull();
      const handle = stub.state('event', { get: () => ({}), set: () => undefined });
      handle.refresh();
      handle.unregister();
      stub.action('save', { run: () => null }).unregister();
      stub.screen({ title: 'x' }).clear();
      expect(dom.window.document.querySelectorAll('script[src]').length).toBe(0);
    }
  });

  it('loads the SDK by its versioned folder inside Harnessly and replays what the site called meanwhile', async () => {
    const { r, seen, last, call } = host();
    const dom = site(`https://admin.example.org/events?id=4#tab&hl-nonce=${NONCE}`, r.hostWindow);
    r.current = dom;
    dom.window.eval(INLINE_SRC);
    const stub = (dom.window as unknown as { harnessly: ISdk }).harnessly;
    const f = form({ title: '' });
    // The site's components mount before the SDK is there.
    stub.screen({ title: 'New event', route: '/events/new' });
    stub.state('event', { description: 'd', schema: { type: 'object' }, get: f.get, set: f.patch });
    const gone = stub.action('gone', { run: () => null });
    const save = stub.action('event_save', { mutates: true, run: () => ({ id: 1 }) });
    gone.unregister();

    const loaded: string[] = [];
    await runAppended(dom, loaded);
    expect(loaded).toEqual([`${VIEWS}/loader.js`, `${VIEWS}/bridge-shim.js`, `${VIEWS}/app-sdk.classic.js`]);
    // The nonce went to the shim through the meta and left the address; the rest of the address stays.
    expect(dom.window.location.href).toBe('https://admin.example.org/events?id=4#tab');
    await settle(60);
    expect(seen.tools.map((t) => t.name).sort()).toEqual(['event_save', 'event_set']);
    expect(last()).toMatchObject({
      screen: { title: 'New event' },
      states: { event: { value: { title: '' } } },
    });
    expect(await call('event_set', { title: 'Jazz' })).toEqual({
      ok: true,
      value: { title: 'Jazz' },
      issues: [],
    });
    // A handle the site got from the stand-in works on the real registration.
    save.unregister();
    await settle();
    expect(seen.tools.map((t) => t.name)).toEqual(['event_set']);
    // The stand-in the site may have kept calls the SDK now.
    stub.action('later', { mutates: false, run: () => null });
    await settle();
    expect(seen.tools.map((t) => t.name).sort()).toEqual(['event_set', 'later']);
  });

  it('gives a page of another origin that frames the site no state and no tool', async () => {
    // The page in the rig answers as HOST; the site trusts another Harnessly.
    const { r, seen } = host();
    const dom = site(`https://admin.example.org/events#hl-nonce=${NONCE}`, r.hostWindow);
    r.current = dom;
    dom.window.eval(inlineFor('https://harnessly.example.org'));
    const stub = (dom.window as unknown as { harnessly: ISdk }).harnessly;
    stub.state('event', { get: () => ({ card: '4111' }), set: () => undefined });
    stub.action('refund', { run: () => null });
    await runAppended(dom, []);
    await settle(60);
    expect(seen.tools).toEqual([]);
    expect(seen.published).toEqual([]);
  });

  it('loads nothing when the line names no Harnessly origin', async () => {
    const { r } = host();
    const dom = site(`https://admin.example.org/events#hl-nonce=${NONCE}`, r.hostWindow);
    dom.window.eval(INLINE_AT.replace("['https://HARNESSLY_HOST']", '[]'));
    const loaded: string[] = [];
    await runAppended(dom, loaded);
    expect(loaded).toEqual([`${VIEWS}/loader.js`]);
  });
});
