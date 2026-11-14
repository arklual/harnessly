// @vitest-environment jsdom
// The comment row of a feed annotation stays inside the feed's shown part: the right pane open beside it, the field
// under it (screenshot of 14.11: «Что здесь не так» ran under the pane).
import { describe, expect, it } from 'vitest';
import { rowPlace } from '@/next/features/chat/selbar';

/** A feed from x 64 to 1180 (the pane is to the right of it), from y 60 to the dock at 700. */
const box = { left: 64, right: 1180, top: 60, bottom: 700 };

describe('the comment row of a feed annotation', () => {
  it('slides left to end inside the feed when the words end at its right edge', () => {
    const x = 1170;
    const place = rowPlace(x, 300, box, 120);
    const left = x - 14 + place.dx;
    expect(place.width).toBe(320);
    expect(left + place.width).toBeLessThanOrEqual(box.right - 8);
    expect(left).toBeGreaterThanOrEqual(box.left + 8);
    expect(place.up).toBe(false);
  });

  it('opens over its dot when the words are near the field', () => {
    expect(rowPlace(400, 640, box, 120).up).toBe(true);
    // A row taller than the room under it, with less room over it, stays under.
    expect(rowPlace(400, 120, { ...box, bottom: 200 }, 120).up).toBe(false);
  });

  it('a feed narrower than the row gets a row as wide as the feed, inside it', () => {
    const narrow = { left: 0, right: 300, top: 0, bottom: 800 };
    const place = rowPlace(290, 100, narrow, 120);
    expect(place.width).toBe(284);
    expect(290 - 14 + place.dx).toBe(8);
  });
});
