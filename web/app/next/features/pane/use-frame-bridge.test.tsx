// @vitest-environment jsdom
// The browser tab hears a page's ask to be loaded again (`tk:reload`): a Sinopia board in a slot by path, on an opaque
// origin, cannot reload itself past the proxy in front of the platform, so the tab reloads its frame.
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { asksReload, useFrameBridge } from '@/next/features/pane/use-frame-bridge';

const ORIGIN = 'https://hub.example.com/m/abc/__pv/4-0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a';

const setup = () => {
  const win = { postMessage: vi.fn() } as unknown as Window;
  const iframe = { contentWindow: win } as HTMLIFrameElement;
  const onReload = vi.fn();
  renderHook(() =>
    useFrameBridge({
      frame: { current: iframe },
      origin: ORIGIN,
      onOpen: () => undefined,
      onRedirect: () => false,
      onFailed: () => undefined,
      onPicked: () => undefined,
      onReload,
    }),
  );
  const say = (data: unknown, origin: string, source: unknown = win) =>
    act(() => {
      window.dispatchEvent(
        new MessageEvent('message', { data, origin, source: source as MessageEventSource }),
      );
    });
  return { say, onReload };
};

describe('the browser tab and a page that asks to reload', () => {
  it('reads the ask and nothing else', () => {
    expect(asksReload({ type: 'tk:reload' })).toBe(true);
    expect(asksReload({ type: 'tk:hello' })).toBe(false);
    expect(asksReload('tk:reload')).toBe(false);
    expect(asksReload(null)).toBe(false);
  });

  it('reloads for its frame on an opaque origin (a slot by path)', () => {
    const { say, onReload } = setup();
    say({ type: 'tk:reload' }, 'null');
    expect(onReload).toHaveBeenCalledTimes(1);
  });

  it('does not reload for another window', () => {
    const { say, onReload } = setup();
    say({ type: 'tk:reload' }, 'null', {});
    expect(onReload).not.toHaveBeenCalled();
  });
});
