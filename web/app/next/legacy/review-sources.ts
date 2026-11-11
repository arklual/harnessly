// The review's sources, the bridge between the current interface's data and the review screen: a merge request, pull request or commit as the runtime's change view gives it
// (`/api/platform/change-view`), and the local work of a chat — a repository's uncommitted diff or one commit
// (`/api/platform/changes`). Both list the files from the patch at once and read a file's two versions only when it is
// opened.
import { appFetch, scopedAppPath } from '@/lib/app-path';
import type { IChangeSet, IWorkEntry } from '@/lib/chat-work';
import type { IChangeView } from '@/lib/hl-gitlab-ref';
import {
  FILE_STATUS,
  LINE_SIDE,
  type IFileContent,
  type IReviewCommit,
  type IReviewFile,
  type IReviewSource,
  type TLineSide,
} from '@/next/features/review/model';
import { filesFromPatch } from '@/next/features/review/patch';

interface ISidesAnswer {
  oldContent: string;
  newContent: string;
  truncated: boolean;
  binary: boolean;
}

/** The query naming one file's two versions: its path now, what it was, and whether it is new. */
export const fileQuery = (file: IReviewFile): Record<string, string> => {
  const query: Record<string, string> = {
    path: file.status === FILE_STATUS.deleted ? '' : file.path,
    oldPath: file.oldPath,
  };
  if (file.status === FILE_STATUS.added) query.added = '1';
  return query;
};

const readSides = async (address: string, machineId?: string): Promise<IFileContent | null> => {
  try {
    const response = await (machineId
      ? fetch(scopedAppPath(address, machineId), { credentials: 'same-origin' })
      : appFetch(address));
    if (!response.ok) return null;
    const sides = (await response.json()) as ISidesAnswer;
    if (sides.truncated || sides.binary) return null;
    return { before: sides.oldContent, after: sides.newContent, complete: true };
  } catch {
    return null;
  }
};

const commitsOf = (view: IChangeView): IReviewCommit[] => view.commits ?? [];

const failedJobOf = (view: IChangeView): string | null => {
  for (const stage of view.stages ?? [])
    for (const job of stage.jobs) if (job.status === 'failed' && job.url) return job.url;
  return null;
};

/** A link to one line on the forge, at the commit of its side; `path:line` when the commit is unknown. */
export const forgeLineLink = (
  view: Pick<IChangeView, 'provider' | 'url' | 'project' | 'diffRefs'>,
  file: IReviewFile,
  line: number,
  side: TLineSide,
): string => {
  const sha = side === LINE_SIDE.before ? view.diffRefs?.base : view.diffRefs?.head;
  const path = side === LINE_SIDE.before ? file.oldPath : file.path;
  if (!sha) return `${path}:${line}`;
  const origin = new URL(view.url).origin;
  const blob = view.provider === 'github' ? 'blob' : '-/blob';
  return `${origin}/${view.project}/${blob}/${sha}/${path}#L${line}`;
};

/** A merge request, pull request or commit of the change view. */
export const changeViewSource = (view: IChangeView): IReviewSource => {
  const files = filesFromPatch(view.diff);
  const base = { key: view.url, files, truncated: view.truncated };
  const loadContent = (file: IReviewFile) =>
    readSides(`/api/platform/change-view?${new URLSearchParams({ url: view.url, ...fileQuery(file) })}`);
  const lineLink = (file: IReviewFile, line: number, side: TLineSide) =>
    forgeLineLink(view, file, line, side);
  if (view.kind === 'commit')
    return {
      ...base,
      loadContent,
      lineLink,
      meta: {
        kind: 'commit',
        provider: view.provider,
        url: view.url,
        project: view.project,
        ref: view.ref,
        title: view.title,
        author: view.author,
        at: view.at,
      },
    };
  return {
    ...base,
    loadContent,
    lineLink,
    meta: {
      kind: 'mr',
      provider: view.provider,
      url: view.url,
      project: view.project,
      ref: view.ref,
      title: view.title,
      author: view.author,
      sourceBranch: view.sourceBranch,
      targetBranch: view.targetBranch,
      state: view.state,
      draft: view.draft,
      pipeline: view.pipeline
        ? { status: view.pipeline.status, url: view.pipeline.url, failedJobUrl: failedJobOf(view) }
        : null,
      approvedBy: view.approvedBy ?? [],
      approvalsLeft: view.approvalsLeft ?? null,
      reviewers: view.reviewers ?? [],
      labels: view.labels,
      commits: commitsOf(view),
      behind: view.behind ?? null,
      conflicts: view.conflicts ?? false,
    },
  };
};

