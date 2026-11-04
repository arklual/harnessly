// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useBrowserSocket } from './agent-browser-socket';
class Socket {
  static OPEN = 1;
  static all: Socket[] = [];
  readyState = 1;
  onmessage: ((e: { data: string | ArrayBuffer }) => void) | null = null;
  onclose: ((e: { code: number }) => void) | null = null;
  close = vi.fn();
  send = vi.fn();
  constructor() {
    Socket.all.push(this);
  }
}
const frame = () => {
  const bytes = new ArrayBuffer(13),
    view = new DataView(bytes);
  view.setUint32(0, 800);
  view.setUint32(4, 600);
  view.setUint32(8, 1);
  return bytes;
};
const tabs = {
  t: 'tabs',
  tabs: [{ id: 'old', owner: 'this' }],
  current: 'old',
  agentTab: 'old',
  following: true,
  gen: 1,
};
beforeEach(() => {
  Socket.all = [];
  vi.stubGlobal('WebSocket', Socket);
  vi.useFakeTimers();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
it('pauses input and discards a decoded old-profile frame until a fresh socket is opened', async () => {
  let finish: (image: ImageBitmap) => void = () => undefined;
  vi.stubGlobal(
    'createImageBitmap',
    vi.fn(
      () =>
        new Promise<ImageBitmap>((resolve) => {
          finish = resolve;
        }),
    ),
  );
  const { result } = renderHook(() => useBrowserSocket('ws://example.test/browser'));
  const old = Socket.all[0]!;
  act(() => {
    old.onmessage?.({ data: JSON.stringify(tabs) });
    old.onmessage?.({ data: frame() });
  });
  act(() => result.current.pause());
  expect(result.current.tabs.tabs).toHaveLength(0);
  const close = vi.fn();
  await act(async () => finish({ width: 800, height: 600, close } as unknown as ImageBitmap));
  expect(close).toHaveBeenCalledOnce();
  expect(result.current.framed).toBe(false);
  act(() => {
    result.current.send({ t: 'text', text: 'private' });
    vi.advanceTimersByTime(5000);
  });
  expect(old.send).not.toHaveBeenCalled();
  expect(Socket.all).toHaveLength(1);
  act(() => result.current.resume());
  expect(Socket.all).toHaveLength(2);
  act(() => old.onmessage?.({ data: JSON.stringify(tabs) }));
  expect(result.current.tabs.tabs).toHaveLength(0);
});
it('clears frames, page dimensions and tabs on a 1012 profile restart and reconnects immediately', () => {
  const { result } = renderHook(() => useBrowserSocket('ws://example.test/browser'));
  const old = Socket.all[0]!;
  const canvas = document.createElement('canvas');
  canvas.width = 800;
  canvas.height = 600;
  result.current.canvas.current = canvas;
  result.current.page.current = { w: 800, h: 600 };
  act(() => old.onmessage?.({ data: JSON.stringify(tabs) }));
  act(() => old.onclose?.({ code: 1012 }));
  expect(result.current.tabs.tabs).toHaveLength(0);
  expect(result.current.page.current).toEqual({ w: 0, h: 0 });
  expect(canvas.width).toBe(0);
  expect(canvas.height).toBe(0);
  act(() => vi.advanceTimersByTime(0));
  expect(Socket.all).toHaveLength(2);
});
