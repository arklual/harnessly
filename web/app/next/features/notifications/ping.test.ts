import { describe, expect, it } from 'vitest';
import { titleWithCount } from '@/next/features/notifications/ping';

describe("the tab's title", () => {
  it('counts the notices that wait, and drops the count at none', () => {
    expect(titleWithCount('harnessly', 2)).toBe('(2) harnessly');
    expect(titleWithCount('(2) harnessly', 3)).toBe('(3) harnessly');
    expect(titleWithCount('(3) harnessly', 0)).toBe('harnessly');
  });
});
