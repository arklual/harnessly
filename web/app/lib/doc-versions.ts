import { DOC_DIFF_KIND, type IDocDiffBlock, type IDocVersion } from '@/types/doc-threads';

/** Hex, 7 to 40 characters: what the gateway accepts as a version in `?v=`. */
const SHA_RE = /^[0-9a-f]{7,40}$/;

/** The `?v=` of the viewer's address, or null when it is absent or not a version hash. */
export const parseVersionParam = (value: string | string[] | undefined): string | null => {
  const raw = typeof value === 'string' ? value.toLowerCase() : '';
  return SHA_RE.test(raw) ? raw : null;
};

/** A version by full hash or by the prefix of a shared link. */
export const findVersion = (versions: IDocVersion[], sha: string | null): IDocVersion | null =>
  sha ? (versions.find((v) => v.sha === sha) ?? versions.find((v) => v.sha.startsWith(sha)) ?? null) : null;

export const currentVersion = (versions: IDocVersion[]): IDocVersion | null =>
  versions.find((v) => v.current) ?? null;

/** The version published right before `version`, or null for the first one. */
export const previousVersion = (versions: IDocVersion[], version: IDocVersion): IDocVersion | null =>
  versions.find((v) => v.n === version.n - 1) ?? null;

/** A comparison always reads older → newer, whichever way the person picked the two. */
export const orderedPair = (a: IDocVersion, b: IDocVersion): { from: IDocVersion; to: IDocVersion } =>
  a.n <= b.n ? { from: a, to: b } : { from: b, to: a };

export interface IDiffView {
  /** Blocks worth showing: changed ones, with every unchanged run folded into one marker. */
  rows: ({ kind: 'added' | 'removed'; text: string } | { kind: 'same'; hidden: number })[];
  empty: boolean;
}

/** Folds the unchanged blocks, so the list shows what changed and how much text sits between the changes. */
export const foldDiff = (blocks: IDocDiffBlock[]): IDiffView => {
  const rows: IDiffView['rows'] = [];
  let hidden = 0;
  for (const block of blocks) {
    if (block.kind === DOC_DIFF_KIND.same) {
      hidden += 1;
      continue;
    }
    if (hidden) rows.push({ kind: DOC_DIFF_KIND.same, hidden });
    hidden = 0;
    rows.push({ kind: block.kind, text: block.text });
  }
  if (hidden && rows.length) rows.push({ kind: DOC_DIFF_KIND.same, hidden });
  return { rows, empty: rows.length === 0 };
};

/** The first words of a changed block: enough to find it in the page, short enough to survive inline markup. */
export const blockQuote = (text: string, max = 120): string => text.replace(/\s+/g, ' ').trim().slice(0, max);
