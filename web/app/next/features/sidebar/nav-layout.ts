// The sidebar menu as the person laid it out (Settings → «Меню», kept in the person's settings, so every device has the
// same): the order of the items, the hidden ones, the apps raised to the top level beside Sinopia Design, and the order
// of the apps under «Приложения». Pure: the sidebar draws what this says, the settings page edits it.
import type { NavSettings } from '@/next/stores/me';

/** The menu's own items; an app's key is `app:<id>`. */
export const NAV_ITEM = {
  newChat: 'newChat',
  search: 'search',
  designly: 'designly',
  apps: 'apps',
} as const;
export type TNavItem = (typeof NAV_ITEM)[keyof typeof NAV_ITEM];

const ITEMS: readonly string[] = Object.values(NAV_ITEM);
const DEFAULT_ORDER: readonly TNavItem[] = [
  NAV_ITEM.newChat,
  NAV_ITEM.search,
  NAV_ITEM.designly,
  NAV_ITEM.apps,
];

/** The boards and the documents stand first under «Приложения» until the person orders the apps otherwise. */
export const PINNED_APPS: readonly string[] = ['harnessly.boards', 'harnessly.docs'];

const APP_PREFIX = 'app:';
export const appKey = (id: string): string => `${APP_PREFIX}${id}`;
export const appOfKey = (key: string): string | null =>
  key.startsWith(APP_PREFIX) ? key.slice(APP_PREFIX.length) : null;
export const isNavItem = (key: string): key is TNavItem => ITEMS.includes(key);

export interface INavLayout {
  /** The top level in order, hidden ones included: the menu's items and the apps raised there (`app:<id>`). */
  top: string[];
  /** The apps under «Приложения» in order (ids), hidden ones included; the raised ones are not here. */
  apps: string[];
  hidden: ReadonlySet<string>;
}

/**
 * The layout over what there is: `items` are the menu's items this screen has (no Sinopia Design without it), `apps`
 * the apps the menu may list, in their own order (activity). A key of the settings that names nothing here is skipped;
 * an item the settings do not name stands where the default puts it, an app goes after the ones named.
 */
export const navLayout = (
  nav: NavSettings,
  items: readonly TNavItem[],
  apps: readonly string[],
): INavLayout => {
  const have = new Set(apps);
  const raised = new Set<string>();
  const top: string[] = [];
  for (const key of nav.order) {
    if (top.includes(key)) continue;
    const app = appOfKey(key);
    if (app !== null ? have.has(app) : isNavItem(key) && items.includes(key)) {
      top.push(key);
      if (app !== null) raised.add(app);
    }
  }
  // An item missing from the saved order goes after the item it follows by default.
  DEFAULT_ORDER.forEach((item, i) => {
    if (!items.includes(item) || top.includes(item)) return;
    const before = DEFAULT_ORDER.slice(0, i)
      .reverse()
      .find((b) => top.includes(b));
    top.splice(before === undefined ? 0 : top.indexOf(before) + 1, 0, item);
  });
  const named = nav.apps.length > 0 ? nav.apps : PINNED_APPS;
  const under: string[] = [];
  for (const id of [...named, ...apps])
    if (have.has(id) && !raised.has(id) && !under.includes(id)) under.push(id);
  return { top, apps: under, hidden: new Set(nav.hidden) };
};

/** The settings that make a layout: what the settings page writes after a move. */
export const navSettingsOf = (layout: INavLayout): NavSettings => ({
  order: [...layout.top],
  hidden: [...layout.hidden],
  apps: [...layout.apps],
});

/** A key shown or hidden. */
export const withHidden = (layout: INavLayout, key: string, hidden: boolean): INavLayout => {
  const next = new Set(layout.hidden);
  if (hidden) next.add(key);
  else next.delete(key);
  return { ...layout, hidden: next };
};

/** An app raised to the top level (after the last item there) or put back first under «Приложения». */
export const withRaised = (layout: INavLayout, app: string, raised: boolean): INavLayout => {
  const key = appKey(app);
  if (raised)
    return {
      ...layout,
      top: [...layout.top.filter((k) => k !== key), key],
      apps: layout.apps.filter((a) => a !== app),
    };
  return {
    ...layout,
    top: layout.top.filter((k) => k !== key),
    apps: [app, ...layout.apps.filter((a) => a !== app)],
  };
};

/** A list with the entry at `from` moved to `to`. */
export const moved = <T>(list: readonly T[], from: number, to: number): T[] => {
  const next = [...list];
  const [item] = next.splice(from, 1);
  if (item !== undefined) next.splice(Math.max(0, Math.min(to, next.length)), 0, item);
  return next;
};
