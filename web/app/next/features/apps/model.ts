// The words and the rules of the Apps screen: what a row says, how a list is searched, filtered and sorted, the chip a
// row becomes in a new chat. The machine sends facts (a state, a flag, a day a row last moved); the page words them.
// Nothing here draws: the views and the tests call it.
import { DAY, startOfDay } from '@/next/i18n/format';
import { plural, t } from '@/next/i18n';
import type { AppFeed, AppId, AppPipeline, AppRow, AppSectionId, AppsFeed } from '@/next/protocol/v2';
import { MENTION_KIND, type Mention } from '@/next/stores/mentions';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

/** The apps the screen knows how to draw, in the order of the list on the left, with the glyph of each. An app the
 *  machine does not name is not in the list; an app the screen does not know is not drawn (the machine is newer). */
export const APP_REGISTRY = [
  { id: 'tracker', icon: 'square-check-big' },
  { id: 'gitlab', icon: 'git-pull-request' },
] as const satisfies readonly { id: AppId; icon: string }[];

export const appIcon = (id: AppId): string => APP_REGISTRY.find((app) => app.id === id)?.icon ?? 'app-window';

/** The apps of a feed the screen can draw, in the registry's order. */
export const appsOf = (feed: AppsFeed): AppFeed[] =>
  APP_REGISTRY.flatMap((known) => feed.apps.filter((app) => app.id === known.id));

export const rowsIn = (app: AppFeed | null, section: AppSectionId): AppRow[] =>
  app?.sections.find((item) => item.id === section)?.rows ?? [];

export const rowsOf = (app: AppFeed): AppRow[] => app.sections.flatMap((section) => section.rows);

/** The tracker's own state names the page has words for; any other name is shown as the tracker spells it. */
const STATE_KEYS: Readonly<Record<string, string>> = {
  New: 'new',
  'In Progress': 'inProgress',
  Testing: 'testing',
  Done: 'done',
  Rejected: 'rejected',
};

/** The states of work that is going on: the ones that can stand still. «В работе» is the same in the spaces kept in
 *  Russian. */
const ACTIVE_STATES: ReadonlySet<string> = new Set(['In Progress', 'Testing', 'В работе']);

export const stateText = (state: string): string => {
  const key = STATE_KEYS[state];
  return key ? t(`next.apps.state.${key}`) : state;
};

/** Whole days a row has stood still. */
export const daysStill = (row: AppRow, now: number): number =>
  Math.max(0, Math.floor((now - row.updatedAt) / DAY));

export const isDone = (row: AppRow): boolean => row.closedAt !== null;

/** Open work that stands still: an active task, or an open merge request that is not a draft, untouched for `days`. */
export const isStale = (row: AppRow, now: number, days: number): boolean => {
  if (isDone(row) || daysStill(row, now) < days) return false;
  if (row.mr) return row.state === 'opened' && !row.mr.draft;
  return row.state !== null && ACTIVE_STATES.has(row.state);
};

/** «2 часа назад», «вчера», «6 дней назад»: when a row last moved. */
export const agoText = (at: number, now: number): string => {
  const ago = Math.max(0, now - at);
  if (ago < MINUTE) return t('next.apps.ago.now');
  if (ago < HOUR) return t('next.apps.ago.minutes', { n: Math.floor(ago / MINUTE) });
  const today = startOfDay(now);
  if (at >= today) return plural(Math.floor(ago / HOUR), 'next.apps.ago.hours');
  if (at >= today - DAY) return t('next.apps.ago.yesterday');
  return plural(Math.round((today - startOfDay(at)) / DAY), 'next.apps.ago.days');
};

/** How far an epic is, 0 to 1; an epic with no tasks is not begun. */
export const progressShare = (row: AppRow): number =>
  row.progress && row.progress.total > 0 ? row.progress.done / row.progress.total : 0;

