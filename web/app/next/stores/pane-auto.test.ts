import { describe, expect, it } from 'vitest';
import { ALL_PANE_AUTO_KINDS, formatPaneAuto, parsePaneAuto } from '@/next/stores/pane-auto';

describe('pane auto-open switches', () => {
  it('reads the old values: absent is on, 0 is off', () => {
    expect(parsePaneAuto(null)).toEqual({ on: true, kinds: ALL_PANE_AUTO_KINDS });
    expect(parsePaneAuto('0').on).toBe(false);
    expect(parsePaneAuto('{broken').on).toBe(true);
  });

  it('keeps the kinds in the same key, so Legacy still reads it as on', () => {
    const value = { on: true, kinds: { ...ALL_PANE_AUTO_KINDS, files: false } };
    const raw = formatPaneAuto(value);
    expect(raw).not.toBe('0');
    expect(parsePaneAuto(raw)).toEqual(value);
    expect(formatPaneAuto({ on: true, kinds: ALL_PANE_AUTO_KINDS })).toBeNull();
    expect(formatPaneAuto({ on: false, kinds: value.kinds })).toBe('0');
  });
});
