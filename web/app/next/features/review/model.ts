// What the review screen shows, whatever it came from (a merge request, a commit, the work of a chat): the list of
// changed files with their patches, the change's metadata, and a way to load both whole versions of one file when it
// is opened. The sources (sources.ts) turn the runtime's answers into this shape; the screen knows nothing else.

export const FILE_STATUS = {
  added: 'added',
  deleted: 'deleted',
  renamed: 'renamed',
  modified: 'modified',
} as const;
export type TFileStatus = (typeof FILE_STATUS)[keyof typeof FILE_STATUS];

export interface IReviewFile {
  /** The file's place in the change: its new path, or the old one for a deleted file. Unique within a change. */
  path: string;
  oldPath: string;
  status: TFileStatus;
  additions: number;
  deletions: number;
  binary: boolean;
  /** This file's part of the unified diff, from its `diff --git` line; empty when the source gave none. */
  patch: string;
  /** A hash of what changed in the file: a new version of the file is a new value. */
  version: string;
}

/** Both versions of a file for the diff view. */
export interface IFileContent {
  before: string;
  after: string;
  /** True for whole files; false when the sides are rebuilt from the patch and the unchanged lines between hunks
   *  are unknown (shown blank). */
  complete: boolean;
}

export type TPipelineStatus =
  'running' | 'pending' | 'success' | 'failed' | 'canceled' | 'skipped' | 'manual' | 'created' | 'other';

export const LINE_SIDE = { before: 'before', after: 'after' } as const;
export type TLineSide = (typeof LINE_SIDE)[keyof typeof LINE_SIDE];

export interface IReviewCommit {
  sha: string;
  title: string;
  author: string | null;
  at: string | null;
}

export interface IMrMeta {
  kind: 'mr';
  provider: 'gitlab' | 'github';
  url: string;
  project: string;
  /** `!42`, `#42`. */
  ref: string;
  title: string;
  author: string | null;
  sourceBranch: string | null;
  targetBranch: string | null;
  state: string | null;
  draft: boolean;
  pipeline: { status: TPipelineStatus; url: string | null; failedJobUrl: string | null } | null;
  approvedBy: string[];
  approvalsLeft: number | null;
  reviewers: string[];
  labels: string[];
  commits: IReviewCommit[];
  /** How many commits the target branch has that the source does not; null when unknown. */
  behind: number | null;
  conflicts: boolean;
}

export interface ICommitMeta {
  kind: 'commit';
  provider: 'gitlab' | 'github' | 'local';
  url: string | null;
  project: string;
  ref: string;
  title: string;
  author: string | null;
  at: string | null;
}

export interface ILocalMeta {
  kind: 'local';
  repo: string;
  root: string;
  branch: string | null;
  /** What the diff is taken against: `HEAD` for uncommitted work, a commit for a range. */
  base: string | null;
  commits: IReviewCommit[];
  uncommitted: boolean;
}

export type TReviewMeta = IMrMeta | ICommitMeta | ILocalMeta;

export interface IReviewSource {
  /** Changes when the source is another change; the screen's per-change state resets on a new key. */
  key: string;
  files: readonly IReviewFile[];
  /** The source cut the list of files (too many, too long). */
  truncated: boolean;
  meta: TReviewMeta;
  /** Both whole versions of a file; null when the source cannot give them (the patch alone is shown then). */
  loadContent?: (file: IReviewFile) => Promise<IFileContent | null>;
  /** An address of one line to copy: a link to the forge, or `path:line` for local work. */
  lineLink?: (file: IReviewFile, line: number, side: TLineSide) => string;
}

/** A discussion of a merge request: a comment on a line or on the whole change, with its replies. */
export interface IReviewThread {
  id: string;
  /** null for what cannot be resolved. */
  resolved: boolean | null;
  path: string | null;
  line: number | null;
  /** The line is no longer in the diff. */
  outdated: boolean;
  notes: { author: string | null; at: string | null; body: string }[];
}
