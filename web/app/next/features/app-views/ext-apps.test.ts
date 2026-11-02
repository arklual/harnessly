// Two views of the public MCP Apps SDK (modelcontextprotocol/ext-apps v2.0.4, see fixtures/ext-apps/README.md) run as they
// are in a frame of the page, against the real bridge, with the view server's shim between them: no Harnessly code in
// the views, and the host without a line for them.
import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers } from '@/next/features/app-views/bridge';
import { FrameRig, NONCE, settle, SLOT, servePage } from '@/next/features/app-views/frame-rig';

const read = (rel: string) => readFileSync(new URL(`fixtures/ext-apps/${rel}`, import.meta.url), 'utf8');

const rig = (handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  r.bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.foreign', version: '1', permissions: ['chat.prompt'], targets: ['pane'] },
    context: {
      app: { id: 'acme.foreign', version: '1' },
      target: 'pane',
      view: 'main',
      theme: { mode: 'dark', tokens: {} },
      locale: 'en',
      chat: { id: 'c1' },
      user: null,
    },
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  return r;
};

const time = {
  content: [{ type: 'text', text: '2026-10-31T12:00:00Z' }],
  structuredContent: { time: '2026-10-31T12:00:00Z' },
};

afterEach(() => vi.useRealTimers());

describe('foreign MCP Apps views through the shim', () => {
  it('quickstart (Get Time App) handshakes and calls its tool unchanged', async () => {
    const tool = vi.fn(async () => time);
    const r = rig({ tool });
    const dom = r.load(read('quickstart/mcp-app.html'), NONCE, [read('quickstart/mcp-app.js')]);
    await settle(80);
    const out = dom.window.document.getElementById('server-time')!;
    dom.window.document.getElementById('get-time-btn')!.dispatchEvent(new dom.window.Event('click'));
    await vi.waitFor(() => expect(out.textContent).toBe('2026-10-31T12:00:00Z'));
    expect(tool).toHaveBeenCalledWith('get-time', {});
    // Only the handshake went through the window; every call after it went on the port.
    expect((r.onWindow as { method?: string }[]).every((m) => m.method === 'ui/initialize')).toBe(true);
    expect(r.onWindow.length).toBeGreaterThan(0);
  });

  it('basic-server-vanillajs handshakes, takes the host theme and calls its tool unchanged', async () => {
    const tool = vi.fn(async () => time);
    const r = rig({ tool });
    const dom = r.load(read('basic-server-vanillajs/mcp-app.html'), NONCE, [
      read('basic-server-vanillajs/mcp-app.js'),
    ]);
    await settle(80);
    const out = dom.window.document.getElementById('server-time')!;
    dom.window.document.getElementById('get-time-btn')!.dispatchEvent(new dom.window.Event('click'));
    await vi.waitFor(() => expect(out.textContent).toBe('2026-10-31T12:00:00Z'));
    expect(tool).toHaveBeenCalledWith('get-time', {});
    expect(dom.window.document.documentElement.dataset.theme).toBe('dark');
  });

  it('a navigated document, which the view server wrote no nonce into, is refused and gets no port', async () => {
    const tool = vi.fn(async () => time);
    const r = rig({ tool });
    // The frame navigated before the mounted document shook hands: the page it shows has no nonce.
    const other = r.load('<html><head></head><body></body></html>', null);
    const reply = vi.fn();
    other.window.addEventListener('message', (e: Event) => reply((e as MessageEvent).data));
    (other.window.parent as unknown as { postMessage: (m: unknown) => void }).postMessage({
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: { name: 'get-time' },
    });
    await settle(100);
    expect(tool).not.toHaveBeenCalled();
    expect(reply).toHaveBeenCalledWith(expect.objectContaining({ id: 1, error: expect.anything() }));
    // And a plain call of the view on the window, as an unshimmed page would make it, is not heard either.
    expect(
      r.bridge.onMessage({
        source: r.proxy,
        origin: SLOT,
        data: { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'get-time' } },
      }),
    ).toBe(true);
    await settle(30);
    expect(tool).not.toHaveBeenCalled();
  });

  it('a navigated document that has the nonce still gets no port once the first took it', async () => {
    const tool = vi.fn(async () => time);
    const r = rig({ tool });
    r.load(read('quickstart/mcp-app.html'), NONCE, [read('quickstart/mcp-app.js')]);
    await settle(80);
    // A page of the same view (a link inside it) carries the same nonce, but the bridge hears the window no more.
    const second = r.load(servePage('<html><head></head><body></body></html>', NONCE), NONCE);
    const reply = vi.fn();
    second.window.addEventListener('message', (e: Event) => reply((e as MessageEvent).data));
    (second.window.parent as unknown as { postMessage: (m: unknown) => void }).postMessage({
      jsonrpc: '2.0',
      id: 7,
      method: 'tools/call',
      params: { name: 'get-time' },
    });
    await settle(300);
    expect(tool).not.toHaveBeenCalled();
    expect(reply).not.toHaveBeenCalled();
  });
});
