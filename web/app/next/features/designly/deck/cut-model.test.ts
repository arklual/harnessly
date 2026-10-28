import { describe, expect, it } from 'vitest';
import {
  cutRanges,
  partKey,
  segmentAt,
  skipTarget,
  skippedMs,
  slideAt,
  subtitleAt,
  toggleKept,
  trackWeight,
} from './cut-model';
import type { IDeckEdit, IDeckPart, IDeckSegment } from './types';

const edit = (over: Partial<IDeckEdit> = {}): IDeckEdit => ({
  cut: true,
  subs: true,
  kept: [],
  cursor: 'smooth',
  cam: 'br',
  ...over,
});
const part = (
  text: string,
  t0: number,
  t1: number,
  cut = false,
  kind: IDeckPart['kind'] = 'word',
): IDeckPart => ({
  text,
  cut,
  kind,
  t0,
  t1,
});

const SEGMENTS: IDeckSegment[] = [
  {
    t0: 0,
    t1: 5000,
    page: 'a',
    parts: [
      part('Привет!', 0, 1000),
      part('э-э', 1000, 1500, true, 'filler'),
      part('Это вторая часть курса про нейросети.', 1500, 5000),
    ],
  },
  {
    t0: 5000,
    t1: 12_000,
    page: 'b',
    parts: [
      part('Запомните одну формулу.', 5000, 7000),
      part('', 7000, 9100, true, 'pause'),
      part('Модель думает, инструменты дают ей руки.', 9100, 12_000),
    ],
  },
];

describe('cutRanges', () => {
  it('lists the parts the cut takes', () => {
    expect(cutRanges(SEGMENTS, edit())).toEqual([
      { t0: 1000, t1: 1500 },
      { t0: 7000, t1: 9100 },
    ]);
    expect(skippedMs(cutRanges(SEGMENTS, edit()))).toBe(2600);
  });

  it('takes nothing when the cut is off', () => {
    expect(cutRanges(SEGMENTS, edit({ cut: false }))).toEqual([]);
  });

  it('leaves out a part the person brought back', () => {
    const kept = edit({ kept: [partKey(0, 1)] });
    expect(cutRanges(SEGMENTS, kept)).toEqual([{ t0: 7000, t1: 9100 }]);
  });

  it('joins ranges that touch', () => {
    const segs: IDeckSegment[] = [
      {
        t0: 0,
        t1: 3000,
        page: 'a',
        parts: [
          part('э', 0, 1000, true, 'filler'),
          part('', 1000, 2000, true, 'pause'),
          part('да', 2000, 3000),
        ],
      },
    ];
    expect(cutRanges(segs, edit())).toEqual([{ t0: 0, t1: 2000 }]);
  });
});

describe('skipTarget', () => {
  const ranges = cutRanges(SEGMENTS, edit());
  it('jumps over a cut and plays the rest', () => {
    expect(skipTarget(ranges, 1200)).toBe(1500);
    expect(skipTarget(ranges, 1500)).toBeNull();
    expect(skipTarget(ranges, 999)).toBeNull();
    expect(skipTarget(ranges, 7000)).toBe(9100);
  });
});

describe('toggleKept', () => {
  it('adds and removes a key', () => {
    const on = toggleKept(edit(), '0.1');
    expect(on).toEqual(['0.1']);
    expect(toggleKept(edit({ kept: on }), '0.1')).toEqual([]);
  });
});

describe('segmentAt and slideAt', () => {
  it('finds the segment of a moment', () => {
    expect(segmentAt(SEGMENTS, 0)).toBe(0);
    expect(segmentAt(SEGMENTS, 5000)).toBe(1);
    expect(segmentAt(SEGMENTS, 99_000)).toBe(1);
    expect(segmentAt([], 10)).toBe(-1);
  });
  it('picks the newest take that has begun, so a retake wins', () => {
    const takes = [
      { page: 'a', n: 1, startMs: 0, endMs: 4000 },
      { page: 'b', n: 1, startMs: 4000, endMs: 9000 },
      { page: 'b', n: 2, startMs: 9000, endMs: 12_000 },
      { page: 'c', n: 1, startMs: 12_000, endMs: 20_000 },
    ];
    expect(slideAt(takes, 100)).toBe('a');
    expect(slideAt(takes, 9500)).toBe('b');
    expect(slideAt(takes, 13_000)).toBe('c');
    expect(slideAt(takes.slice(1), 100)).toBeNull();
  });
});

describe('subtitleAt', () => {
  it('shows up to nine words, and an ellipsis after them', () => {
    expect(subtitleAt(SEGMENTS, edit(), 2000)).toBe('Привет! Это вторая часть курса про нейросети.');
    expect(subtitleAt(SEGMENTS, edit(), 6000)).toBe(
      'Запомните одну формулу. Модель думает, инструменты дают ей руки.',
    );
    expect(subtitleAt(SEGMENTS, edit(), 6000, 4)).toBe('Запомните одну формулу. Модель…');
  });
  it('leaves out what the cut takes', () => {
    const segs: IDeckSegment[] = [
      { t0: 0, t1: 1000, page: 'a', parts: [part('э-э', 0, 500, true, 'filler'), part('да', 500, 1000)] },
    ];
    expect(subtitleAt(segs, edit(), 10)).toBe('да');
    expect(subtitleAt(segs, edit({ cut: false }), 10)).toBe('э-э да');
  });
  it('is empty before any segment', () => {
    expect(subtitleAt([], edit(), 0)).toBe('');
  });
});

describe('trackWeight', () => {
  it('follows the duration and is never zero', () => {
    expect(trackWeight(SEGMENTS[1]!)).toBe(7000);
    expect(trackWeight({ t0: 5, t1: 5, page: 'x', parts: [] })).toBe(1);
  });
});
