// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  DOT_TONES,
  dotTone,
  readRecent,
  RECENT_MAX,
  rememberRecent,
} from '@/next/features/designly/ds-recent';

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('dotTone', () => {
  it('leaves the systems the stylesheet names to their own colours', () => {
    expect(dotTone('shop')).toBeNull();
    expect(dotTone('none')).toBeNull();
  });

  it('gives an unknown id the same tone every time, inside the palette', () => {
    const ids = ['brand-2', 'ds_x', 'Лендинги', 'a', 'b', 'c', 'd', 'e', 'f', 'g'];
    for (const id of ids) {
      const tone = dotTone(id);
      expect(tone).not.toBeNull();
      expect(tone).toBe(dotTone(id));
      expect(tone).toBeGreaterThanOrEqual(0);
      expect(tone).toBeLessThan(DOT_TONES);
    }
    expect(new Set(ids.map(dotTone)).size).toBeGreaterThan(2);
  });
});

describe('recent picks', () => {
  it('keeps the newest first, without repeats, up to the limit', () => {
    let list: string[] = [];
    for (const id of ['a', 'b', 'c', 'a', 'd']) list = rememberRecent(list, id);
    expect(list).toEqual(['d', 'a', 'c']);
    expect(list).toHaveLength(RECENT_MAX);
    expect(readRecent()).toEqual(['d', 'a', 'c']);
  });

  it('reads nothing from a broken record', () => {
    localStorage.setItem('hx-next-dl-ds-recent', '{"a":1}');
    expect(readRecent()).toEqual([]);
    localStorage.setItem('hx-next-dl-ds-recent', 'not json');
    expect(readRecent()).toEqual([]);
  });

  it('works where the browser keeps nothing', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('full');
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(readRecent()).toEqual([]);
    expect(rememberRecent(['a'], 'b')).toEqual(['b', 'a']);
  });
});
