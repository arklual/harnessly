// «Приложения» of the search (⌘K): the commands of the apps on in the chat on screen, and the installed apps that
// have a page or a pane view of their own. A row's id says what it does: `appcmd:<chat>|<text>` puts the command into
// that chat's field, `appopen:<app>` opens the app on its own page, `apppane:<app>/<view>` opens its view in the pane.
import type { IInstalledApp } from '@/next/features/app-views/api';
import { foldText } from '@/next/features/search/highlight';
import type { ISearchGroup, ISearchRow } from '@/next/features/search/search-dialog';
import { t } from '@/next/i18n';

export const APP_ROW = { command: 'appcmd:', open: 'appopen:', pane: 'apppane:' } as const;

const pluginOf = (id: string): string => id.replaceAll('.', '-');

const matches = (words: readonly string[], ...texts: string[]) => {
  const all = foldText(texts.join(' '));
  return words.every((w) => all.includes(w));
};

export const appSearchGroup = (
  installed: readonly IInstalledApp[],
  chat: { id: string; apps: readonly string[] } | null,
  query: string,
): ISearchGroup | null => {
  const words = foldText(query).trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return null;
  const rows: ISearchRow[] = [];
  for (const id of chat?.apps ?? []) {
    const app = installed.find((a) => a.id === id);
    for (const c of app?.commands ?? []) {
      if (!app || !matches(words, c.name, c.description, app.name)) continue;
      rows.push({
        id: `${APP_ROW.command}${chat?.id}|/${pluginOf(app.id)}:${c.name} `,
        kind: 'app',
        icon: 'square-slash',
        title: `/${c.name}`,
        snippet: c.description || undefined,
        meta: app.name,
      });
    }
  }
  for (const app of installed) {
    if (!matches(words, app.name, app.description)) continue;
    const own = app.views.find((v) => v.targets.includes('app'));
    const pane = app.views.find((v) => v.targets.includes('pane'));
    if (own)
      rows.push({
        id: `${APP_ROW.open}${app.id}`,
        kind: 'app',
        icon: app.icon ?? 'layout-grid',
        title: app.name,
        meta: t('next.search.appOpen'),
      });
    else if (pane)
      rows.push({
        id: `${APP_ROW.pane}${app.id}/${pane.id}`,
        kind: 'app',
        icon: app.icon ?? 'layout-grid',
        title: app.name,
        meta: t('next.search.appPane'),
      });
  }
  return rows.length > 0 ? { id: 'apps', label: t('next.search.groupApps'), rows } : null;
};
