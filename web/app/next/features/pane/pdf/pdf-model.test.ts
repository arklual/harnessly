import { describe, expect, it } from 'vitest';
import {
  hitsIn,
  hitsOf,
  offsetsOf,
  pageAt,
  stepHit,
  typedPage,
  zoomIn,
  zoomOut,
} from '@/next/features/pane/pdf/pdf-model';

describe('the PDF view', () => {
  it('knows the page under a scroll position from the proportions alone', () => {
    const offsets = offsetsOf([100, 200, 100], 10);
    expect(offsets).toEqual([0, 110, 320, 430]);
    expect(pageAt(offsets, 0)).toBe(0);
    expect(pageAt(offsets, 109)).toBe(0);
    expect(pageAt(offsets, 110)).toBe(1);
    expect(pageAt(offsets, 5000)).toBe(2);
    expect(pageAt(offsetsOf([]), 0)).toBe(0);
  });

  it('keeps the zoom in its limits', () => {
    expect(zoomIn(4)).toBe(4);
    expect(zoomOut(0.5)).toBe(0.5);
    expect(zoomIn(1)).toBe(1.25);
  });

  it('takes only a real page number', () => {
    expect(typedPage(' 3 ', 5)).toBe(2);
    expect(typedPage('0', 5)).toBeNull();
    expect(typedPage('6', 5)).toBeNull();
    expect(typedPage('2.5', 5)).toBeNull();
    expect(typedPage('abc', 5)).toBeNull();
  });

  it('finds the words whatever their case, and ё as е', () => {
    expect(hitsIn('Договор и ДОГОВОР', 'договор')).toEqual([0, 10]);
    expect(hitsIn('ещё', 'еще')).toEqual([0]);
    expect(hitsIn('текст', '  ')).toEqual([]);
  });

  it('numbers the matches through the file, skipping pages not read yet', () => {
    expect(hitsOf(['a a', null, 'a'], 'a')).toEqual([
      { page: 0, nth: 0 },
      { page: 0, nth: 1 },
      { page: 2, nth: 0 },
    ]);
  });

  it('goes round the ends', () => {
    expect(stepHit(2, 3, 1)).toBe(0);
    expect(stepHit(0, 3, -1)).toBe(2);
    expect(stepHit(0, 0, 1)).toBe(0);
  });
});
