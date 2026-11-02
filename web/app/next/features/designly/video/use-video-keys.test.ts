// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { digitOf, videoHandlers } from '@/next/features/designly/video/use-video-keys';
import { KEY_COMMANDS } from '@/next/features/keys/commands';

describe('video keys', () => {
  it('answers every action of the video in the list', () => {
    const handlers = videoHandlers(() => undefined);
    for (const c of KEY_COMMANDS.filter((x) => x.scope === 'video'))
      expect(handlers[c.id]).toBeTypeOf('function');
  });

  it('runs an action with its key, and a digit with its tenth', () => {
    const onKey = vi.fn();
    const handlers = videoHandlers(onKey);
    handlers['video.toggle']?.('Space');
    handlers['video.tenth']?.('Digit7');
    expect(onKey).toHaveBeenNthCalledWith(1, { key: 'toggle' });
    expect(onKey).toHaveBeenNthCalledWith(2, { key: 'tenth', n: 7 });
  });

  it('changes scene from the global step keys', () => {
    const onKey = vi.fn();
    const handlers = videoHandlers(onKey);
    handlers['global.stepOn']?.('Alt+ArrowRight');
    handlers['global.stepBack']?.('Alt+ArrowLeft');
    expect(onKey).toHaveBeenNthCalledWith(1, { key: 'nextScene' });
    expect(onKey).toHaveBeenNthCalledWith(2, { key: 'prevScene' });
  });

  it('reads a digit from its place, on either keypad', () => {
    expect(digitOf('Digit0')).toBe(0);
    expect(digitOf('Numpad9')).toBe(9);
    expect(digitOf('KeyA')).toBeNull();
  });
});
