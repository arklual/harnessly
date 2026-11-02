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
}

const view = async (permissions: string[], handlers: IBridgeHandlers) => {
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
  const harnessly = (dom.window as unknown as { harnessly: { connect: () => Promise<IHl> } }).harnessly;
  const hl = await harnessly.connect();
  return { r, dom, hl };
};

describe('@harnessly/app-sdk against the bridge, through the shim', () => {
  it('shakes hands, takes the theme and calls what the manifest grants', async () => {
    const storage = vi.fn(async () => 'stored');
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
});
