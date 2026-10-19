import { describe, expect, it } from 'vitest';

import { place } from './position';

const view = { width: 1000, height: 800 };

describe('place', () => {
  it('puts the popup under the anchor, aligned to its start', () => {
    const p = place({ left: 100, top: 100, width: 80, height: 30 }, { width: 200, height: 150 }, view, {
      side: 'bottom',
      align: 'start',
    });
    expect(p).toMatchObject({ left: 100, top: 136, side: 'bottom' });
  });

  it('flips above when there is no room below', () => {
    const p = place({ left: 100, top: 700, width: 80, height: 30 }, { width: 200, height: 150 }, view, {
      side: 'bottom',
      align: 'start',
    });
    expect(p.side).toBe('top');
    expect(p.top).toBe(700 - 6 - 150);
  });

  it('stays inside the viewport along the edge', () => {
    const p = place({ left: 950, top: 100, width: 40, height: 30 }, { width: 200, height: 100 }, view, {
      side: 'bottom',
      align: 'start',
    });
    expect(p.left).toBe(1000 - 8 - 200);
  });

  it('centres a tooltip over its anchor', () => {
    const p = place({ left: 400, top: 300, width: 100, height: 20 }, { width: 60, height: 24 }, view, {
      side: 'top',
      align: 'center',
    });
    expect(p).toMatchObject({ left: 420, side: 'top', top: 300 - 6 - 24 });
  });
});
