// A document of the hub's reader (`/agents/docs/<slug>`, a link of the feed or the agent's `open`) opens in the pane in
// the «Документы» app's own view, as it opens on the whole screen: the document drawn by the app (headings,
// tables), its comments bound to the chat the pane is of. The reader stays for what only it does: a version, a thread,
// a document of another service (an alias `x-…`); and where the app is not here (not installed, turned off).
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { IBoardPlace } from '@/next/features/pane/board-link';

/** The documents app's id, as its package names it (platform/docs-redirect DOCS_APP). */
export const DOCS_APP = 'harnessly.docs';

const DOC_PATH_RE = /^(?:\/[^/]+)*\/docs\/([^/]+)\/?$/;
const SLUG_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const READER_ONLY = ['v', 'thread', 'focus', 'legacy'] as const;
/** A document of another service, as the runtime names it (internal/docprovider alias.go). */
const ALIAS_PREFIX = 'x-';

/** The document's slug of a reader's address of this page's origin (or a path), or null. */
export const docSlugOf = (target: string, origin: string): string | null => {
  let url: URL;
  try {
    url = new URL(target, origin);
  } catch {
    return null;
  }
  if (url.origin !== new URL(origin).origin) return null;
  if (READER_ONLY.some((key) => url.searchParams.has(key))) return null;
  const match = DOC_PATH_RE.exec(url.pathname);
  if (!match) return null;
  let slug: string;
  try {
    slug = decodeURIComponent(match[1] ?? '');
  } catch {
    return null;
  }
  return SLUG_RE.test(slug) && !slug.startsWith(ALIAS_PREFIX) ? slug : null;
};

/** Where a document opens in the pane: the documents app's view for the pane and the document's slug; null when the
 *  address is no document of the hub's or the app cannot show it here. */
export const docPlaceOf = (
  target: string,
  apps: readonly IInstalledApp[] | null | undefined,
  origin: string,
): IBoardPlace | null => {
  const slug = docSlugOf(target, origin);
  if (slug === null || !apps) return null;
  const app = apps.find((a) => a.id === DOCS_APP);
  if (!app || app.muted || app.off || (app.state !== undefined && app.state !== 'ready')) return null;
  const view = app.views.find((v) => v.targets.includes('pane'));
  return view ? { app: app.id, view: view.id, slug } : null;
};
