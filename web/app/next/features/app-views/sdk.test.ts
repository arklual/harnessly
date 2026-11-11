// The SDK the runtime serves (internal/apps/sdk/app-sdk.js) in a frame of the page, in one process: a real DOM for the
// view, the real bridge for the host and the real bridge shim between them (see frame-rig.ts).
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';
// Loaded by its address, not imported: the interface's image is built from web/ alone, without the SDK's folder.
const { connect } = (await import(
  /* @vite-ignore */ new URL('../../../../../internal/apps/sdk/app-sdk.js', import.meta.url).href
)) as { connect: () => Promise<unknown> };

const context: IViewContext = {
  app: { id: 'acme.demo', version: '1' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'dark', tokens: { '--t-bg': '#111' } },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

interface IHl {
  context: IViewContext & { app: { id: string } };
  storage: { install: { get: (k: string) => Promise<unknown> } };
  chat: { send: (t: string) => Promise<void>; read: (o: { last: number }) => Promise<unknown> };
  on: (event: string, fn: (v: unknown) => void) => void;
  protocol: number;
  has: (name: string) => boolean;
  refused: (v: unknown) => boolean;
  ui: { toast: (t: string, o?: { tone?: string }) => Promise<void> };
  keys: {
    on: (
      scope: string,
      h: Record<string, (chord: string) => void>,
    ) => (() => Promise<void>) & { ready: Promise<void> };
  };
}

const view = async (
  permissions: string[],
  handlers: IBridgeHandlers,
  options: { protocol?: number } = {},
) => {
  const r = new FrameRig();
  r.bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.demo', version: '1', permissions, targets: ['pane'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  const hl = await harnessly.connect(options);
  return { r, dom, hl };
};

describe('@harnessly/app-sdk against the bridge, through the shim', () => {
  it('shakes hands, takes the theme and calls what the manifest grants', async () => {
    const storage = vi.fn(async () => ({ value: 'stored', version: 1 }));
    const chatSend = vi.fn(async () => undefined);
    const { dom, hl } = await view(['storage', 'chat.prompt'], { storage, chatSend });
    expect(hl.context.app.id).toBe('acme.demo');
    expect(dom.window.document.documentElement.dataset.theme).toBe('dark');
    expect(dom.window.document.documentElement.style.getPropertyValue('--t-bg')).toBe('#111');

    expect(await hl.storage.install.get('k')).toBe('stored');
    expect(storage).toHaveBeenCalledWith('get', 'install', 'k');
    await hl.chat.send('hi');
    expect(chatSend).toHaveBeenCalledWith('hi');
  });

  it('throws HarnesslyError with the code when the manifest does not grant', async () => {
    const { hl } = await view([], { chatRead: async () => [] });
    const failure = await hl.chat.read({ last: 3 }).catch((e: unknown) => e);
    expect(failure).toMatchObject({ name: 'HarnesslyError', code: 'permission_denied' });
  });

  it('says what this host does for the app with has, and keeps protocol 1 for a view that names none', async () => {
    const { hl } = await view(['storage'], {});
    expect(hl.protocol).toBe(1);
    expect(hl.has('storage.user.get')).toBe(true);
    expect(hl.has('harnessly/storage.set')).toBe(true);
    expect(hl.has('chat.send')).toBe(false);
    expect(hl.has('files.watch')).toBe(false);
    expect(hl.has('ui.toast')).toBe(true);
  });

  it('answers a refusal as a value under protocol 2, without an exception', async () => {
    const { hl } = await view([], { chatRead: async () => [] }, { protocol: 2 });
    expect(hl.protocol).toBe(2);
    const answer = await hl.chat.read({ last: 3 });
    expect(answer).toMatchObject({ error: 'denied', code: 'permission_denied' });
    expect(hl.refused(answer)).toBe(true);
    expect(hl.refused([])).toBe(false);
  });

  it('runs the host’s shortcut commands of a screen and toasts with a tone (HRN-808)', async () => {
    let handlers: (() => Record<string, ((chord: string) => void | false) | undefined>) | null = null;
    const toast = vi.fn();
    const { hl } = await view([], {
      toast,
      keys: (_scope, h) => {
        handlers = h;
        return () => undefined;
      },
    });
    expect(hl.has('keys.on')).toBe(true);
    await hl.ui.toast('Готово', { tone: 'success' });
    expect(toast).toHaveBeenCalledWith('Готово', { tone: 'success' });
    const heard: string[] = [];
    const stop = hl.keys.on('deck', { 'deck.next': (chord: string) => heard.push(chord) });
    await stop.ready;
    handlers!()['deck.next']?.('ArrowRight');
    await vi.waitFor(() => expect(heard).toEqual(['ArrowRight']));
    await stop();
  });

  it('is told of a changed context', async () => {
    const { r, hl } = await view([], {});
    const seen: unknown[] = [];
    hl.on('theme', (t) => seen.push(t));
    // The view's `initialized` travels the port, which takes a turn of the event loop.
    await settle(30);
    r.bridge.setContext({ ...context, theme: { mode: 'light', tokens: {} } });
    await vi.waitFor(() => expect(seen).toEqual([{ mode: 'light', tokens: {} }]));
  });

  it('is not told by the window after the handshake: only the port is the host', async () => {
    const { r, dom, hl } = await view([], {});
    const seen: unknown[] = [];
    hl.on('theme', (t) => seen.push(t));
    await settle(30);
    const forged = new dom.window.Event('message');
    Object.defineProperties(forged, {
      data: {
        value: {
          jsonrpc: '2.0',
          method: 'ui/notifications/host-context-changed',
          params: { theme: 'light', harnessly: { ...context, theme: { mode: 'light', tokens: {} } } },
        },
      },
      origin: { value: SLOT },
      source: { value: r.hostWindow },
    });
    dom.window.dispatchEvent(forged);
    await settle(30);
    expect(seen).toEqual([]);
  });

  it('throws host_unavailable outside a frame', async () => {
    vi.stubGlobal('window', {});
    vi.stubGlobal('document', {});
    // window.parent is undefined: not in a frame.
    await expect(connect()).rejects.toMatchObject({ code: 'host_unavailable' });
    const self: Record<string, unknown> = {};
    self.parent = self;
    vi.stubGlobal('window', self);
    await expect(connect()).rejects.toMatchObject({ code: 'host_unavailable' });
    vi.unstubAllGlobals();
  });
  it('runs an agent’s call once, however many times its id comes, and answers the same again (HRN-1117)', async () => {
    const answers: { id: string; answer: unknown }[] = [];
    const { r, hl } = await view(['tools.page'], {
      pageTools: () => undefined,
      pageToolResult: (id, answer) => void answers.push({ id, answer }),
    });
    const tools = (
      hl as unknown as {
        tools: { register: (t: object, run: (a: { n: number }) => Promise<unknown>) => Promise<void> };
      }
    ).tools;
    let ran = 0;
    let finish: (v: unknown) => void = () => undefined;
    await tools.register({ name: 'add', description: 'adds a row' }, () => {
      ran += 1;
      return new Promise((done) => (finish = done));
    });
    const call = (id: string, name = 'add') =>
      r.bridge.notify('harnessly/notifications/tool-call', { id, name, arguments: { n: 1 } });

    // The same command comes twice while it runs: it is not started again.
    call('cmd-1');
    await vi.waitFor(() => expect(ran).toBe(1));
    call('cmd-1');
    await settle();
    expect(ran).toBe(1);
    finish({ rows: 1 });
    await vi.waitFor(() => expect(answers).toEqual([{ id: 'cmd-1', answer: { result: { rows: 1 } } }]));

    // It comes again after its answer (the host did not hear it): the same answer goes, the tool does not run.
    call('cmd-1');
    await vi.waitFor(() => expect(answers).toHaveLength(2));
    expect(answers[1]).toEqual({ id: 'cmd-1', answer: { result: { rows: 1 } } });
    expect(ran).toBe(1);

    // Another command runs; a call of a tool the view does not offer is refused and not remembered.
    call('cmd-2');
    await vi.waitFor(() => expect(ran).toBe(2));
    finish({ rows: 2 });
    await vi.waitFor(() => expect(answers).toHaveLength(3));
    call('cmd-3', 'later');
    await vi.waitFor(() => expect(answers).toHaveLength(4));
    expect(answers[3]).toEqual({ id: 'cmd-3', answer: { error: 'the view offers no tool later' } });
    await tools.register({ name: 'later', description: 'comes later' }, async () => ({ ok: true }));
    call('cmd-3', 'later');
    await vi.waitFor(() => expect(answers).toHaveLength(5));
    expect(answers[4]).toEqual({ id: 'cmd-3', answer: { result: { ok: true } } });
  });
});