/** The status tag of a merge request: its state when it is over, else what it waits for. */
export const mrTag = (row: AppRow): string | null => {
  if (row.state === 'merged' || row.state === 'closed') return t(`next.apps.mr.${row.state}`);
  if (row.mr?.draft) return t('next.apps.mr.draft');
  return row.flag ? t(`next.apps.flag.${row.flag}`) : null;
};

/** The status tag of a row: the tracker's state, or what the merge request waits for. */
export const rowTag = (row: AppRow): string | null => {
  if (row.mr) return mrTag(row);
  return row.state ? stateText(row.state) : null;
};

export const PIPELINE_TONE: Readonly<Record<AppPipeline, 'good' | 'bad' | 'busy' | 'idle'>> = {
  success: 'good',
  failed: 'bad',
  running: 'busy',
  pending: 'busy',
  canceled: 'idle',
  skipped: 'idle',
  manual: 'idle',
};

export const PIPELINE_ICON: Readonly<Record<AppPipeline, string>> = {
  success: 'circle-check',
  failed: 'circle-x',
  running: 'loader-circle',
  pending: 'clock',
  canceled: 'ban',
  skipped: 'circle-dashed',
  manual: 'play',
};

// ── search ──

const fold = (text: string): string => text.toLowerCase().replaceAll('ё', 'е');

/** A query as the words that all have to be found. */
export const tokens = (query: string): string[] => fold(query).split(/\s+/).filter(Boolean);

const searchable = (row: AppRow): string =>
  fold(
    [
      row.id,
      row.title,
      row.state,
      row.project,
      row.person,
      row.author,
      row.priority,
      row.kind,
      row.parent?.id,
      row.parent?.title,
      row.mr?.source,
      row.target,
      ...row.labels,
      ...row.assignees,
      ...row.reviewers,
    ]
      .filter((part): part is string => !!part)
      .join(' '),
  );

const hasAll = (haystack: string, words: readonly string[]): boolean =>
  words.every((word) => haystack.includes(word));

export const matchesQuery = (row: AppRow, words: readonly string[]): boolean =>
  words.length === 0 || hasAll(searchable(row), words);

/** The subtasks of an epic that the words find (all of them for no words). */
export const matchingChildren = (row: AppRow, words: readonly string[]): AppRow['children'] =>
  words.length === 0
    ? row.children
    : row.children.filter((child) => hasAll(fold(`${child.id} ${child.title} ${child.person ?? ''}`), words));

// ── facets ──

/** The values a facet has in some rows, the most frequent first (then by name), for a menu to offer. */
export const facet = (
  rows: readonly AppRow[],
  pick: (row: AppRow) => readonly (string | null)[],
): string[] => {
  const counts = new Map<string, number>();
  for (const row of rows)
    for (const value of new Set(pick(row))) if (value) counts.set(value, (counts.get(value) ?? 0) + 1);
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([value]) => value);
};

const anyOf = (chosen: readonly string[], have: readonly (string | null)[]): boolean =>
  chosen.length === 0 || have.some((value) => value !== null && chosen.includes(value));

// ── tracker ──

export const TRACKER_VIEWS = ['epics', 'tasks'] as const;
export type TrackerView = (typeof TRACKER_VIEWS)[number];
export const TRACKER_SORTS = ['updated', 'created', 'progress', 'priority', 'title'] as const;
export type TrackerSort = (typeof TRACKER_SORTS)[number];
export const TASK_GROUPS = ['none', 'status', 'epic'] as const;
export type TaskGroup = (typeof TASK_GROUPS)[number];

export interface ITrackerQuery {
  view: TrackerView;
  q: string;
  /** The tracker's state names; none: the open ones (or all, when the person asked for finished work too). */
  states: readonly string[];
  labels: readonly string[];
  kinds: readonly string[];
  /** The ids of the epics whose tasks are wanted. */
  epics: readonly string[];
  sort: TrackerSort;
  group: TaskGroup;
}

export const DEFAULT_TRACKER_QUERY: ITrackerQuery = {
  view: 'epics',
  q: '',
  states: [],
  labels: [],
  kinds: [],
  epics: [],
  sort: 'updated',
  group: 'none',
};

