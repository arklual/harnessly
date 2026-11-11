// F7 / Shift+F7: the next or the previous change. Within a file it walks the changed chunks by their first line; past the
// last one it moves to the next file of the tree (its first change), before the first one to the previous file (its
// last change), as IntelliJ does.
import type { IReviewFile } from '@/next/features/review/model';

export const NAV_DIR = { next: 1, prev: -1 } as const;
export type TNavDir = (typeof NAV_DIR)[keyof typeof NAV_DIR];

export const FILE_EDGE = { first: 'first', last: 'last' } as const;
export type TFileEdge = (typeof FILE_EDGE)[keyof typeof FILE_EDGE];

export type TNavTarget =
  { kind: 'line'; line: number } | { kind: 'file'; path: string; edge: TFileEdge } | null;

/** The first line of the next (or previous) change strictly after (or before) the cursor line; null past the end. */
export const nextChangeLine = (
  changeLines: readonly number[],
  cursor: number,
  dir: TNavDir,
): number | null => {
  const sorted = [...changeLines].sort((a, b) => a - b);
  if (dir === NAV_DIR.next) return sorted.find((line) => line > cursor) ?? null;
  for (let i = sorted.length - 1; i >= 0; i--) if (sorted[i]! < cursor) return sorted[i]!;
  return null;
};

/** The file next to `current` in the tree's order, or null at the end. */
export const neighbourFile = (
  order: readonly IReviewFile[],
  current: string | null,
  dir: TNavDir,
): IReviewFile | null => {
  const at = order.findIndex((file) => file.path === current);
  if (at < 0) return dir === NAV_DIR.next ? (order[0] ?? null) : (order.at(-1) ?? null);
  return order[at + dir] ?? null;
};

interface INavigateInput {
  changeLines: readonly number[];
  /** The cursor line; for a file just opened, 0 (before everything). */
  cursor: number;
  order: readonly IReviewFile[];
  current: string | null;
  dir: TNavDir;
}

/** Where F7 or Shift+F7 goes from here. */
export const navigate = ({ changeLines, cursor, order, current, dir }: INavigateInput): TNavTarget => {
  const line = nextChangeLine(changeLines, cursor, dir);
  if (line !== null) return { kind: 'line', line };
  const file = neighbourFile(order, current, dir);
  if (!file || file.path === current) return null;
  return { kind: 'file', path: file.path, edge: dir === NAV_DIR.next ? FILE_EDGE.first : FILE_EDGE.last };
};
