import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  agoText,
  appsOf,
  barText,
  DEFAULT_MR_QUERY,
  DEFAULT_TRACKER_QUERY,
  facet,
  filterMrs,
  filterTracker,
  groupTasks,
  isStale,
  matchingChildren,
  mentionOf,
  priorityRank,
  progressShare,
  rowTag,
  tokens,
} from '@/next/features/apps/model';
import type { AppFeed, AppMR, AppRow, AppsFeed } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));

const DAY = 86_400_000;
const NOW = new Date(2026, 9, 24, 15, 0, 0).getTime();

const row = (patch: Partial<AppRow>): AppRow => ({
  key: 'k',
  id: 'PRJ-1',
  title: 'A title',
  url: 'https://tracker.example.com/issue/PRJ-1',
  state: null,
  flag: null,
  tone: null,
  project: null,
  person: null,
  target: null,
  createdAt: NOW - 10 * DAY,
  updatedAt: NOW,
  closedAt: null,
  author: null,
  assignees: [],
  reviewers: [],
  me: [],
  labels: [],
  priority: null,
  kind: null,
  parent: null,
  progress: null,
  children: [],
  mr: null,
  ...patch,
});

const mr = (patch: Partial<AppMR> = {}): AppMR => ({
  source: 'feature/x',
  draft: false,
  conflicts: false,
  needsRebase: false,
  ready: false,
  unresolved: false,
  comments: 0,
  pipeline: null,
  approvals: null,
  ...patch,
});

const mrRow = (id: string, patch: Partial<AppRow> = {}, mrPatch: Partial<AppMR> = {}): AppRow =>
  row({ key: id, id, title: `MR ${id}`, state: 'opened', mr: mr(mrPatch), ...patch });

const task = (id: string, patch: Partial<AppRow> = {}): AppRow =>
  row({ key: id, id, title: `Task ${id}`, state: 'New', ...patch });

describe('apps model: words', () => {
  it('words when a row last moved', () => {
    expect(agoText(NOW - 10_000, NOW)).toBe('just now');
    expect(agoText(NOW - 5 * 60_000, NOW)).toBe('5 min ago');
    expect(agoText(NOW - 2 * 3_600_000, NOW)).toBe('2 hours ago');
    expect(agoText(new Date(2026, 9, 23, 22, 0, 0).getTime(), NOW)).toBe('yesterday');
    expect(agoText(NOW - 6 * DAY, NOW)).toBe('6 days ago');
  });

  it('words a status: the tracker`s state in the page`s words or as spelled, a merge request by its state or what it waits for', () => {
    expect(rowTag(task('a', { state: 'In Progress' }))).toBe('In progress');
    expect(rowTag(task('a', { state: 'Новая' }))).toBe('Новая');
    expect(rowTag(task('a', { state: null }))).toBeNull();
    expect(rowTag(mrRow('!1', { flag: 'review' }))).toBe('Review');
    expect(rowTag(mrRow('!1', { flag: 'review' }, { draft: true }))).toBe('Draft');
    expect(rowTag(mrRow('!1', { state: 'merged' }))).toBe('Merged');
  });

  it('names the picked rows for the bar and makes a chip of a row', () => {
    const rows = [task('PRJ-1'), task('PRJ-2')];
    expect(barText('tracker', [rows[0]!])).toBe('PRJ-1');
    expect(barText('tracker', rows)).toBe('2 tasks');
    expect(barText('gitlab', rows)).toBe('2 MR');
    expect(
      mentionOf('tracker', task('PRJ-2628', { title: 'Epic', url: 'https://t.example/issue/PRJ-2628' })),
    ).toEqual({
      id: 'PRJ-2628',
      kind: 'task',
      label: 'PRJ-2628 · Epic',
      url: 'https://t.example/issue/PRJ-2628',
    });
    expect(mentionOf('gitlab', mrRow('!5')).kind).toBe('mr');
  });

  it('keeps the apps it knows in its own order and leaves the ones it does not', () => {
    const feed = (ids: string[]): AppsFeed => ({
      at: NOW,
      staleDays: 5,
      apps: ids.map((id) => ({
        id,
        state: 'off',
        reason: 'no-token',
        host: null,
        me: null,
        sections: [],
      })) as AppFeed[],
    });
    expect(appsOf(feed(['gitlab', 'tracker', 'jira'])).map((app) => app.id)).toEqual(['tracker', 'gitlab']);
  });
});