const PRIORITIES = ['Show-stopper', 'Critical', 'Major', 'Normal', 'Minor'] as const;

/** 0 is the most urgent; a priority the page does not know sorts after the known ones. */
export const priorityRank = (priority: string | null): number => {
  const at = PRIORITIES.findIndex((name) => name === priority);
  return at === -1 ? PRIORITIES.length : at;
};

const byTitle = (a: AppRow, b: AppRow): number => a.title.localeCompare(b.title);

const TRACKER_ORDER: Readonly<Record<TrackerSort, (a: AppRow, b: AppRow) => number>> = {
  updated: (a, b) => b.updatedAt - a.updatedAt,
  created: (a, b) => b.createdAt - a.createdAt,
  // What is left to do first: the least done, then the biggest.
  progress: (a, b) =>
    progressShare(a) - progressShare(b) || (b.progress?.total ?? 0) - (a.progress?.total ?? 0),
  priority: (a, b) => priorityRank(a.priority) - priorityRank(b.priority) || b.updatedAt - a.updatedAt,
  title: byTitle,
};

const sortedBy = (rows: AppRow[], order: (a: AppRow, b: AppRow) => number): AppRow[] =>
  [...rows].sort((a, b) => order(a, b) || byTitle(a, b));

/** The rows a tracker query lets through, in its order. `showDone` lists finished work when no state was named. */
export const filterTracker = (rows: readonly AppRow[], query: ITrackerQuery, showDone: boolean): AppRow[] => {
  const words = tokens(query.q);
  const kept = rows.filter((row) => {
    if (query.states.length > 0 ? !anyOf(query.states, [row.state]) : isDone(row) && !showDone) return false;
    if (!anyOf(query.labels, row.labels) || !anyOf(query.kinds, [row.kind])) return false;
    if (query.view === 'tasks' && !anyOf(query.epics, [row.parent?.id ?? null])) return false;
    return matchesQuery(row, words) || matchingChildren(row, words).length > 0;
  });
  return sortedBy(kept, TRACKER_ORDER[query.sort]);
};

export interface ITaskGroup {
  /** The state name or the epic's id; null for the tasks that have none. */
  key: string | null;
  rows: AppRow[];
}

/** Tasks in groups, the groups in the order their first task comes in; a task without an epic goes last. */
export const groupTasks = (rows: readonly AppRow[], by: TaskGroup): ITaskGroup[] => {
  if (by === 'none') return rows.length > 0 ? [{ key: null, rows: [...rows] }] : [];
  const groups = new Map<string | null, AppRow[]>();
  for (const row of rows) {
    const key = by === 'status' ? row.state : (row.parent?.id ?? null);
    groups.set(key, [...(groups.get(key) ?? []), row]);
  }
  const list = [...groups.entries()].map(([key, items]) => ({ key, rows: items }));
  return by === 'epic' ? [...list].sort((a, b) => Number(a.key === null) - Number(b.key === null)) : list;
};

// ── GitLab ──

export const MR_SCOPES = ['all', 'review', 'mine', 'assigned'] as const;
export type MrScope = (typeof MR_SCOPES)[number];
export const MR_STATES = ['opened', 'merged', 'closed'] as const;
export type MrState = (typeof MR_STATES)[number];
export const MR_SORTS = ['updated', 'created', 'title', 'project'] as const;
export type MrSort = (typeof MR_SORTS)[number];
/** What a merge request can be said to be at the moment: facts the list can be narrowed by. */
export const MR_CONDITIONS = ['draft', 'conflicts', 'rebase', 'ready', 'threads', 'approval'] as const;
export type MrCondition = (typeof MR_CONDITIONS)[number];
/** The pipeline filter: a pipeline's state, or `none` for a merge request with no pipeline to show. */
export const MR_CHECKS = ['success', 'failed', 'running', 'none'] as const;
export type MrCheck = (typeof MR_CHECKS)[number];

