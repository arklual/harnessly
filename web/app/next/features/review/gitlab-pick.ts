// The review picker's model: the lists the runtime's GET <platform>/gitlab-pick answers (the projects of the
// deployment's GitLab, a project's merge requests, branches and a branch's commits), the links of a change a person
// pastes, and the first message of a chat that asks the agent about a change.
import { getPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId, MessageRef } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

const TIMEOUT_MS = 15_000;

export const PICK_LIST = {
  projects: 'projects',
  mrs: 'mrs',
  branches: 'branches',
  commits: 'commits',
} as const;
export type TPickList = (typeof PICK_LIST)[keyof typeof PICK_LIST];

export const MR_FILTER = { open: 'open', review: 'review', mine: 'mine' } as const;
export type TMrFilter = (typeof MR_FILTER)[keyof typeof MR_FILTER];

export const PICK_STATE = { ready: 'ready', off: 'off', failed: 'failed' } as const;
export type TPickState = (typeof PICK_STATE)[keyof typeof PICK_STATE];

export interface IPickProject {
  path: string;
  name: string;
  url: string;
  activity: string | null;
}

export interface IPickMr {
  iid: number;
  title: string;
  url: string;
  state: string;
  draft: boolean;
  author: string | null;
  source: string;
  target: string;
  updated: string | null;
  /** The project's path, for a list across every project; null when the runtime does not say. */
  project: string | null;
}

export interface IPickBranch {
  name: string;
  sha: string;
  title: string;
  at: string | null;
  url: string;
}

export interface IPickCommit {
  sha: string;
  short: string;
  title: string;
  author: string;
  at: string | null;
  url: string;
}

export interface IPickAnswer<T> {
  state: TPickState;
  /** Why the list is not there: not-configured, no-token, denied, unreachable. */
  reason: string | null;
  items: readonly T[];
}

export interface IPickRequest {
  list: TPickList;
  project?: string;
  filter?: TMrFilter;
  q?: string;
  ref?: string;
}

// ── Reading the answer ──

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const strOrNull = (v: unknown): string | null => (typeof v === 'string' && v !== '' ? v : null);
const isHttps = (v: unknown): v is string => typeof v === 'string' && v.startsWith('https://');

const readProject = (v: unknown): IPickProject | null => {
  if (!isRecord(v) || str(v.path) === '' || !isHttps(v.url)) return null;
  return { path: str(v.path), name: str(v.name) || str(v.path), url: v.url, activity: strOrNull(v.activity) };
};

const readMr = (v: unknown): IPickMr | null => {
  if (!isRecord(v) || typeof v.iid !== 'number' || !isHttps(v.url)) return null;
  return {
    iid: v.iid,
    title: str(v.title),
    url: v.url,
    state: str(v.state),
    draft: v.draft === true,
    author: strOrNull(v.author),
    source: str(v.source),
    target: str(v.target),
    updated: strOrNull(v.updated),
    project: typeof v.project === 'string' && v.project !== '' ? v.project : null,
  };
};

const readBranch = (v: unknown): IPickBranch | null => {
  if (!isRecord(v) || str(v.name) === '' || !isHttps(v.url)) return null;
  return { name: str(v.name), sha: str(v.sha), title: str(v.title), at: strOrNull(v.at), url: v.url };
};

const readCommit = (v: unknown): IPickCommit | null => {
  if (!isRecord(v) || str(v.sha) === '' || !isHttps(v.url)) return null;
  return {
    sha: str(v.sha),
    short: str(v.short) || str(v.sha).slice(0, 8),
    title: str(v.title),
    author: str(v.author),
    at: strOrNull(v.at),
    url: v.url,
  };
};

const READERS = {
  projects: readProject,
  mrs: readMr,
  branches: readBranch,
  commits: readCommit,
} as const;

const isState = (v: unknown): v is TPickState => v === 'ready' || v === 'off' || v === 'failed';

/** The runtime's answer through the reader of its list; items it does not understand are left out. */
export const readPick =
  <T>(read: (v: unknown) => T | null) =>
  (v: unknown): IPickAnswer<T> => {
    if (!isRecord(v) || !isState(v.state)) throw new Error('gitlab-pick: unknown answer');
    const items = Array.isArray(v.items) ? v.items.flatMap((x) => read(x) ?? []) : [];
    return { state: v.state, reason: strOrNull(v.reason), items };
  };

/** The route's query: only what the list needs. */
export const pickQuery = (req: IPickRequest): string => {
  const q = new URLSearchParams({ list: req.list });
  if (req.project) q.set('project', req.project);
  if (req.filter) q.set('filter', req.filter);
  if (req.q?.trim()) q.set('q', req.q.trim());
  if (req.ref) q.set('ref', req.ref);
  return q.toString();
};

type TPickItem = {
  projects: IPickProject;
  mrs: IPickMr;
  branches: IPickBranch;
  commits: IPickCommit;
};

