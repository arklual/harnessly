// What the person chose to see on the Apps screen, kept in the browser (per device): the app that was open, how long a
// row stands still before it is called stale, and a few switches per app. Every read and write is in try/catch: with
// storage blocked the screen works the same, it only forgets on reload.
import { createStore, useStore } from '@/next/core/store';
import { APP_REGISTRY, MR_SCOPES, type MrScope } from '@/next/features/apps/model';
import type { AppId } from '@/next/protocol/v2';

const KEY = 'hx-next:apps';

/** The terms a stale row can be called after, in days. */
export const STALE_CHOICES = [3, 5, 10, 14] as const;

export interface IAppsPrefs {
  /** The app that was open last. */
  app: AppId | null;
  /** Null: the number the machine sent. */
  staleDays: (typeof STALE_CHOICES)[number] | null;
  /** Tracker: finished work of the last month is listed too. */
  showDone: boolean;
  /** Tracker: epics open with their subtasks. */
  openEpics: boolean;
  /** GitLab: drafts stay out of the list unless asked for. */
  hideDrafts: boolean;
  /** GitLab: the view the screen opens on. */
  scope: MrScope;
}

export const DEFAULT_PREFS: IAppsPrefs = {
  app: null,
  staleDays: null,
  showDone: false,
  openEpics: false,
  hideDrafts: false,
  scope: 'all',
};

const read = (): IAppsPrefs => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    if (typeof raw !== 'object' || raw === null) return DEFAULT_PREFS;
    const saved = raw as Partial<Record<keyof IAppsPrefs, unknown>>;
    const flag = (name: 'showDone' | 'openEpics' | 'hideDrafts') =>
      typeof saved[name] === 'boolean' ? saved[name] : DEFAULT_PREFS[name];
    const stale = STALE_CHOICES.find((days) => days === saved.staleDays) ?? null;
    return {
      app: APP_REGISTRY.find((known) => known.id === saved.app)?.id ?? null,
      staleDays: stale,
      showDone: flag('showDone'),
      openEpics: flag('openEpics'),
      hideDrafts: flag('hideDrafts'),
      scope: MR_SCOPES.find((scope) => scope === saved.scope) ?? DEFAULT_PREFS.scope,
    };
  } catch {
    return DEFAULT_PREFS;
  }
};

const store = createStore<IAppsPrefs>(read());

export const usePrefs = (): IAppsPrefs => useStore(store);

export const setPrefs = (patch: Partial<IAppsPrefs>): void => {
  const next = { ...store.get(), ...patch };
  store.set(next);
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // Private mode or full storage: the choice holds until the page is closed.
  }
};