describe('apps model: stale work', () => {
  it('calls stale only open work that is going on and has not moved for the term', () => {
    const old = NOW - 9 * DAY;
    expect(isStale(task('a', { state: 'In Progress', updatedAt: old }), NOW, 5)).toBe(true);
    expect(isStale(task('a', { state: 'В работе', updatedAt: old }), NOW, 5)).toBe(true);
    expect(isStale(task('a', { state: 'In Progress', updatedAt: old }), NOW, 10)).toBe(false);
    expect(isStale(task('a', { state: 'New', updatedAt: old }), NOW, 5)).toBe(false);
    expect(isStale(task('a', { state: 'Done', updatedAt: old, closedAt: old }), NOW, 5)).toBe(false);
    expect(isStale(mrRow('!1', { updatedAt: old }), NOW, 5)).toBe(true);
    expect(isStale(mrRow('!1', { updatedAt: old }, { draft: true }), NOW, 5)).toBe(false);
    expect(isStale(mrRow('!1', { updatedAt: old, state: 'merged', closedAt: old }), NOW, 5)).toBe(false);
  });
});

describe('apps model: search', () => {
  it('splits a query into the words that all have to be found, folding case and ё', () => {
    expect(tokens('  Ёлка  PRJ-12 ')).toEqual(['елка', 'prj-12']);
    expect(tokens('')).toEqual([]);
  });

  it('finds a row by its id, title, tag, person, epic and branch', () => {
    const rows = [
      task('PRJ-1', { title: 'Refund filter', labels: ['repo:admin'], person: 'Ann Lee' }),
      task('PRJ-2', {
        title: 'Cart',
        parent: { id: 'PRJ-9', title: 'Checkout', url: 'https://t.example/issue/PRJ-9' },
      }),
    ];
    const find = (q: string) =>
      filterTracker(rows, { ...DEFAULT_TRACKER_QUERY, view: 'tasks', q }, false).map((r) => r.id);
    expect(find('prj-1')).toEqual(['PRJ-1']);
    expect(find('refund admin')).toEqual(['PRJ-1']);
    expect(find('ann')).toEqual(['PRJ-1']);
    expect(find('checkout')).toEqual(['PRJ-2']);
    expect(find('nothing here')).toEqual([]);
    const branch = mrRow('!7', {}, { source: 'fix/login' });
    expect(filterMrs([branch], { ...DEFAULT_MR_QUERY, q: 'login' }, false)).toHaveLength(1);
  });

  it('finds an epic by one of its subtasks and keeps only the subtasks the words find', () => {
    const epic = row({
      id: 'PRJ-5',
      key: 'PRJ-5',
      title: 'Big epic',
      state: 'In Progress',
      children: [
        {
          id: 'PRJ-6',
          title: 'Write the schema',
          url: 'https://t.example/issue/PRJ-6',
          state: 'New',
          done: false,
          person: null,
        },
        {
          id: 'PRJ-7',
          title: 'Ship it',
          url: 'https://t.example/issue/PRJ-7',
          state: 'Done',
          done: true,
          person: 'Bob',
        },
      ],
    });
    expect(filterTracker([epic], { ...DEFAULT_TRACKER_QUERY, q: 'schema' }, false)).toHaveLength(1);
    expect(matchingChildren(epic, tokens('schema')).map((kid) => kid.id)).toEqual(['PRJ-6']);
    expect(matchingChildren(epic, tokens('bob')).map((kid) => kid.id)).toEqual(['PRJ-7']);
    expect(matchingChildren(epic, [])).toHaveLength(2);
    expect(filterTracker([epic], { ...DEFAULT_TRACKER_QUERY, q: 'unrelated' }, false)).toHaveLength(0);
  });
});

