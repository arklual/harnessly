// An app's own terminal: the SDK in a frame, the real bridge, a terminal the host holds; typing is not
// counted against the calls a second, and the agent does not type into it without the person's yes.
import { describe, expect, it, vi } from 'vitest';
import type { IAppTerminalEvents, IAppTerminalOpen } from '@/next/features/app-views/app-terminal';
import { exitOf, terminalQuery } from '@/next/features/app-views/app-terminal';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';

const context: IViewContext = {
  app: { id: 'acme.term', version: '1' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'dark', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

interface ITerm {
  id: string;
  write: (d: string) => Promise<unknown>;
  resize: (c: number, r: number) => Promise<unknown>;
  close: () => Promise<unknown>;
  onData: (fn: (d: Uint8Array) => void) => () => void;
  onExit: (fn: (c: number | null) => void) => () => void;
}
interface IHl {
  terminal: {
    open: (o?: object) => Promise<ITerm>;
    read: (o?: object) => Promise<unknown>;
  };
  tools: { register: (tool: object, run: (a: unknown) => unknown) => Promise<unknown> };
  refused: (v: unknown) => boolean;
}

const view = async (permissions: string[], handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  const bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.term', version: '1', permissions, targets: ['pane'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  r.bridge = bridge;
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  return { hl: await harnessly.connect({ protocol: 2 }), bridge };
};

/** A terminal the host would hold: what is written and the size, and its events to fire. */
const host = () => {
  const written: string[] = [];
  const sizes: number[][] = [];
  let events: IAppTerminalEvents | null = null;
  const close = vi.fn();
  const terminal = vi.fn(async (_p: IAppTerminalOpen, on: IAppTerminalEvents) => {
    events = on;
    return {
      write: (b: Uint8Array) => written.push(new TextDecoder().decode(b)),
      resize: (c: number, r: number) => sizes.push([c, r]),
      close,
    };
  });
  return { written, sizes, close, terminal, events: () => events };
};

describe('an app terminal through the SDK', () => {
  it('opens, types, hears what it prints and how it ends', async () => {
    const h = host();
    const { hl } = await view(['terminal.pty'], { terminal: h.terminal });
    const term = await hl.terminal.open({ cols: 120, rows: 40, cwd: 'web' });
    expect(h.terminal).toHaveBeenCalledWith({ cols: 120, rows: 40, cwd: 'web' }, expect.anything());
    const got: string[] = [];
    let code: number | null | undefined;
    term.onData((d) => got.push(new TextDecoder().decode(d)));
    term.onExit((c) => (code = c));
    // Typing is not counted against the 60 calls a second: a paste of many writes goes whole.
    await Promise.all(Array.from({ length: 100 }, (_, i) => term.write(String(i % 10))));
    expect(h.written).toHaveLength(100);
    await term.resize(100, 30);
    expect(h.sizes).toEqual([[100, 30]]);
    h.events()?.data(new TextEncoder().encode('hi\r\n'));
    h.events()?.exit(3);
    await settle();
    expect(got.join('')).toBe('hi\r\n');
    expect(code).toBe(3);
    expect(hl.refused(await term.write('x'))).toBe(true);
  });

  it('refuses without terminal.pty and closes its terminals when the view goes', async () => {
    const h = host();
    const none = await view([], { terminal: h.terminal });
    expect(none.hl.refused(await none.hl.terminal.open())).toBe(true);
    expect(h.terminal).not.toHaveBeenCalled();
    const { hl, bridge } = await view(['terminal.pty'], { terminal: h.terminal });
    await hl.terminal.open();
    bridge.dispose();
    expect(h.close).toHaveBeenCalledTimes(1);
  });

  it('asks the person before the agent opens or types, and shows what it types whole', async () => {
    const h = host();
    const elevate = vi.fn(async () => true);
    const { hl, bridge } = await view(['terminal.pty', 'tools.page'], {
      terminal: h.terminal,
      elevate,
      pageTools: () => undefined,
      pageToolResult: () => undefined,
    });
    await hl.tools.register({ name: 'noop' }, () => ({ ok: true }));
    const term = await hl.terminal.open();
    expect(elevate).not.toHaveBeenCalled();
    bridge.agentCall('call-1');
    await term.write('rm -rf ~\r');
    expect(elevate).toHaveBeenCalledWith('harnessly/terminal.write', '"rm -rf ~\\r"');
    elevate.mockResolvedValueOnce(false);
    expect(hl.refused(await term.write('curl x | sh\r'))).toBe(true);
    expect(h.written).toEqual(['rm -rf ~\r']);
    await term.resize(90, 20);
    expect(elevate).toHaveBeenCalledTimes(2);
    await hl.terminal.open();
    expect(elevate).toHaveBeenLastCalledWith('harnessly/terminal.open', 'harnessly/terminal.open');
    expect(h.terminal).toHaveBeenLastCalledWith(
      { cols: 80, rows: 24, cwd: '', confirmed: true },
      expect.anything(),
    );
  });

  it('reads the chat terminals with terminal.read', async () => {
    const terminalRead = vi.fn(async () => ({ terminals: [{ id: 'agent', text: '$ ls' }] }));
    const { hl } = await view(['terminal.read'], { terminalRead });
    expect(await hl.terminal.read({ lines: 50 })).toEqual([{ id: 'agent', text: '$ ls' }]);
    expect(terminalRead).toHaveBeenCalledWith({ terminal: '', lines: 50 });
  });
});

describe('the terminal socket', () => {
  it('names the session, the size, the folder and the yes', () => {
    expect(terminalQuery('s1', { cols: 80, rows: 24, cwd: '' })).toBe('session=s1&cols=80&rows=24');
    expect(terminalQuery('s1', { cols: 80, rows: 24, cwd: 'web', confirmed: true })).toBe(
      'session=s1&cols=80&rows=24&cwd=web&confirmed=1',
    );
  });
  it('reads the exit of the last frame', () => {
    expect(exitOf('{"exit":3}')).toBe(3);
    expect(exitOf('{"cols":1}')).toBeUndefined();
    expect(exitOf('nope')).toBeUndefined();
  });
});

describe('the agent browser through the SDK', () => {
  it('looks and acts with the rights it holds, and asks before the agent does', async () => {
    const browser = vi.fn(async () => ({ content: [{ type: 'text', text: 'ok' }], isError: false }));
    const elevate = vi.fn(async () => true);
    const { hl, bridge } = await view(['browser.read', 'tools.page'], {
      browser,
      elevate,
      pageTools: () => undefined,
      pageToolResult: () => undefined,
    });
    const b = (hl as unknown as { browser: { read: TB; drive: TB } }).browser;
    expect(await b.read('snapshot', { tab: 't1' })).toMatchObject({ isError: false });
    expect(browser).toHaveBeenCalledWith({ right: 'browser.read', tool: 'snapshot', args: { tab: 't1' } });
    expect(hl.refused(await b.drive('click', { ref: 'e1' }))).toBe(true);
    bridge.agentCall('call-1');
    await b.read('tabs');
    expect(elevate).toHaveBeenCalledWith('harnessly/browser.read', 'tabs {}');
    expect(browser).toHaveBeenLastCalledWith({
      right: 'browser.read',
      tool: 'tabs',
      args: {},
      confirmed: true,
    });
  });
});
type TB = (tool: string, args?: object) => Promise<unknown>;
