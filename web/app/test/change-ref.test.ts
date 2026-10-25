import { describe, expect, it } from 'vitest';
import { gitlabRefOf } from '@/lib/hl-gitlab-ref';
import { paneRequestOf } from '@/lib/hl-pane-link';

describe('a GitHub link opens in the tab of changes', () => {
  it('reads a pull request, a commit and an issue', () => {
    expect(gitlabRefOf('https://github.com/ly-solutions/harnessly/pull/12/files')).toEqual({
      provider: 'github',
      kind: 'mr',
      host: 'github.com',
      project: 'ly-solutions/harnessly',
      iid: 12,
    });
    expect(gitlabRefOf('https://github.com/ly-solutions/harnessly/commit/0123ABCdef')).toEqual({
      provider: 'github',
      kind: 'commit',
      host: 'github.com',
      project: 'ly-solutions/harnessly',
      sha: '0123abcdef',
    });
    expect(gitlabRefOf('https://github.com/ly-solutions/harnessly/issues/7')).toMatchObject({
      provider: 'github',
      kind: 'issue',
      iid: 7,
    });
  });

  it('leaves the rest of GitHub to the browser', () => {
    for (const link of [
      'https://github.com/ly-solutions/harnessly',
      'https://github.com/ly-solutions/harnessly/pull/x',
      'http://github.com/ly-solutions/harnessly/pull/1',
      'https://github.com:8443/ly-solutions/harnessly/pull/1',
      'https://evil.example/ly-solutions/harnessly/pull/1',
    ])
      expect(gitlabRefOf(link)).toBeNull();
  });

  it('names the tab by the repository and the number', () => {
    expect(paneRequestOf('https://github.com/ly-solutions/harnessly/pull/12')).toEqual({
      kind: 'gitlab',
      target: 'https://github.com/ly-solutions/harnessly/pull/12',
      title: 'harnessly #12',
    });
    expect(paneRequestOf('https://github.com/ly-solutions/harnessly/commit/0123abcdef45')?.title).toBe(
      'harnessly 0123abcd',
    );
    // An issue is this tab as well, not the task card.
    expect(paneRequestOf('https://github.com/ly-solutions/harnessly/issues/7')).toMatchObject({
      kind: 'gitlab',
      title: 'harnessly #7',
    });
  });
});
