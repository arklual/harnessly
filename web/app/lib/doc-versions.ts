import { DOC_DIFF_KIND, type IDocDiffBlock, type IDocVersion } from '@/types/doc-threads';

const HEX_RE = /^[0-9a-f]{7,40}$/i;
/** An id of the artifacts service: opaque, letters, digits and dashes, up to 64 characters; or a version number. */
const VERSION_REF_RE = /^[A-Za-z0-9][A-Za-z0-9-]{0,63}$/;
const VERSION_NUMBER_RE = /^[1-9]\d{0,5}$/;
/** The shortest id prefix the artifacts service resolves; shorter digits are a version number. */
const MIN_PREFIX = 7;

/**
 * A version as the reader takes it in `?v=`, `from` and `to`: a commit hash of a git-backed portal, an opaque id of
 * the artifacts service, a prefix of either, or a version number. Hashes are lowercased, since the portal matches
 * them as stored; anything else is passed as given.
 */
export const parseVersionRef = (value: string | null | undefined): string | null => {
  const raw = (value ?? '').trim();
  if (HEX_RE.test(raw)) return raw.toLowerCase();
  return VERSION_REF_RE.test(raw) ? raw : null;
};

/** The `?v=` of the viewer's address, or null when it is absent or not a version reference. */
export const parseVersionParam = (value: string | string[] | undefined): string | null =>
  typeof value === 'string' ? parseVersionRef(value) : null;

/** The id to pass back for a version: the service's own id, or the commit hash of the portal, which has no other. */
export const versionId = (version: IDocVersion): string => version.id ?? version.sha;

/** A version by full id, by the prefix of a shared link, or by its number. */
export const findVersion = (versions: IDocVersion[], ref: string | null): IDocVersion | null => {
  if (!ref) return null;
  const exact = versions.find((v) => versionId(v) === ref || v.sha === ref);
  if (exact) return exact;
  if (ref.length >= MIN_PREFIX) {
    const prefixed = versions.filter((v) => versionId(v).startsWith(ref));
    if (prefixed.length === 1) return prefixed[0];
  }
  return VERSION_NUMBER_RE.test(ref) ? (versions.find((v) => v.n === Number(ref)) ?? null) : null;
};

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
