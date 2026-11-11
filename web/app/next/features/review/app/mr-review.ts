// The review of a merge request (a pull request on GitHub) as the runtime reads it through the person's connection
// (cmd/runtime/routes_code_review.go): its discussions at their lines, the person's draft notes (GitLab's draft notes,
// GitHub's pending review), the approvals, and the acts. The truth is the code host's; here are its shape and what the
// screen derives from it.
import { LINE_SIDE, type IReviewFile, type TLineSide } from '@/next/features/review/model';

export interface IMrPerson {
  name: string;
  login?: string;
}

export interface IMrNote {
  id: number;
  author?: IMrPerson | null;
  mine: boolean;
  body: string;
  created?: string;
  edited?: boolean;
}

export interface IMrThread {
  id: string;
  resolvable: boolean;
  resolved: boolean;
  path?: string;
  line?: number;
  side?: TLineSide;
  outdated?: boolean;
  notes: IMrNote[];
}

export interface IMrDraft {
  id: number;
  body: string;
  path?: string;
  line?: number;
  side?: TLineSide;
  replyTo?: string;
}

export interface IMrRefs {
  base: string;
  start: string;
  head: string;
}

export interface IMrReview {
  me: IMrPerson | null;
  mine: boolean;
  title: string;
  state: string;
  draft: boolean;
  approved: boolean;
  canApprove: boolean;
  approvedBy: string[];
  refs: IMrRefs;
  threads: IMrThread[];
  drafts: IMrDraft[];
}

/** The code host a review lives on, as the screen names it. */
export const FORGE = { gitlab: 'GitLab', github: 'GitHub' } as const;
export type TForge = (typeof FORGE)[keyof typeof FORGE];

