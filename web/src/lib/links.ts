import type { Integrations } from '../api/integrations';

/** A board link: `<boards>/b/<slug>?k=<key>`; the key is the board's bearer secret. */
export interface BoardLink {
  kind: 'board';
  slug: string;
  key: string | null;
  /** The path and query to open on the boards server (what a sign-in hand-off lands on). */
  path: string;
  url: string;
}

/** A document link: `<artifacts>/a/<slug>` or `/embed/<slug>`, optionally `?v=<version>`. */
export interface DocumentLink {
  kind: 'document';
  slug: string;
  version: string | null;
  url: string;
  embedUrl: string;
}

export type ServiceLink = BoardLink | DocumentLink;

const BOARD_SLUG = /^\/b\/([a-z0-9][a-z0-9-]{2,63})\/?$/;
// Paths under /b that are the server's own pages, not boards.
const BOARD_ROUTES = new Set(['api', 'auth', 'assets', 'static', 'new']);
const DOCUMENT_SLUG = /^\/(?:a|embed)\/([a-z0-9][a-z0-9-]{1,79})\/?$/;

function parse(href: string): URL | null {
  try {
    return new URL(href);
  } catch {
    return null;
  }
}

/** Recognises a link to the configured boards server or artifacts catalog; null for anything else. */
export function recognize(href: string, services: Integrations): ServiceLink | null {
  const u = parse(href.trim());
  if (!u || (u.protocol !== 'https:' && u.protocol !== 'http:')) return null;
  if (services.boards && u.origin === new URL(services.boards.url).origin) {
    const m = BOARD_SLUG.exec(u.pathname);
    if (!m?.[1] || BOARD_ROUTES.has(m[1])) return null;
    const key = u.searchParams.get('k');
    const q = new URLSearchParams();
    if (key) q.set('k', key);
    const at = u.searchParams.get('at');
    if (at) q.set('at', at);
    const query = q.toString();
    const path = `/b/${m[1]}${query ? `?${query}` : ''}`;
    return { kind: 'board', slug: m[1], key, path, url: services.boards.url + path };
  }
  if (services.artifacts && u.origin === new URL(services.artifacts.url).origin) {
    const m = DOCUMENT_SLUG.exec(u.pathname);
    if (!m?.[1]) return null;
    const version = u.searchParams.get('v');
    const v = version ? `?v=${encodeURIComponent(version)}` : '';
    return {
      kind: 'document',
      slug: m[1],
      version,
      url: `${services.artifacts.url}/a/${m[1]}${v}`,
      embedUrl: `${services.artifacts.url}/embed/${m[1]}${v}`,
    };
  }
  return null;
}

/** A readable name for a link until its service has said more: the slug without its random suffix. */
export function linkTitle(link: ServiceLink): string {
  const words = link.slug.split('-');
  if (words.length > 1 && /\d/.test(words.at(-1) ?? '')) words.pop();
  const text = words.join(' ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** The embed address with the page's theme, for the document pane. */
export function themedEmbed(embedUrl: string, theme: 'light' | 'dark'): string {
  const u = new URL(embedUrl);
  u.searchParams.set('theme', theme);
  return u.toString();
}
