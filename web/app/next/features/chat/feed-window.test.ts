import { describe, expect, it } from 'vitest';
import {
  bottomWindow,
  coversWindow,
  ESTIMATED_TURN_PX,
  feedWindow,
  OVERSCAN,
  RESERVE,
  sameWindow,
  VIRTUAL_FROM,
} from '@/next/features/chat/feed-window';

const keys = (n: number) => Array.from({ length: n }, (_, index) => `t${index}`);
const none = new Map<string, number>();

describe('feedWindow', () => {
  it('draws everything below the threshold', () => {
    expect(feedWindow(keys(VIRTUAL_FROM - 1), none, 5000, 800)).toMatchObject({
      start: 0,
      end: VIRTUAL_FROM - 1,
      top: 0,
      bottom: 0,
    });
  });

  it('draws the viewport ±40 turns, with spacers that add up to the whole height', () => {
    const list = keys(5000);
    const scrollTop = 2000 * ESTIMATED_TURN_PX;
    const win = feedWindow(list, none, scrollTop, 800);
    const inView = Math.ceil(800 / ESTIMATED_TURN_PX);
    expect(win.start).toBe(2000 - OVERSCAN);
    expect(win.end).toBeGreaterThanOrEqual(2000 + inView + OVERSCAN);
    expect(win.end - win.start).toBeLessThan(inView + OVERSCAN * 2 + 3);
    const drawn = (win.end - win.start) * ESTIMATED_TURN_PX;
    expect(win.top + drawn + win.bottom).toBe(list.length * ESTIMATED_TURN_PX);
  });

  it('uses measured heights over the estimate', () => {
    const list = keys(400);
    const heights = new Map(list.map((key) => [key, 40]));
    const win = feedWindow(list, heights, 200 * 40, 400);
    expect(win.start).toBe(200 - OVERSCAN);
    expect(win.top).toBe((200 - OVERSCAN) * 40);
  });

  it('opens at the bottom', () => {
    const list = keys(1000);
    const win = bottomWindow(list, none);
    expect(win.end).toBe(1000);
    expect(win.bottom).toBe(0);
    expect(win.top).toBe(win.start * ESTIMATED_TURN_PX);
    expect(sameWindow(win, { ...win, top: win.top + 0.5 })).toBe(true);
    expect(sameWindow(win, { ...win, start: win.start + 1 })).toBe(false);
  });

  it('keeps the drawn range until the viewport comes within RESERVE turns of its edge', () => {
    const list = keys(5000);
    const at = (index: number) => feedWindow(list, none, index * ESTIMATED_TURN_PX, 800);
    const drawn = at(2000);
    expect(coversWindow(drawn.start, drawn.end, at(2000 - (OVERSCAN - RESERVE)), list.length)).toBe(true);
    expect(coversWindow(drawn.start, drawn.end, at(2000 - (OVERSCAN - RESERVE) - 1), list.length)).toBe(
      false,
    );
    expect(coversWindow(0, 100, at(0), list.length)).toBe(true);
  });
});
