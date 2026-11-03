// The documentation page's rules apart from the screen: the anchors, the links inside the section, the filter.
import type { IGuidePage } from '@/next/features/help/api';

/** The anchor of a heading as GitHub makes it, which is how the guide's own `#links` are written. */
export const slugOf = (text: string): string =>
  text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .replace(/\s/g, '-');

export const LINK = { page: 'page', anchor: 'anchor', outside: 'outside', plain: 'plain' } as const;

export type TGuideLink =
  | { kind: typeof LINK.page; name: string; anchor: string | null }
  | { kind: typeof LINK.anchor; anchor: string }
  | { kind: typeof LINK.outside; href: string }
  | { kind: typeof LINK.plain };

const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

/** The name of a page from a relative link to its file: `01-first-plugin.md` is first-plugin (as docs.PageName). */
const pageNameOf = (file: string): string => {
  const parts = file.split('/');
  const base = (parts.pop() ?? '').replace(/\.md$/, '');
  // The reference is one folder up from the guide, and two from its translation.
  if (base === 'apps' && parts.length > 0 && parts.every((part) => part === '..')) return 'reference';
  if (base === 'README') return 'overview';
  const [num, ...rest] = base.split('-');
  return num !== undefined && /^\d+$/.test(num) && rest.length > 0 ? rest.join('-') : base;
};

/** The Markdown renderer percent-encodes a link, and a heading's id is plain text: `#уровни` has to match it as written. */
const plainAnchor = (hash: string): string => {
  try {
    return decodeURIComponent(hash);
  } catch {
    return hash;
  }
};

/**
 * What a link of a page leads to. A link to another page of the guide, or to a heading, stays inside the section; an
 * outside one opens as it is; anything else (a folder, an example's source, a file the guide does not carry) is text.
 */
export const resolveLink = (href: string, pages: readonly IGuidePage[]): TGuideLink => {
  if (href.startsWith('#'))
    return href.length > 1 ? { kind: LINK.anchor, anchor: plainAnchor(href.slice(1)) } : { kind: LINK.plain };
  if (SCHEME.test(href)) return { kind: LINK.outside, href };
  const [file = '', hash = ''] = href.split('#');
  if (!file.endsWith('.md')) return { kind: LINK.plain };
  const name = pageNameOf(file);
  if (!pages.some((page) => page.name === name)) return { kind: LINK.plain };
  return { kind: LINK.page, name, anchor: hash === '' ? null : plainAnchor(hash) };
};

export interface IContentsRow {
  page: IGuidePage;
  /** The headings that matched the query; none without one. */
  headings: readonly string[];
}

/** The contents under the filter: the pages whose title matched, or that have a heading that did. */
export const filterContents = (pages: readonly IGuidePage[], query: string): IContentsRow[] => {
  const needle = query.trim().toLowerCase();
  if (needle === '') return pages.map((page) => ({ page, headings: [] }));
  const rows: IContentsRow[] = [];
  for (const page of pages) {
    const titled = page.title.toLowerCase().includes(needle);
    const headings = page.headings.filter((h) => h.toLowerCase().includes(needle));
    if (titled || headings.length > 0) rows.push({ page, headings });
  }
  return rows;
};
