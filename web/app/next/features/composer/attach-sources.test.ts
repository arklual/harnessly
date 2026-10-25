import { describe, expect, it, vi } from 'vitest';

const config = vi.hoisted(() => ({
  taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}',
  trackerIssueUrl: 'https://tracker.example/issue/{key}',
}));
vi.mock('@/platform/config', () => ({ appConfig: () => config }));

const { linkRef, attachSource, ATTACH_KIND } = await import('@/next/features/composer/attach-sources');

describe('linkRef', () => {
  it('reads a GitLab merge request link', () => {
    const url = 'https://gitlab.example/team/harnessly/-/merge_requests/42';
    expect(linkRef(url)).toEqual({ kind: 'mr', id: url, title: 'harnessly !42', target: url });
  });

  it('reads a GitHub pull request link', () => {
    const url = 'https://github.com/org/repo/pull/7';
    expect(linkRef(`  ${url} `)).toEqual({ kind: 'mr', id: url, title: 'repo #7', target: url });
  });

  it('reads a tracker issue link as its key', () => {
    const url = 'https://tracker.example/issue/abc-3156';
    expect(linkRef(url)).toEqual({ kind: 'task', id: 'ABC-3156', title: 'ABC-3156', target: url });
  });

  it('reads a bare key and points it at the tracker', () => {
    expect(linkRef('abc-12')).toEqual({
      kind: 'task',
      id: 'ABC-12',
      title: 'ABC-12',
      target: 'https://tracker.example/issue/ABC-12',
    });
  });

  it('is nothing for other words', () => {
    expect(linkRef('hello')).toBeNull();
    expect(linkRef('https://example.com/page')).toBeNull();
  });
});

describe('the link source', () => {
  it('answers one row for a key and none for words', async () => {
    const load = attachSource(ATTACH_KIND.link, { machine: 'm1' as never, cwd: null });
    const signal = new AbortController().signal;
    expect((await load('ABC-1', signal)).map((r) => r.key)).toEqual(['task:ABC-1']);
    expect(await load('nothing', signal)).toEqual([]);
  });
});
