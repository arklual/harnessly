// The order the «Ревью кода» app walks a change in: the tests first (they say what the change means to do), the code
// after them in the tree's order, the lock files and generated files last (they are seldom read). «Отметить
// просмотренным и дальше» goes to the next file of this order that is not viewed yet.
import type { IReviewFile } from '@/next/features/review/model';

const TEST_RE = /(^|\/)(__tests__|tests?|spec|e2e)\/|[._-](test|spec)\.[a-z0-9]+$|_test\.go$/i;
const LOCK_RE =
  /(^|\/)(package-lock\.json|pnpm-lock\.yaml|yarn\.lock|go\.sum|Cargo\.lock|poetry\.lock|Gemfile\.lock|composer\.lock|bun\.lockb?|uv\.lock)$|\.gen\.[a-z0-9]+$|(^|\/)__snapshots__\//i;

export const FILE_RANK = { test: 0, code: 1, lock: 2 } as const;
export type TFileRank = (typeof FILE_RANK)[keyof typeof FILE_RANK];

export const rankOf = (path: string): TFileRank => {
  if (LOCK_RE.test(path)) return FILE_RANK.lock;
  if (TEST_RE.test(path)) return FILE_RANK.test;
  return FILE_RANK.code;
};

/** The files in the walking order; within one rank, the order they came in (the tree's). */
export const reviewOrder = (files: readonly IReviewFile[]): IReviewFile[] =>
  files
    .map((file, index) => ({ file, index, rank: rankOf(file.path) }))
    .sort((a, b) => a.rank - b.rank || a.index - b.index)
    .map((x) => x.file);

/** The next file after `current` that is not viewed, going round to the start; null when all are viewed. */
export const nextUnviewed = (
  order: readonly IReviewFile[],
  current: string | null,
  viewed: (file: IReviewFile) => boolean,
): IReviewFile | null => {
  const at = order.findIndex((f) => f.path === current);
  for (let i = 1; i <= order.length; i += 1) {
    const file = order[(at + i) % order.length];
    if (file && file.path !== current && !viewed(file)) return file;
  }
  return null;
};

/** The file next to `current` in the order, one way or the other; null at the ends. */
export const neighbour = (
  order: readonly IReviewFile[],
  current: string | null,
  dir: 1 | -1,
): IReviewFile | null => {
  const at = order.findIndex((f) => f.path === current);
  if (at < 0) return order[0] ?? null;
  return order[at + dir] ?? null;
};
