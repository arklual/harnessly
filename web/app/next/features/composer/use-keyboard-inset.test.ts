import { describe, expect, it } from 'vitest';
import { keyboardInset, type ViewportSample } from '@/next/features/composer/use-keyboard-inset';

const sample = (over: Partial<ViewportSample> = {}): ViewportSample => ({
  innerHeight: 800,
  height: 800,
  offsetTop: 0,
  scale: 1,
  editing: true,
  ...over,
});

describe('keyboardInset', () => {
  it('lifts by the part of the layout viewport an on-screen keyboard covers', () => {
    expect(keyboardInset(sample({ height: 460 }))).toBe(340);
    expect(keyboardInset(sample({ height: 460, offsetTop: 40 }))).toBe(300);
  });

  it('stays at the bottom without a keyboard, including browser zoom (Ctrl/Cmd +)', () => {
    expect(keyboardInset(sample())).toBe(0);
    // Browser zoom resizes the layout and the visual viewport together; scale stays 1.
    expect(keyboardInset(sample({ innerHeight: 533, height: 533 }))).toBe(0);
  });

  it('ignores a pinch-zoomed page', () => {
    expect(keyboardInset(sample({ height: 400, offsetTop: 120, scale: 2 }))).toBe(0);
    expect(keyboardInset(sample({ height: 400, scale: 2, editing: false }))).toBe(0);
  });

  it('ignores the gap while no field is being edited', () => {
    expect(keyboardInset(sample({ height: 460, editing: false }))).toBe(0);
  });

  it('never goes negative', () => {
    expect(keyboardInset(sample({ height: 820 }))).toBe(0);
  });
});