export interface ILocalDiffInput {
  /** The repository's top folder. */
  root: string;
  /** The repository's name. */
  name: string;
  branch: string | null;
  diff: string;
  truncated: boolean;
  /** A commit's diff; absent for uncommitted work (HEAD against the working tree). */
  sha?: string;
  /** The commit's title, for a commit. */
  title?: string;
  /** The chat's commits in this repository, listed in the header of uncommitted work. */
  commits?: IReviewCommit[];
  /** The machine the repository is on; absent for this page's own machine. */
  machineId?: string;
}

/** A repository's uncommitted work, or one commit of it, on this machine. */
export const localDiffSource = (input: ILocalDiffInput): IReviewSource => {
  const files = filesFromPatch(input.diff);
  const query = (file: IReviewFile) => {
    const q: Record<string, string> = { root: input.root, ...fileQuery(file) };
    if (input.sha) q.sha = input.sha;
    return q;
  };
  const sharedFields = {
    key: `${input.root}\0${input.sha ?? 'work'}`,
    files,
    truncated: input.truncated,
    loadContent: (file: IReviewFile) =>
      readSides(`/api/platform/changes?${new URLSearchParams(query(file))}`, input.machineId),
    lineLink: (file: IReviewFile, line: number, side: TLineSide) =>
      `${input.root}/${side === LINE_SIDE.before ? file.oldPath : file.path}:${line}`,
  };
  if (input.sha)
    return {
      ...sharedFields,
      meta: {
        kind: 'commit',
        provider: 'local',
        url: null,
        project: input.name,
        ref: input.sha.slice(0, 8),
        title: input.title ?? input.sha.slice(0, 8),
        author: null,
        at: null,
      },
    };
  return {
    ...sharedFields,
    meta: {
      kind: 'local',
      repo: input.name,
      root: input.root,
      branch: input.branch,
      base: 'HEAD',
      commits: input.commits ?? [],
      uncommitted: files.length > 0,
    },
  };
};

/**
 * One worktree of a chat's work, as «Changes» reads it: from its machine (uncommitted work and the chat's commits, both
 * versions of a file on demand), or a pushed branch from its code host (the patch alone).
 */
export const changeSetSource = (set: IChangeSet, entry: IWorkEntry): IReviewSource => {
  const name = entry.repo.replace(/\/+$/, '').split('/').at(-1) ?? entry.repo;
  const commits: IReviewCommit[] = set.commits.map((c) => ({
    sha: c.sha,
    title: c.subject,
    author: c.author || null,
    at: c.date ? new Date(c.date).toISOString() : null,
  }));
  if (set.source === 'machine')
    return localDiffSource({
      root: entry.worktree,
      name,
      branch: set.branch,
      diff: set.diff,
      truncated: set.truncated,
      commits,
      machineId: entry.machineId,
    });
  const files = filesFromPatch(set.diff);
  return {
    key: `${entry.worktree}\0remote\0${set.head ?? ''}`,
    files,
    truncated: set.truncated,
    meta: {
      kind: 'local',
      repo: name,
      root: entry.worktree,
      branch: set.branch,
      base: set.base,
      commits,
      uncommitted: false,
    },
  };
};
