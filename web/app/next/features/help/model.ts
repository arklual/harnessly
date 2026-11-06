// The documentation page's rules apart from the screen: which page an address names, the anchors, the links and
// pictures of a page, the filter over the contents.
import type { IGuidePage, IGuideSection } from '@/next/features/help/api';

/** The anchor of a heading as GitHub makes it, which is how the pages' own `#links` are written. */
export const slugOf = (text: string): string =>
  text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .replace(/\s/g, '-');

/** The section of the apps guide, where an older address /help/<page> leads. */
export const APPS_SECTION = 'build-apps';

/** The apps guide's pages under their older names. */
const OLD_NAMES: Readonly<Record<string, string>> = { reference: 'apps' };

export interface IGuidePlace {
  section: IGuideSection;
  page: IGuidePage;
}

const firstOf = (sections: readonly IGuideSection[]): IGuidePlace | null => {
  for (const section of sections) {
    const page = section.pages[0];
    if (page) return { section, page };
  }
  return null;
};

const pageIn = (section: IGuideSection | undefined, name: string): IGuidePlace | null => {
  const page = section?.pages.find((p) => p.name === name);
  return section && page ? { section, page } : null;
};

/**
 * The page an address names: /help/<section>/<page>, /help/<section> (its first page), an older /help/<page> of the apps
 * guide, else the first page of the library.
 */
export const placeOf = (
  sections: readonly IGuideSection[],
  section: string | null,
  page: string | null,
): IGuidePlace | null => {
  const named = sections.find((s) => s.id === section);
  if (named) {
    const exact = page ? pageIn(named, page) : null;
    if (exact) return exact;
    const first = named.pages[0];
    if (first) return { section: named, page: first };
  }
  if (section && !named) {
    const old = OLD_NAMES[section] ?? section;
    const apps = pageIn(
      sections.find((s) => s.id === APPS_SECTION),
      old,
    );
    if (apps) return apps;
    for (const s of sections) {
      const found = pageIn(s, old);
      if (found) return found;
    }
  }
  return firstOf(sections);
};

export const LINK = { page: 'page', anchor: 'anchor', outside: 'outside', plain: 'plain' } as const;

export type TGuideLink =
  | { kind: typeof LINK.page; section: string; name: string; anchor: string | null }
  | { kind: typeof LINK.anchor; anchor: string }
  | { kind: typeof LINK.outside; href: string }
  | { kind: typeof LINK.plain };

const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

/** The Markdown renderer percent-encodes a link, and a heading's id is plain text: `#уровни` has to match it as written. */
const plainText = (encoded: string): string => {
  try {
    return decodeURIComponent(encoded);
  } catch {
    return encoded;
  }
};

/** A path relative to a page's file, made a path of the source; null when it climbs out of it. */
export const joinPath = (fromFile: string, relative: string): string | null => {
  const parts = fromFile.split('/').slice(0, -1);
  for (const part of plainText(relative).split('/')) {
    if (part === '' || part === '.') continue;
    if (part === '..') {
      if (parts.length === 0) return null;
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  return parts.join('/');
};

/**
 * What a link of a page leads to. A link to another page of the library (by its file, relative to the page, as the
 * Markdown is written for a repository), or to a heading, stays inside; an outside one opens as it is; anything else
 * (a folder, an example's source, a file the library does not carry) is text. A page of the same origin wins when two
 * sources have the same file.
 */
export const resolveLink = (
  href: string,
  from: IGuidePlace | null,
  sections: readonly IGuideSection[],
): TGuideLink => {
  if (href.startsWith('#'))
    return href.length > 1 ? { kind: LINK.anchor, anchor: plainText(href.slice(1)) } : { kind: LINK.plain };
  if (SCHEME.test(href))
    return /^https?:|^mailto:/i.test(href) ? { kind: LINK.outside, href } : { kind: LINK.plain };
  if (!from) return { kind: LINK.plain };
  const [file = '', hash = ''] = href.split('#');
  if (!file.endsWith('.md')) return { kind: LINK.plain };
  // A translation's links are relative to the translation (ru/...), and lead to an English path or a translated one.
  const target = joinPath(from.page.read || from.page.file, file);
  if (target === null) return { kind: LINK.plain };
  let found: IGuidePlace | null = null;
  for (const section of sections) {
    for (const page of section.pages) {
      if (page.file !== target && page.read !== target) continue;
      if (!found || (page.origin === from.page.origin && found.page.origin !== from.page.origin))
        found = { section, page };
    }
  }
  if (!found) return { kind: LINK.plain };
  return {
    kind: LINK.page,
    section: found.section.id,
    name: found.page.name,
    anchor: hash === '' ? null : plainText(hash),
  };
};

/** A picture of a page is one of its own source, by a relative path; an outside picture is not loaded. */
export const isOwnPicture = (src: string): boolean =>
  src !== '' && !SCHEME.test(src) && !src.startsWith('/') && !src.startsWith('//');

export interface IContentsRow {
  page: IGuidePage;
  /** The headings that matched the query; none without one. */
  headings: readonly string[];
}

export interface IContentsGroup {
  section: IGuideSection;
  rows: IContentsRow[];
}

/** The contents under the filter: per section, the pages whose title matched, or that have a heading that did. */
export const filterContents = (sections: readonly IGuideSection[], query: string): IContentsGroup[] => {
  const needle = query.trim().toLowerCase();
  const groups: IContentsGroup[] = [];
  for (const section of sections) {
    if (needle === '') {
      groups.push({ section, rows: section.pages.map((page) => ({ page, headings: [] })) });
      continue;
    }
    const all = section.title.toLowerCase().includes(needle);
    const rows: IContentsRow[] = [];
    for (const page of section.pages) {
      const titled = all || page.title.toLowerCase().includes(needle);
      const headings = page.headings.filter((h) => h.toLowerCase().includes(needle));
      if (titled || headings.length > 0) rows.push({ page, headings });
    }
    if (rows.length > 0) groups.push({ section, rows });
  }
  return groups;
};
