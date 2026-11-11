import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  homeCounts,
  homeRows,
  homeSourceRows,
  initialOf,
  mrRowsOf,
  openableLink,
  sourceOf,
} from '@/next/features/review/app/home';
import type { AppFeed, AppRow } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));

const row = (patch: Partial<AppRow>): AppRow => ({
  key: patch.id ?? 'k',
  id: '!1',
  title: 'A change',
  url: 'https://gitlab.example.com/team/app/-/merge_requests/1',
  state: 'opened',
  flag: null,
  tone: null,
  project: 'app',
  person: null,
  target: 'main',
  createdAt: 1,
  updatedAt: 1,
  closedAt: null,
  author: 'Oleg',
  assignees: [],
  reviewers: [],
  me: [],
  labels: [],
  priority: null,
  kind: null,
  parent: null,
  progress: null,
  children: [],
  mr: {
    source: 'feature',
    draft: false,
    conflicts: false,
    needsRebase: false,
    ready: false,
    unresolved: false,
    comments: 0,
    pipeline: null,
    approvals: null,
  },
  ...patch,
});

const rows = [
  row({ id: '!1', me: ['reviewer'], updatedAt: 3 }),
  row({ id: '!2', me: ['author'], title: 'Safe secrets', updatedAt: 2 }),
  row({ id: '!3', updatedAt: 1 }),
  row({ id: '!4', state: 'merged', me: ['reviewer'] }),
];

describe('homeRows', () => {
  it('splits open merge requests into waiting, mine and all', () => {
    expect(homeRows(rows, 'waiting', '').map((r) => r.id)).toEqual(['!1']);
    expect(homeRows(rows, 'mine', '').map((r) => r.id)).toEqual(['!2']);
    expect(homeRows(rows, 'all', '').map((r) => r.id)).toEqual(['!1', '!2', '!3']);
    expect(homeCounts(rows)).toEqual({ waiting: 1, mine: 1, all: 3 });
  });
  it('narrows by words', () => {
    expect(homeRows(rows, 'all', 'secrets').map((r) => r.id)).toEqual(['!2']);
  });
});

describe('mrRowsOf', () => {
  it('reads the merge requests of the feed', () => {
    const feed = { id: 'gitlab', sections: [{ id: 'mrs', rows }] } as unknown as AppFeed;
    expect(mrRowsOf(feed)).toHaveLength(4);
    expect(mrRowsOf(null)).toEqual([]);
  });
});

describe('openableLink', () => {
  it('opens a merge request or commit link and leaves words to the search', () => {
    expect(openableLink(' https://gitlab.example.com/team/app/-/merge_requests/12/diffs ')).toBe(
      'https://gitlab.example.com/team/app/-/merge_requests/12',
    );
    expect(openableLink('https://gitlab.example.com/team/app/-/commit/0123abc')).toBe(
      'https://gitlab.example.com/team/app/-/commit/0123abc',
    );
    expect(openableLink('secrets')).toBeNull();
  });
});

describe('initialOf', () => {
  it('takes the first letter', () => {
    expect(initialOf('oleg')).toBe('O');
    expect(initialOf(null)).toBe('?');
  });
});

describe('GitHub beside GitLab', () => {
  it('puts the rows of whichever source answered into the lists, each saying where it lives', () => {
    const gl = row({ id: '!1', key: 'gl' });
    const gh = row({ id: '#2', key: 'gh', url: 'https://github.com/acme/app/pull/2', me: ['reviewer'] });
    const feed = {
      id: 'gitlab',
      state: 'ready',
      sections: [{ id: 'mrs', rows: [gl] }],
    } as unknown as AppFeed;
    const github = { state: 'ready' as const, reason: null, host: 'github.com', rows: [gh] };
    expect(homeSourceRows(feed, github).map((r) => r.key)).toEqual(['gl', 'gh']);
    expect(homeSourceRows(null, github).map((r) => r.key)).toEqual(['gh']);
    expect(homeSourceRows(feed, { ...github, state: 'failed' }).map((r) => r.key)).toEqual(['gl']);
    expect(sourceOf(gh)).toBe('GitHub');
    expect(sourceOf(gl)).toBe('GitLab');
  });
});
