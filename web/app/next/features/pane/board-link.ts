// A link to a board of the installation's boards service opens in the «Доски» app's view of the pane, on that board
// the view signs the person in by itself (no separate sign-in), shows what they select to the chat and
// lets the agent edit the board in front of them. The documents keep their reader (it reads through the hub).
// Without the app (not installed, turned off, not configured) the link opens as before.
import { boardSlugOf } from '@/next/features/chat/link-card';
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { IAppConfig } from '@/platform/config';

/** The app that opens the boards: its manifest id (internal/hub/env BoardsAppID). */
export const BOARDS_APP = 'harnessly.boards';

export interface IBoardPlace {
  app: string;
  view: string;
  slug: string;
}

/** Where a board link opens in the pane: the boards app's view for the pane and the board's slug; null when the
 *  link is no board of ours or the app cannot show it here. */
export const boardPlaceOf = (
  href: string,
  apps: readonly IInstalledApp[] | null | undefined,
  config?: Pick<IAppConfig, 'boardsUrl'>,
): IBoardPlace | null => {
  const slug = boardSlugOf(href, config);
  if (slug === null || !apps) return null;
  const app = apps.find((a) => a.id === BOARDS_APP);
  if (!app || app.muted || app.off || (app.state !== undefined && app.state !== 'ready')) return null;
  const view = app.views.find((v) => v.targets.includes('pane'));
  return view ? { app: app.id, view: view.id, slug } : null;
};