const MR_PATH = /\/-\/merge_requests\/\d+(?:[/?#]|$)/;
const PULL_PATH = /^\/[^/]+\/[^/]+\/pull\/\d+(?:[/?#]|$)/;

const pathOf = (url: string): string | null => {
  try {
    return new URL(url).pathname;
  } catch {
    return null;
  }
};

/** GitHub for a pull request's link, GitLab for a merge request's. */
export const forgeOf = (url: string): TForge => {
  const path = pathOf(url);
  return path !== null && PULL_PATH.test(path) ? FORGE.github : FORGE.gitlab;
};

/** A link whose discussions the runtime reads: a merge request of GitLab or a pull request of GitHub. */
export const isReviewLink = (url: string): boolean => {
  const path = pathOf(url);
  return path !== null && (MR_PATH.test(path) || PULL_PATH.test(path));
};

/** The code host of a git remote (git@host:group/repo.git or https://host/group/repo). */
const hostOfRemote = (remote: string): string => {
  const scp = /^[\w.-]+@([^:/]+):/.exec(remote);
  if (scp?.[1]) return scp[1];
  try {
    return new URL(remote).hostname;
  } catch {
    return '';
  }
};

export const forgeOfRemote = (remote: string): TForge =>
  hostOfRemote(remote).toLowerCase().includes('github') ? FORGE.github : FORGE.gitlab;

/** GitHub's review holds only threads at lines: an answer to the conversation goes at once, never into the draft. */
export const draftableIn = (forge: TForge, thread: Pick<IMrThread, 'path'>): boolean =>
  forge === FORGE.gitlab || Boolean(thread.path);

/** A submitted review's text on GitHub (its thread is review:<id>) is edited, never deleted. */
export const deletableIn = (forge: TForge, thread: Pick<IMrThread, 'id'>): boolean =>
  forge === FORGE.gitlab || !thread.id.startsWith('review:');

export interface IMrConnection {
  id: string;
  name: string;
  /** The person's own connection: writes go through it. False for the deployment's, which only reads. */
  personal: boolean;
  host: string;
}

export const MR_ACT = {
  reply: 'reply',
  edit: 'edit',
  delete: 'delete',
  resolve: 'resolve',
  draft: 'draft',
  draftEdit: 'draft-edit',
  draftDelete: 'draft-delete',
  publish: 'publish',
  approve: 'approve',
  unapprove: 'unapprove',
  ready: 'ready',
} as const;
export type TMrActOp = (typeof MR_ACT)[keyof typeof MR_ACT];

export interface IMrAct {
  op: TMrActOp;
  thread?: string;
  note?: number;
  draft?: number;
  body?: string;
  path?: string;
  oldPath?: string;
  line?: number;
  side?: TLineSide;
  paired?: number;
  refs?: IMrRefs;
  resolved?: boolean;
}

/** Why the runtime did not do it: no connection reaches the host, only the deployment's does (writes need the
 *  person's own), the token lacks a scope, or another failure. */
export type TMrRefusal =
  | { k: 'no-connection' }
  | { k: 'no-personal'; host: string }
  | { k: 'denied'; scope: string | null }
  | { k: 'failed'; text: string };

export type TMrAnswer =
  { ok: true; connection: IMrConnection; review: IMrReview } | { ok: false; refusal: TMrRefusal };

const HUNK = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

export interface ILineMap {
  /** The old number of a line of the new text the change did not touch; null for an added line. */
  oldOf: (line: number) => number | null;
  /** The new number of a line of the old text the change did not touch; null for a removed line. */
  newOf: (line: number) => number | null;
}

/** How the lines of the two versions of a file pair, read from its patch: inside a hunk by its lines, after a hunk by
 *  the shift it left (before the first one the versions are the same). */
export const lineMap = (patch: string): ILineMap => {
  const newToOld = new Map<number, number | null>();
  const oldToNew = new Map<number, number | null>();
  const ends: { oldEnd: number; newEnd: number }[] = [];
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;
  const close = () => {
    if (inHunk) ends.push({ oldEnd: oldLine, newEnd: newLine });
  };
  for (const raw of patch.split('\n')) {
    const hunk = HUNK.exec(raw);
    if (hunk) {
      close();
      oldLine = Number(hunk[1]);
      newLine = Number(hunk[2]);
      inHunk = true;
    } else if (!inHunk || raw.startsWith('\\')) {
      continue;
    } else if (raw.startsWith('diff --git')) {
      close();
      inHunk = false;
    } else if (raw.startsWith('+')) {
      newToOld.set(newLine++, null);
    } else if (raw.startsWith('-')) {
      oldToNew.set(oldLine++, null);
    } else {
      newToOld.set(newLine, oldLine);
      oldToNew.set(oldLine++, newLine++);
    }
  }
  close();
  const shift = (line: number, side: 'old' | 'new') => {
    let delta = 0;
    for (const e of ends) if (line >= (side === 'new' ? e.newEnd : e.oldEnd)) delta = e.newEnd - e.oldEnd;
    return delta;
  };
  return {
    oldOf: (line) => (newToOld.has(line) ? (newToOld.get(line) ?? null) : line - shift(line, 'new')),
    newOf: (line) => (oldToNew.has(line) ? (oldToNew.get(line) ?? null) : line + shift(line, 'old')),
  };
};

/** The act that keeps a new draft note at a line of a file, at the refs the person saw. */
export const draftAt = (
  file: Pick<IReviewFile, 'path' | 'oldPath' | 'patch'>,
  side: TLineSide,
  line: number,
  refs: IMrRefs,
  body: string,
): IMrAct => {
  const map = lineMap(file.patch);
  const paired = side === LINE_SIDE.after ? map.oldOf(line) : map.newOf(line);
  return {
    op: MR_ACT.draft,
    path: file.path,
    oldPath: file.oldPath || file.path,
    line,
    side,
    refs,
    body: body.trim(),
    ...(paired ? { paired } : {}),
  };
};

/** The threads at lines of a file that the diff shows now (outdated ones are listed apart). */
export const threadsOf = (review: IMrReview, path: string): IMrThread[] =>
  review.threads.filter((t) => t.path === path && t.line && t.side && !t.outdated);

/** The draft notes that start a thread at a line of a file. */
export const draftsAt = (review: IMrReview, path: string): IMrDraft[] =>
  review.drafts.filter((d) => !d.replyTo && d.path === path && d.line && d.side);

/** The draft replies to a thread. */
export const repliesOf = (review: IMrReview, thread: string): IMrDraft[] =>
  review.drafts.filter((d) => d.replyTo === thread);

/** How many discussions of a file wait for an answer, for the tree's marks. */
export const openThreadsByFile = (review: IMrReview): Map<string, number> => {
  const out = new Map<string, number>();
  for (const t of review.threads)
    if (t.path && t.resolvable && !t.resolved) out.set(t.path, (out.get(t.path) ?? 0) + 1);
  return out;
};
