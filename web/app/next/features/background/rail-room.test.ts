import { describe, expect, it } from 'vitest';
import { railFits } from '@/next/features/background/rail-room';

const base = { feedMax: 760, feedPad: 32, rail: 320 };

describe('railFits', () => {
  it('fits on a wide stage and not on a narrow one', () => {
    expect(railFits({ ...base, stage: 1600, pane: 0 })).toBe(true);
    expect(railFits({ ...base, stage: 1200, pane: 0 })).toBe(false);
  });

  it('is exactly the chat column, the gap and the column', () => {
    // The column is centred: (stage - 760) / 2 is free at each side, and the column needs its width, the edge and the gap.
    const need = 760 + 2 * (320 + 16 + 12);
    expect(railFits({ ...base, stage: need, pane: 0 })).toBe(true);
    expect(railFits({ ...base, stage: need - 1, pane: 0 })).toBe(false);
  });

  it('counts what the pane leaves of the stage', () => {
    expect(railFits({ ...base, stage: 1600, pane: 400 })).toBe(false);
    expect(railFits({ ...base, stage: 2400, pane: 400 })).toBe(true);
  });

  it('does not fit when the chat column is squeezed below its widest', () => {
    expect(railFits({ ...base, stage: 700, pane: 0 })).toBe(false);
  });
});
