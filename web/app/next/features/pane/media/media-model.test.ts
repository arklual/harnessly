import { describe, expect, it } from 'vitest';
import {
  FIRST_FRAME_AT,
  formatTime,
  MEDIA_ACTION,
  mediaActionOf,
  movedFromStart,
  nextRate,
  peaksOf,
  tenthOf,
} from '@/next/features/pane/media/media-model';

describe('the media player', () => {
  it('writes time the way a player does', () => {
    expect(formatTime(7.9)).toBe('0:07');
    expect(formatTime(754)).toBe('12:34');
    expect(formatTime(3723)).toBe('1:02:03');
    expect(formatTime(null)).toBe('–:––');
    expect(formatTime(Number.POSITIVE_INFINITY)).toBe('–:––');
  });

  it('reads keys by their place, so the Russian layout works', () => {
    expect(mediaActionOf('Space')).toBe(MEDIA_ACTION.toggle);
    expect(mediaActionOf('KeyF')).toBe(MEDIA_ACTION.full);
    expect(mediaActionOf('KeyQ')).toBeNull();
    expect(tenthOf('Digit3')).toBe(0.3);
    expect(tenthOf('Numpad0')).toBe(0);
    expect(tenthOf('KeyA')).toBeNull();
  });

  it('does not take the place a video opens at for a moment the person chose', () => {
    expect(movedFromStart(0)).toBe(false);
    expect(movedFromStart(FIRST_FRAME_AT)).toBe(false);
    expect(movedFromStart(0.04)).toBe(true);
  });

  it('cycles the speed through all its values', () => {
    expect(nextRate(1)).toBe(1.25);
    expect(nextRate(2)).toBe(0.5);
    expect(nextRate(0.5)).toBe(1);
  });

  it('scales the wave to the loudest bar and survives silence', () => {
    expect(peaksOf(new Float32Array([0.1, -0.5, 0.25, 0.25]), 2)).toEqual([1, 0.5]);
    expect(peaksOf(new Float32Array(4), 2)).toEqual([0, 0]);
  });
});
