// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FILE_STATUS, type IReviewFile } from '@/next/features/review/model';
import {
  isViewed,
  markKey,
  parseViewed,
  resetViewedCache,
  setViewed,
  useViewed,
  VIEWED_MAX,
  viewedStorageKey,
} from '@/next/features/review/viewed';

const file = (path: string, version: string): IReviewFile => ({
  path,
  oldPath: path,
  status: FILE_STATUS.modified,
  additions: 1,
  deletions: 1,
  binary: false,
  patch: '',
  version,
});

afterEach(() => {
  window.localStorage.clear();
  resetViewedCache();
});

describe('«viewed» marks', () => {
  it('hold the version they were put on: a changed file is not viewed again', () => {
    const marks = setViewed({}, 'repo', file('a.ts', 'v1'), true);
    expect(isViewed(marks, 'repo', file('a.ts', 'v1'))).toBe(true);
    expect(isViewed(marks, 'repo', file('a.ts', 'v2'))).toBe(false);
  });

  it('are kept per scope: the same path in another repository is not viewed', () => {
    const marks = setViewed({}, 'repo', file('a.ts', 'v1'), true);
    expect(isViewed(marks, 'other', file('a.ts', 'v1'))).toBe(false);
  });

  it('come off when unmarked', () => {
    const marks = setViewed(
      setViewed({}, 'repo', file('a.ts', 'v1'), true),
      'repo',
      file('a.ts', 'v1'),
      false,
    );
    expect(marks).toEqual({});
  });

  it('drop the oldest past the limit', () => {
    const full: Record<string, string> = {};
    for (let i = 0; i < VIEWED_MAX; i++) full[markKey('s', `f${i}`)] = 'v';
    const marks = setViewed(full, 's', file(`f${VIEWED_MAX}`, 'v'), true);
    expect(Object.keys(marks)).toHaveLength(VIEWED_MAX);
    expect(markKey('s', 'f0') in marks).toBe(false);
  });

  it('read broken storage as no marks', () => {
    expect(parseViewed('nope')).toEqual({});
    expect(parseViewed('[1]')).toEqual({});
    expect(parseViewed('{"a":1,"b":"v"}')).toEqual({ b: 'v' });
  });

  it('are stored per person', () => {
    const a = renderHook(() => useViewed('anna@example.com', 'repo'));
    act(() => a.result.current.toggle(file('a.ts', 'v1'), true));
    expect(a.result.current.viewed(file('a.ts', 'v1'))).toBe(true);
    expect(window.localStorage.getItem(viewedStorageKey('anna@example.com'))).toContain('v1');
    const b = renderHook(() => useViewed('bo@example.com', 'repo'));
    expect(b.result.current.viewed(file('a.ts', 'v1'))).toBe(false);
  });
});
