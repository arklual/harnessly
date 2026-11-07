import { describe, expect, it, vi } from 'vitest';

import { BridgeError, targetAllowed, type TAppKeyHandlers } from '@/next/features/app-views/bridge';
import { connectBuiltin } from '@/next/features/app-views/builtin-app';

// A builtin app reaches the host through the bridge in the page's own process: the same methods, the rights
// of its manifest, the same refusals.
describe('connectBuiltin', () => {
  it('sends to the chat through ui/message, under the right its manifest has', async () => {
    const chatSend = vi.fn(async () => undefined);
    const onCall = vi.fn();
    const hl = connectBuiltin('harnessly.sinopia', { handlers: { chatSend }, onCall });
    await hl.chat.send('Shorten this');
    expect(chatSend).toHaveBeenCalledWith('Shorten this');
    expect(onCall).toHaveBeenCalledWith(
      expect.objectContaining({ method: 'ui/message', permission: 'chat.prompt', ok: true }),
    );
  });

  it('refuses what the manifest gives no right to, as a frame is refused', async () => {
    const storage = vi.fn(async () => ({ value: 1, version: 1 }));
    const hl = connectBuiltin('harnessly.sinopia', { handlers: { storage } });
    const got = hl.call('harnessly/storage.get', { scope: 'chat', key: 'k' });
    await expect(got).rejects.toBeInstanceOf(BridgeError);
    await expect(got).rejects.toMatchObject({ code: 'permission_denied' });
    expect(storage).not.toHaveBeenCalled();
  });

  it('refuses a method the host does not do here, and an unknown one', async () => {
    const hl = connectBuiltin('harnessly.sinopia', { handlers: {} });
    await expect(hl.chat.send('x')).rejects.toMatchObject({ code: 'host_unavailable' });
    await expect(hl.call('harnessly/nope')).rejects.toBeInstanceOf(BridgeError);
  });

  it('a handler’s refusal comes back to the app', async () => {
    const hl = connectBuiltin('harnessly.sinopia', {
      handlers: {
        chatSend: async () => {
          throw new BridgeError('host_unavailable', 'the document has no chat');
        },
      },
    });
    await expect(hl.chat.send('x')).rejects.toMatchObject({ message: 'the document has no chat' });
  });

  it('offers its page tools in WebMCP form and runs the agent’s call in the page (HRN-808)', async () => {
    const pageTools = vi.fn();
    const pageToolResult = vi.fn();
    const hl = connectBuiltin('harnessly.sinopia', { handlers: { pageTools, pageToolResult } });
    hl.modelContext.registerTool({
      name: 'show_board',
      description: 'Show a board',
      inputSchema: { type: 'object' },
      execute: ({ board }) => ({ shown: board }),
    });
    await vi.waitFor(() =>
      expect(pageTools).toHaveBeenLastCalledWith([
        { name: 'show_board', description: 'Show a board', inputSchema: { type: 'object' } },
      ]),
    );
    hl.notifyView('harnessly/notifications/tool-call', {
      id: 'c1',
      name: 'show_board',
      arguments: { board: 2 },
    });
    await vi.waitFor(() => expect(pageToolResult).toHaveBeenCalledWith('c1', { result: { shown: 2 } }));
    hl.notifyView('harnessly/notifications/tool-call', { id: 'c2', name: 'nope', arguments: {} });
    await vi.waitFor(() =>
      expect(pageToolResult).toHaveBeenCalledWith('c2', { error: 'no tool nope on this page' }),
    );
    hl.modelContext.unregisterTool('show_board');
    await vi.waitFor(() => expect(pageTools).toHaveBeenLastCalledWith([]));
  });

  it('publishes its screen under context.publish and toasts with a tone', async () => {
    const publish = vi.fn();
    const toast = vi.fn();
    const hl = connectBuiltin('harnessly.sinopia', { handlers: { publish, toast } });
    await hl.publish({ board: 1 });
    expect(publish).toHaveBeenCalledWith({ board: 1 });
    await hl.ui.toast('Сохранено', { tone: 'success', description: 'всё' });
    expect(toast).toHaveBeenCalledWith('Сохранено', { tone: 'success', description: 'всё' });
  });

  it('runs a screen’s shortcut commands with its own handlers, until it stops or goes', () => {
    const stop = vi.fn();
    let heard: (() => TAppKeyHandlers) | null = null;
    const keys = vi.fn((_scope: string, handlers: () => TAppKeyHandlers) => {
      heard = handlers;
      return stop;
    });
    const hl = connectBuiltin('harnessly.sinopia', { handlers: { keys } });
    const next = vi.fn();
    const off = hl.keys.on('deck', () => ({ 'deck.next': next }));
    expect(keys).toHaveBeenCalledWith('deck', expect.any(Function));
    heard!()['deck.next']?.('ArrowRight');
    expect(next).toHaveBeenCalledWith('ArrowRight');
    off();
    expect(stop).toHaveBeenCalledTimes(1);
    hl.keys.on('video', () => ({}));
    hl.dispose();
    expect(stop).toHaveBeenCalledTimes(2);
  });

  it('is only for the host’s own apps', () => {
    expect(() => connectBuiltin('acme.other' as never, { handlers: {} })).toThrow();
  });
});

// The older targets of Sinopia are aliases of its screen: sinopia.* still covers every document kind.
describe('targetAllowed with the aliases of the builtin apps', () => {
  it('reads sinopia.* as the wildcard of Sinopia’s aliases', () => {
    expect(targetAllowed(['sinopia.*'], 'sinopia.slides')).toBe(true);
    expect(targetAllowed(['sinopia.slides'], 'sinopia.slides')).toBe(true);
    expect(targetAllowed(['sinopia.site'], 'sinopia.slides')).toBe(false);
    expect(targetAllowed(['sinopia.*'], 'pane')).toBe(false);
    expect(targetAllowed(['pane'], 'pane')).toBe(true);
  });
});