describe('apps model: the tracker`s lists', () => {
  const rows = [
    task('PRJ-1', {
      state: 'In Progress',
      labels: ['repo:a'],
      kind: 'Bug',
      updatedAt: NOW - 1 * DAY,
      priority: 'Major',
    }),
    task('PRJ-2', {
      state: 'New',
      labels: ['repo:b', 'waiting:external'],
      kind: 'Feature',
      updatedAt: NOW - 3 * DAY,
    }),
    task('OPS-3', { state: 'Новая', labels: [], kind: null, updatedAt: NOW - 2 * DAY, priority: 'Critical' }),
    task('PRJ-4', { state: 'Done', closedAt: NOW - DAY, updatedAt: NOW - DAY, kind: 'Bug' }),
  ];
  const ids = (list: AppRow[]) => list.map((r) => r.id);
  const q = { ...DEFAULT_TRACKER_QUERY, view: 'tasks' } as const;

  it('lists open work first and finished work only when asked for', () => {
    expect(ids(filterTracker(rows, q, false))).toEqual(['PRJ-1', 'OPS-3', 'PRJ-2']);
    expect(ids(filterTracker(rows, q, true))).toEqual(['PRJ-1', 'PRJ-4', 'OPS-3', 'PRJ-2']);
  });

  it('lists the states that were named, finished ones too', () => {
    expect(ids(filterTracker(rows, { ...q, states: ['Done'] }, false))).toEqual(['PRJ-4']);
    expect(ids(filterTracker(rows, { ...q, states: ['New', 'Новая'] }, false))).toEqual(['OPS-3', 'PRJ-2']);
  });

  it('narrows by any of the tags, by kind and by epic', () => {
    expect(ids(filterTracker(rows, { ...q, labels: ['repo:b', 'repo:a'] }, false))).toEqual([
      'PRJ-1',
      'PRJ-2',
    ]);
    expect(ids(filterTracker(rows, { ...q, kinds: ['Bug'] }, false))).toEqual(['PRJ-1']);
    const child = task('PRJ-9', {
      parent: { id: 'PRJ-5', title: 'Epic', url: 'https://t.example/issue/PRJ-5' },
    });
    expect(ids(filterTracker([...rows, child], { ...q, epics: ['PRJ-5'] }, false))).toEqual(['PRJ-9']);
  });

  it('sorts by the day it moved, the urgency, or the title', () => {
    expect(ids(filterTracker(rows, { ...q, sort: 'priority' }, false))).toEqual(['OPS-3', 'PRJ-1', 'PRJ-2']);
    expect(ids(filterTracker(rows, { ...q, sort: 'title' }, false))).toEqual(['OPS-3', 'PRJ-1', 'PRJ-2']);
    const born = [
      task('A', { createdAt: NOW - 3 * DAY }),
      task('B', { createdAt: NOW - DAY }),
      task('C', { createdAt: NOW - 2 * DAY }),
    ];
    expect(ids(filterTracker(born, { ...q, sort: 'created' }, false))).toEqual(['B', 'C', 'A']);
    expect(priorityRank('Critical')).toBeLessThan(priorityRank('Normal'));
    expect(priorityRank('Odd')).toBeGreaterThan(priorityRank('Minor'));
  });

  it('sorts epics by what is left to do, the least done first', () => {
    const epic = (id: string, done: number, total: number) =>
      row({ id, key: id, title: id, state: 'New', progress: { done, total } });
    const list = [epic('A', 5, 6), epic('B', 0, 3), epic('C', 1, 4), epic('D', 0, 0)];
    expect(ids(filterTracker(list, { ...DEFAULT_TRACKER_QUERY, sort: 'progress' }, false))).toEqual([
      'B',
      'D',
      'C',
      'A',
    ]);
    expect(progressShare(epic('E', 3, 4))).toBe(0.75);
    expect(progressShare(epic('F', 0, 0))).toBe(0);
  });

  it('groups tasks by state or by epic, the ones without an epic last', () => {
    const epicOf = (id: string) => ({ id, title: id, url: 'https://t.example/issue/' + id });
    const list = [
      task('A', { state: 'New' }),
      task('B', { state: 'In Progress', parent: epicOf('E1') }),
      task('C', { state: 'New', parent: epicOf('E1') }),
    ];
    expect(groupTasks(list, 'status').map((g) => [g.key, ids(g.rows)])).toEqual([
      ['New', ['A', 'C']],
      ['In Progress', ['B']],
    ]);
    expect(groupTasks(list, 'epic').map((g) => [g.key, ids(g.rows)])).toEqual([
      ['E1', ['B', 'C']],
      [null, ['A']],
    ]);
    expect(groupTasks(list, 'none')).toHaveLength(1);
    expect(groupTasks([], 'none')).toEqual([]);
  });

  it('offers the values a facet has, the most frequent first', () => {
    expect(facet(rows, (r) => r.labels)).toEqual(['repo:a', 'repo:b', 'waiting:external']);
    expect(
      facet([task('a', { kind: 'Bug' }), task('b', { kind: 'Bug' }), task('c', { kind: 'Feature' })], (r) => [
        r.kind,
        r.kind,
      ]),
    ).toEqual(['Bug', 'Feature']);
  });
});

