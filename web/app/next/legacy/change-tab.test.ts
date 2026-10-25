import { describe, expect, it } from 'vitest';
import { changeLookOf } from '@/next/legacy/change-tab';

describe('changeLookOf', () => {
  it('names a GitHub issue by its number and gives it the issue icon, not the last part of the address', () => {
    expect(changeLookOf('https://github.com/acme/app/issues/1')).toEqual({
      name: 'app #1',
      icon: 'circle-dot',
    });
  });

  it('keeps the merge request icon for a pull request and a commit', () => {
    expect(changeLookOf('https://github.com/acme/app/pull/7')).toEqual({
      name: 'app #7',
      icon: 'git-pull-request',
    });
    expect(changeLookOf('https://github.com/acme/app/commit/0123456789abcdef')?.icon).toBe(
      'git-pull-request',
    );
  });

  it('is nothing for an address that is no change, or none', () => {
    expect(changeLookOf('https://example.com/a/b')).toBeNull();
    expect(changeLookOf(null)).toBeNull();
  });
});