export interface IMrQuery {
  scope: MrScope;
  q: string;
  states: readonly MrState[];
  authors: readonly string[];
  assignees: readonly string[];
  projects: readonly string[];
  labels: readonly string[];
  checks: readonly MrCheck[];
  conditions: readonly MrCondition[];
  sort: MrSort;
}

export const DEFAULT_MR_QUERY: IMrQuery = {
  scope: 'all',
  q: '',
  states: ['opened'],
  authors: [],
  assignees: [],
  projects: [],
  labels: [],
  checks: [],
  conditions: [],
  sort: 'updated',
};

const mrHas = (row: AppRow, condition: MrCondition): boolean => {
  const mr = row.mr;
  if (!mr) return false;
  switch (condition) {
    case 'draft':
      return mr.draft;
    case 'conflicts':
      return mr.conflicts;
    case 'rebase':
      return mr.needsRebase;
    case 'ready':
      return mr.ready && !mr.draft;
    case 'threads':
      return mr.unresolved;
    case 'approval':
      return !!mr.approvals && mr.approvals.given < mr.approvals.required;
  }
};

const inScope = (row: AppRow, scope: MrScope): boolean => {
  switch (scope) {
    case 'all':
      return true;
    case 'review':
      return row.state === 'opened' && row.me.includes('reviewer') && !row.me.includes('author');
    case 'mine':
      return row.me.includes('author');
    case 'assigned':
      return row.me.includes('assignee');
  }
};

const MR_ORDER: Readonly<Record<MrSort, (a: AppRow, b: AppRow) => number>> = {
  updated: (a, b) => b.updatedAt - a.updatedAt,
  created: (a, b) => b.createdAt - a.createdAt,
  title: byTitle,
  project: (a, b) => (a.project ?? '').localeCompare(b.project ?? '') || b.updatedAt - a.updatedAt,
};

const checkOf = (row: AppRow): MrCheck => {
  const pipeline = row.mr?.pipeline;
  if (!pipeline) return 'none';
  if (pipeline === 'success' || pipeline === 'failed') return pipeline;
  return pipeline === 'running' || pipeline === 'pending' ? 'running' : 'none';
};

/** The merge requests a query lets through, in its order. `hideDrafts` leaves drafts out unless they were asked for. */
export const filterMrs = (rows: readonly AppRow[], query: IMrQuery, hideDrafts: boolean): AppRow[] => {
  const words = tokens(query.q);
  const draftsAsked = query.conditions.includes('draft');
  const kept = rows.filter((row) => {
    if (!inScope(row, query.scope) || !matchesQuery(row, words)) return false;
    if (query.states.length > 0 && !anyOf(query.states, [row.state])) return false;
    if (!anyOf(query.authors, [row.author]) || !anyOf(query.projects, [row.project])) return false;
    if (!anyOf(query.assignees, row.assignees) || !anyOf(query.labels, row.labels)) return false;
    if (query.checks.length > 0 && !query.checks.includes(checkOf(row))) return false;
    if (hideDrafts && !draftsAsked && row.mr?.draft) return false;
    return query.conditions.every((condition) => mrHas(row, condition));
  });
  return sortedBy(kept, MR_ORDER[query.sort]);
};

// ── a new chat ──

/** The text of the bar over the list: one row by its id, several by their number. */
export const barText = (app: AppId, picked: readonly AppRow[]): string => {
  const only = picked.length === 1 ? picked[0] : undefined;
  if (only) return only.id;
  if (app === 'gitlab') return t('next.apps.barMrs', { n: picked.length });
  return plural(picked.length, 'next.apps.barTasks');
};

/** A row as the chip it becomes in the field of a new chat. */
export const mentionOf = (app: AppId, row: AppRow): Mention => ({
  id: row.key,
  kind: app === 'gitlab' ? MENTION_KIND.mr : MENTION_KIND.task,
  label: `${row.id} · ${row.title}`,
  url: row.url,
});