describe('apps model: GitLab`s list', () => {
  const rows = [
    mrRow(
      '!1',
      {
        me: ['reviewer'],
        flag: 'review',
        author: 'Bob',
        assignees: [],
        project: 'api',
        labels: ['backend'],
        updatedAt: NOW - DAY,
      },
      { pipeline: 'success', ready: true },
    ),
    mrRow(
      '!2',
      {
        me: ['author', 'assignee'],
        author: 'Ann',
        assignees: ['Ann'],
        project: 'web',
        updatedAt: NOW - 2 * DAY,
      },
      { pipeline: 'failed', conflicts: true, needsRebase: true },
    ),
    mrRow(
      '!3',
      { author: 'Bob', assignees: ['Cy'], project: 'api', updatedAt: NOW - 3 * DAY },
      { draft: true, unresolved: true, approvals: { given: 0, required: 2 } },
    ),
    mrRow('!4', {
      state: 'merged',
      closedAt: NOW - DAY,
      author: 'Cy',
      project: 'web',
      updatedAt: NOW - 4 * DAY,
    }),
    mrRow('!5', { state: 'closed', closedAt: NOW - DAY, updatedAt: NOW - 5 * DAY }),
  ];
  const ids = (list: AppRow[]) => list.map((r) => r.id);

  it('lists the open ones first and the state asked for', () => {
    expect(ids(filterMrs(rows, DEFAULT_MR_QUERY, false))).toEqual(['!1', '!2', '!3']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, states: ['merged', 'closed'] }, false))).toEqual([
      '!4',
      '!5',
    ]);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, states: [] }, false))).toHaveLength(5);
  });

  it('narrows to what waits for the person, is theirs, or is on them', () => {
    const scope = (s: 'review' | 'mine' | 'assigned') =>
      ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, scope: s }, false));
    expect(scope('review')).toEqual(['!1']);
    expect(scope('mine')).toEqual(['!2']);
    expect(scope('assigned')).toEqual(['!2']);
  });

  it('narrows by author, assignee, project and label', () => {
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, authors: ['Bob'] }, false))).toEqual(['!1', '!3']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, assignees: ['Cy'] }, false))).toEqual(['!3']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, projects: ['web'] }, false))).toEqual(['!2']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, labels: ['backend'] }, false))).toEqual(['!1']);
  });

  it('narrows by the pipeline and by what is the matter with the merge request', () => {
    const checks = (c: 'success' | 'failed' | 'none') =>
      ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, checks: [c] }, false));
    expect(checks('success')).toEqual(['!1']);
    expect(checks('failed')).toEqual(['!2']);
    expect(checks('none')).toEqual(['!3']);
    const cond = (c: 'draft' | 'conflicts' | 'rebase' | 'ready' | 'threads' | 'approval') =>
      ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, conditions: [c] }, false));
    expect(cond('draft')).toEqual(['!3']);
    expect(cond('conflicts')).toEqual(['!2']);
    expect(cond('rebase')).toEqual(['!2']);
    expect(cond('ready')).toEqual(['!1']);
    expect(cond('threads')).toEqual(['!3']);
    expect(cond('approval')).toEqual(['!3']);
  });

  it('keeps drafts out when the person chose so, unless the condition asks for them', () => {
    expect(ids(filterMrs(rows, DEFAULT_MR_QUERY, true))).toEqual(['!1', '!2']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, conditions: ['draft'] }, true))).toEqual(['!3']);
  });

  it('sorts by the day it moved, the project, or the title', () => {
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, sort: 'project' }, false))).toEqual(['!1', '!3', '!2']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, sort: 'title' }, false))).toEqual(['!1', '!2', '!3']);
    expect(ids(filterMrs(rows, { ...DEFAULT_MR_QUERY, sort: 'created', states: [] }, false))[0]).toBe('!1');
  });
});
