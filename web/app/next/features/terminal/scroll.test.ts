import { describe, expect, it } from 'vitest';
import { isTyped, scrolledByTitle, swipeWheel, wheelAllowed } from '@/next/features/terminal/scroll';

describe('terminal scrolling (HRN-1453)', () => {
  it('reads the scrolled-back state from tmux title', () => {
    expect(scrolledByTitle('hx-scroll:1')).toBe(true);
    expect(scrolledByTitle('hx-scroll:0')).toBe(false);
    expect(scrolledByTitle('claude|/home')).toBeNull();
  });

  it('tells typed keys from mouse and focus reports', () => {
    expect(isTyped('a')).toBe(true);
    expect(isTyped('\x1b[A')).toBe(true);
    expect(isTyped('\r')).toBe(true);
    expect(isTyped('\x1b[<64;10;5M')).toBe(false);
    expect(isTyped('\x1b[M`!!')).toBe(false);
    expect(isTyped('\x1b[I')).toBe(false);
  });

  it('never lets the wheel become arrows on a screen without scrollback and mouse', () => {
    expect(wheelAllowed('alternate', 'none')).toBe(false);
    expect(wheelAllowed('alternate', 'drag')).toBe(true);
    expect(wheelAllowed('normal', 'none')).toBe(true);
  });

  it('turns a swipe into wheel reports, a finger down scrolling back', () => {
    expect(swipeWheel(45, 20)).toEqual({ reports: '\x1b[<64;1;1M'.repeat(2), used: 40 });
    expect(swipeWheel(-25, 20)).toEqual({ reports: '\x1b[<65;1;1M', used: -20 });
    expect(swipeWheel(10, 20).reports).toBe('');
    expect(swipeWheel(10, 0).reports).toBe('');
  });
});
