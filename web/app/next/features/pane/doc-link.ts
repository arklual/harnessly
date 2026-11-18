// A document of the hub's reader (`/agents/docs/<slug>`, a link of the feed or the agent's `open`) opens in the pane in
// the «Документы» app's own view, as it opens on the whole screen: the document drawn by the app (headings,
// tables), its comments bound to the chat the pane is of. The reader stays for what only it does: a version, a thread,
// a document of another service (an alias `x-…`); and where the app is not here (not installed, turned off).
// The same document has more addresses than the reader's: the portal's `https://<artifacts>/a/<slug>` (a chip, a card of
// what a turn made, «Документы» of the right column, the agent's `open`) is the same slug, so it opens in the same app tab;
// before HRN-1647 it fell through to the old documents tab of the pane and one document stood in two views.
import type { IInstalledApp } from '@/next/features/app-views/api';
import { boardPlaceOf, type IBoardPlace } from '@/next/features/pane/board-link';
import { appConfig, type IAppConfig } from '@/platform/config';

/** The documents app's id, as its package names it (platform/docs-redirect DOCS_APP). */
export const DOCS_APP = 'harnessly.docs';

const DOC_PATH_RE = /^(?:\/[^/]+)*\/docs\/([^/]+)\/?$/;
const SLUG_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const READER_ONLY = ['v', 'thread', 'focus', 'legacy'] as const;
/** A document of another service, as the runtime names it (internal/docprovider alias.go). */
const ALIAS_PREFIX = 'x-';

const PORTAL_DOC_RE = /^\/a\/([^/]+)\/?$/;

const originOf = (address: string | null): string | null => {
  if (!address) return null;
  try {
    return new URL(address.replace('{slug}', '')).origin;
  } catch {
    return null;
  }
};

/** The document's slug of a reader's address of this page's origin (or a path), or of the portal's `/a/<slug>` of the
 *  installation's documents server; null for any other. */
export const docSlugOf = (
  target: string,
  origin: string,
  config: Pick<IAppConfig, 'artifactsUrl'> = appConfig(),
): string | null => {
  let url: URL;
  try {
    url = new URL(target, origin);
  } catch {
    return null;
  }
  if (READER_ONLY.some((key) => url.searchParams.has(key))) return null;
  const portal = originOf(config.artifactsUrl);
  const re =
    url.origin === new URL(origin).origin
      ? DOC_PATH_RE
      : portal !== null && url.origin === portal
        ? PORTAL_DOC_RE
        : null;
  const match = re?.exec(url.pathname);
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
  config?: Pick<IAppConfig, 'artifactsUrl'>,
): IBoardPlace | null => {
  const slug = docSlugOf(target, origin, config);
  if (slug === null || !apps) return null;
  const app = apps.find((a) => a.id === DOCS_APP);
  if (!app || app.muted || app.off || (app.state !== undefined && app.state !== 'ready')) return null;
  const view = app.views.find((v) => v.targets.includes('pane'));
  return view ? { app: app.id, view: view.id, slug } : null;
};

/**
 * The one rule of the pane for a document or a board: whatever opened it (the agent's `open`, a chip, a card
 * of the turn, «Документы» of the right column, the old documents list), a board goes to the boards app's tab and a
 * document to the documents app's, one tab each, which comes forward with the object when it is there. Null leaves
 * the address to its old tab (the app is not here, a version or a thread of the reader, another service's document).
 */
export const objectPlaceOf = (
  target: string,
  apps: readonly IInstalledApp[] | null | undefined,
  origin: string,
  config?: Pick<IAppConfig, 'artifactsUrl' | 'boardsUrl'>,
): IBoardPlace | null => boardPlaceOf(target, apps, config) ?? docPlaceOf(target, apps, origin, config);
