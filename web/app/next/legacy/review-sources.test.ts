import { afterEach, describe, expect, it, vi } from 'vitest';
import type { IChangeView } from '@/lib/hl-gitlab-ref';
import { FILE_STATUS } from '@/next/features/review/model';
import { changeViewSource, fileQuery, forgeLineLink, localDiffSource } from '@/next/legacy/review-sources';

vi.mock('@/lib/app-path', () => ({ appFetch: (input: string) => fetch(input) }));

afterEach(() => vi.unstubAllGlobals());

const DIFF = 'diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1 +1 @@\n-a\n+b\n';

const view = (over: Partial<IChangeView> = {}): IChangeView => ({
  kind: 'mr',
  provider: 'gitlab',
  url: 'https://gitlab.example.com/team/app/-/merge_requests/7',
  host: 'gitlab.example.com',
  project: 'team/app',
  ref: '!7',
  title: 'Review (HRN-1)',
  description: '',
  author: 'Eva',
  at: null,
  state: 'opened',
  draft: true,
  sourceBranch: 'feat',
  targetBranch: 'main',
  pipeline: { status: 'failed', url: 'https://gitlab.example.com/p/1' },
  diff: DIFF,
  truncated: false,
  labels: ['ui'],
  stages: [
    {
      name: 'test',
      status: 'failed',
      url: null,
      jobs: [{ name: 'unit', status: 'failed', url: 'https://j/2', durationMs: 1 }],
    },
  ],
  comments: null,
  limited: null,
  approvedBy: ['Fil'],
  approvalsLeft: 1,
  reviewers: ['Eva'],
  commits: [{ sha: 'c1', title: 'one', author: 'Eva', at: null }],
  behind: 3,
  conflicts: true,
  diffRefs: { base: 'b1', head: 'h2' },
  ...over,
});

describe('the review sources', () => {
  it('reads a merge request with its review fields and the failed job', () => {
    const source = changeViewSource(view());
    expect(source.files.map((f) => f.path)).toEqual(['a.ts']);
    expect(source.meta).toMatchObject({
      kind: 'mr',
      draft: true,
      behind: 3,
      conflicts: true,
      approvedBy: ['Fil'],
      approvalsLeft: 1,
      pipeline: { status: 'failed', failedJobUrl: 'https://j/2' },
    });
  });

  it('takes missing review fields as empty (GitHub)', () => {
    const source = changeViewSource(
      view({ provider: 'github', approvedBy: undefined, commits: undefined, behind: undefined }),
    );
    expect(source.meta).toMatchObject({ approvedBy: [], commits: [], behind: null });
  });

  it('asks for one file by its paths, and falls back to the patch when refused', async () => {
    const fetcher = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ oldContent: 'a\n', newContent: 'b\n', truncated: false, binary: false }),
        ),
    );
    vi.stubGlobal('fetch', fetcher);
    const source = changeViewSource(view());
    const content = await source.loadContent!(source.files[0]!);
    expect(content).toEqual({ before: 'a\n', after: 'b\n', complete: true });
    const asked = new URL(String((fetcher.mock.calls[0] as unknown as [string])[0]), 'http://x');
    expect(asked.pathname).toBe('/api/platform/change-view');
    expect(asked.searchParams.get('path')).toBe('a.ts');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{}', { status: 404 })),
    );
    expect(await source.loadContent!(source.files[0]!)).toBeNull();
  });

  it('names added and deleted files for the file route', () => {
    const base = {
      path: 'n.ts',
      oldPath: 'n.ts',
      additions: 1,
      deletions: 0,
      binary: false,
      patch: '',
      version: 'v',
    };
    expect(fileQuery({ ...base, status: FILE_STATUS.added })).toEqual({
      path: 'n.ts',
      oldPath: 'n.ts',
      added: '1',
    });
    expect(fileQuery({ ...base, status: FILE_STATUS.deleted })).toEqual({ path: '', oldPath: 'n.ts' });
  });

  it('links a line to the forge at the commit of its side', () => {
    const [f] = changeViewSource(view()).files;
    expect(forgeLineLink(view(), f!, 4, 'after')).toBe(
      'https://gitlab.example.com/team/app/-/blob/h2/a.ts#L4',
    );
    expect(forgeLineLink(view(), f!, 4, 'before')).toBe(
      'https://gitlab.example.com/team/app/-/blob/b1/a.ts#L4',
    );
    expect(forgeLineLink(view({ diffRefs: undefined }), f!, 4, 'after')).toBe('a.ts:4');
  });

  it('reads local work and a local commit', () => {
    const work = localDiffSource({ root: '/r', name: 'r', branch: 'main', diff: DIFF, truncated: false });
    expect(work.meta).toMatchObject({ kind: 'local', branch: 'main', base: 'HEAD', uncommitted: true });
    expect(work.lineLink!(work.files[0]!, 2, 'after')).toBe('/r/a.ts:2');
    const commit = localDiffSource({
      root: '/r',
      name: 'r',
      branch: null,
      diff: DIFF,
      truncated: false,
      sha: 'abcdef123',
      title: 'x',
    });
    expect(commit.meta).toMatchObject({ kind: 'commit', provider: 'local', ref: 'abcdef12' });
    expect(commit.key).not.toBe(work.key);
  });
});
