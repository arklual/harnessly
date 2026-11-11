// The start of the «Ревью кода» app: the open merge requests the machine's GitLab connection reads and the open pull
// requests of the person's GitHub, in three lists — waiting for the person's review, theirs, all — narrowed by the
// words in the field. A link pasted into the field is
// not a search: it is a merge request or commit to open.
import { DEFAULT_MR_QUERY, filterMrs, mrTag } from '@/next/features/apps/model';
import { changeLinkOf } from '@/next/features/review/gitlab-pick';
import type { AppFeed, AppRow } from '@/next/protocol/v2';

export const HOME_TABS = ['waiting', 'mine', 'all'] as const;
export type THomeTab = (typeof HOME_TABS)[number];

const SCOPE = { waiting: 'review', mine: 'mine', all: 'all' } as const satisfies Record<THomeTab, string>;

/** The open merge requests of one list, the newest moved first. */
export const homeRows = (rows: readonly AppRow[], tab: THomeTab, q: string): AppRow[] =>
  filterMrs(rows, { ...DEFAULT_MR_QUERY, scope: SCOPE[tab], q }, false);

export const homeCounts = (rows: readonly AppRow[]): Record<THomeTab, number> => ({
  waiting: homeRows(rows, 'waiting', '').length,
  mine: homeRows(rows, 'mine', '').length,
  all: homeRows(rows, 'all', '').length,
});

/** The merge requests of the feed's GitLab; empty when GitLab is not in it. */
export const mrRowsOf = (feed: AppFeed | null): AppRow[] =>
  feed?.sections.find((s) => s.id === 'mrs')?.rows ?? [];

/** The address in the field when it is a merge request, pull request or commit to open; null for words. */
export const openableLink = (text: string): string | null => changeLinkOf(text)?.url ?? null;

/** The first letter of a person's name, for the round mark of a row. */
export const initialOf = (name: string | null): string => (name?.trim()[0] ?? '?').toUpperCase();

/** What a row's right side says: what the merge request waits for, or its state. */
export const rowNote = (row: AppRow): string | null => mrTag(row);

/** The open pull requests of the person's GitHub (GET <platform>/code-review/github): off without a connection. */
export interface IGithubList {
  state: 'ready' | 'off' | 'failed';
  reason: string | null;
  host: string | null;
  rows: AppRow[];
}

/** The rows of both sources the lists hold: GitLab's and GitHub's, whichever answered. */
export const homeSourceRows = (gitlab: AppFeed | null, github: IGithubList | null): AppRow[] => [
  ...(gitlab?.state === 'ready' ? mrRowsOf(gitlab) : []),
  ...(github?.state === 'ready' ? github.rows : []),
];

const PULL = /^https:\/\/[^/]+\/[^/]+\/[^/]+\/pull\/\d+$/;

/** Where a row lives, said beside it once both sources are in the lists. */
export const sourceOf = (row: AppRow): 'GitHub' | 'GitLab' => (PULL.test(row.url) ? 'GitHub' : 'GitLab');