export const loadPick = <L extends TPickList>(
  machine: MachineId,
  req: IPickRequest & { list: L },
): Promise<PageOutcome<IPickAnswer<TPickItem[L]>>> =>
  getPage(
    runtimePath(platformPath(`/gitlab-pick?${pickQuery(req)}`), machine),
    readPick(READERS[req.list] as unknown as (v: unknown) => TPickItem[L] | null),
    TIMEOUT_MS,
  );

// ── Links a person pastes ──

export const CHANGE_KIND = { mr: 'mr', commit: 'commit' } as const;
export type TChangeKind = (typeof CHANGE_KIND)[keyof typeof CHANGE_KIND];

export interface IChangeLink {
  url: string;
  kind: TChangeKind;
  /** GitHub's pull request or commit; false for GitLab's. */
  github: boolean;
  host: string;
  project: string;
  /** The merge request's number, or the commit's sha. */
  ref: string;
}

const MR_PATH = /^\/(.+?)\/-\/merge_requests\/(\d+)(?:[/?#]|$)/;
const COMMIT_PATH = /^\/(.+?)\/-\/commit\/([0-9a-f]{7,40})(?:[/?#]|$)/;
const GH_PULL_PATH = /^\/([^/]+\/[^/]+)\/pull\/(\d+)(?:[/?#]|$)/;
const GH_COMMIT_PATH = /^\/([^/]+\/[^/]+)\/commit\/([0-9a-f]{7,40})(?:[/?#]|$)/;

/** A merge request or a commit of a GitLab (a pull request or a commit of a GitHub) named by a pasted text; null for
 *  anything else. The runtime decides whether it knows that host: this only tells a change link from an address to
 *  browse. */
export const changeLinkOf = (text: string): IChangeLink | null => {
  let u: URL;
  try {
    u = new URL(text.trim());
  } catch {
    return null;
  }
  if (u.protocol !== 'https:' || u.username || u.password) return null;
  const mr = MR_PATH.exec(u.pathname);
  if (mr)
    return {
      url: `${u.origin}/${mr[1]}/-/merge_requests/${mr[2]}`,
      kind: CHANGE_KIND.mr,
      github: false,
      host: u.host,
      project: mr[1],
      ref: mr[2],
    };
  const commit = COMMIT_PATH.exec(u.pathname);
  if (commit)
    return {
      url: `${u.origin}/${commit[1]}/-/commit/${commit[2]}`,
      kind: CHANGE_KIND.commit,
      github: false,
      host: u.host,
      project: commit[1],
      ref: commit[2],
    };
  const pull = GH_PULL_PATH.exec(u.pathname);
  if (pull)
    return {
      url: `${u.origin}/${pull[1]}/pull/${pull[2]}`,
      kind: CHANGE_KIND.mr,
      github: true,
      host: u.host,
      project: pull[1],
      ref: pull[2],
    };
  const ghCommit = GH_COMMIT_PATH.exec(u.pathname);
  if (ghCommit)
    return {
      url: `${u.origin}/${ghCommit[1]}/commit/${ghCommit[2]}`,
      kind: CHANGE_KIND.commit,
      github: true,
      host: u.host,
      project: ghCommit[1],
      ref: ghCommit[2],
    };
  return null;
};

/** What the tab of a change is called: `app !12` (GitHub's `app #12`) or `app 1a2b3c4d`. */
export const changeTitle = (link: IChangeLink): string => {
  const repo = link.project.split('/').at(-1) ?? link.project;
  if (link.kind !== CHANGE_KIND.mr) return `${repo} ${link.ref.slice(0, 8)}`;
  return `${repo} ${link.github ? '#' : '!'}${link.ref}`;
};

// ── Asking the agent ──

/** What the person picked to look at, as the agent is told about it. */
export interface IReviewContext {
  link: IChangeLink;
  title: string | null;
  source: string | null;
  target: string | null;
}

const askMrLine = (link: IChangeLink, vars: Record<string, string>) =>
  link.github ? t('next.review.askPull', vars) : t('next.review.askMr', vars);

/** The first message of a chat about a change: where it is and what it is, then the person's own words. */
export const askMessage = (ctx: IReviewContext, words: string): string => {
  const { link } = ctx;
  const vars = { ref: link.ref, project: link.project, url: link.url };
  const lines = [link.kind === CHANGE_KIND.mr ? askMrLine(link, vars) : t('next.review.askCommit', vars)];
  if (ctx.title) lines.push(t('next.review.askTitle', { title: ctx.title }));
  if (ctx.source && ctx.target)
    lines.push(t('next.review.askBranches', { source: ctx.source, target: ctx.target }));
  else if (ctx.source) lines.push(t('next.review.askBranch', { branch: ctx.source }));
  const said = words.trim();
  return said ? `${lines.join('\n')}\n\n${said}` : lines.join('\n');
};

/** The merge request as a chip of the first message, so the agent gets its `<attached>` block. */
export const askRef = (ctx: IReviewContext): MessageRef => ({
  kind: 'mr',
  id: ctx.link.url,
  title: changeTitle(ctx.link),
  target: ctx.link.url,
});
