import { describe, expect, it } from 'vitest';
import { keptScreens, keptScreensMax, MAX_KEPT_SCREENS } from '@/next/features/shell/kept-screens';

const tabs = (...paths: string[]) => paths.map((path, i) => ({ id: `t${i + 1}`, path }));
const isDesign = (path: string) => path.startsWith('/designly/');

describe('kept screens', () => {
  it('keeps the active design first, then the ones before it, most recent first', () => {
    const list = tabs('/designly/a', '/c/tab-x', '/designly/b');
    const one = keptScreens([], 't1', list, isDesign);
    expect(one).toEqual(['t1']);
    expect(keptScreens(one, 't3', list, isDesign)).toEqual(['t3', 't1']);
  });

  it('a chat in front keeps the designs as they were, and the same list comes back unchanged', () => {
    const before = ['t3', 't1'];
    expect(keptScreens(before, 't2', tabs('/designly/a', '/c/tab-x', '/designly/b'), isDesign)).toBe(before);
  });

  it('lets go of closed tabs, tabs that left their design, and the least recent beyond the cap', () => {
    const list = tabs('/designly/a', '/c/tab-x');
    expect(keptScreens(['t3', 't2', 't1'], 't1', list, isDesign)).toEqual(['t1']);
    const many = tabs(...Array.from({ length: MAX_KEPT_SCREENS + 2 }, (_, i) => `/designly/d${i}`));
    const all = many.reduce<readonly string[]>((kept, tab) => keptScreens(kept, tab.id, many, isDesign), []);
    expect(all).toHaveLength(MAX_KEPT_SCREENS);
    expect(all[0]).toBe(`t${MAX_KEPT_SCREENS + 2}`);
    expect(all).not.toContain('t1');
  });

  it('keeps fewer on a device with little memory', () => {
    expect(keptScreensMax(undefined)).toBe(MAX_KEPT_SCREENS);
    expect(keptScreensMax(8)).toBe(MAX_KEPT_SCREENS);
    expect(keptScreensMax(4)).toBe(3);
    expect(keptScreensMax(2)).toBe(2);
    expect(keptScreensMax(0.5)).toBe(2);
  });
});
