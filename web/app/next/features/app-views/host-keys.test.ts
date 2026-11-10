// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';

import { hostKeys } from '@/next/features/app-views/host-keys';
import { listenKeys } from '@/next/features/keys/use-keys';

// A key a frame of an app passes on (keys.press) runs only the actions of the screens an app may run: never the
// host's global ones, and only while the frame has the focus.
describe('hostKeys', () => {
  const offs: (() => void)[] = [];
  afterEach(() => {
    for (const off of offs.splice(0)) off();
  });

  it('a frame’s press never runs a global action of the host', () => {
    const newChat = vi.fn();
    offs.push(listenKeys('global', () => ({ 'global.newChat': newChat })));
    expect(hostKeys(() => true).keyPress('Mod+Shift+KeyO', false)).toBe(false);
    expect(newChat).not.toHaveBeenCalled();
    // The host's own app (Sinopia's boards) passes on the global keys as the person's.
    expect(hostKeys(() => true, true).keyPress('Mod+Shift+KeyO', false)).toBe(true);
    expect(newChat).toHaveBeenCalledTimes(1);
  });

  it('a frame’s press runs a screen’s action only while the frame has the focus', () => {
    const next = vi.fn();
    offs.push(listenKeys('deck', () => ({ 'deck.next': next })));
    expect(hostKeys(() => false).keyPress('ArrowRight', false)).toBe(false);
    expect(hostKeys(() => true).keyPress('ArrowRight', false)).toBe(true);
    expect(next).toHaveBeenCalledTimes(1);
  });
});
