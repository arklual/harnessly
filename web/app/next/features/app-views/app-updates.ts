// A new version of an app reaches the machine by itself (the environment follows the app's branch); a view
// that is open then starts anew on the new version, with what it had open (its data.object), so the person keeps
// their board and the place in it. While a view is on screen the page reads the machine's apps now and then; one read
// per machine serves every view of it.
import { loadInstalledApps, refreshInstalledApps, type IInstalledApp } from '@/next/features/app-views/api';
import type { MachineId } from '@/next/protocol/v2';

/** How often a page with an app's view on screen looks for a new version of its apps. */
export const UPDATE_LOOK_MS = 3 * 60_000;

/** What tells two installs of an app apart: its version and when that version was put on the machine. */
export const appStamp = (app: Pick<IInstalledApp, 'version' | 'updatedAt'>): string =>
  `${app.version}@${app.updatedAt ?? 0}`;

const looks = new Map<MachineId, { at: number; asked: Promise<readonly IInstalledApp[]> }>();

/** The machine's apps, read at most once per UPDATE_LOOK_MS for all the views of the page. */
const lookAt = (m: MachineId, now: number): Promise<readonly IInstalledApp[]> => {
  const was = looks.get(m);
  if (was && now - was.at < UPDATE_LOOK_MS - 1_000) return was.asked;
  const asked = loadInstalledApps(m).then(
    (list) => list.apps,
    () => {
      looks.delete(m);
      return [];
    },
  );
  looks.set(m, { at: now, asked });
  return asked;
};

/** Whether the machine has another install of the app than the page shows: a new version, or the same version put
 *  there again from new files. An app that is gone says nothing: its view says so by itself. */
export const updated = (shown: IInstalledApp, now: readonly IInstalledApp[]): boolean => {
  const fresh = now.find((a) => a.id === shown.id);
  return fresh !== undefined && appStamp(fresh) !== appStamp(shown);
};

/** Looks for a new version of the app while its view is on screen; a new one reads the machine's apps again for every
 *  screen, and the views start anew on it. Answers the stop. */
export const watchAppUpdate = (m: MachineId, shown: IInstalledApp, every = UPDATE_LOOK_MS): (() => void) => {
  let live = true;
  const look = () => {
    if (!live || (typeof document !== 'undefined' && document.visibilityState !== 'visible')) return;
    void lookAt(m, Date.now()).then((now) => {
      if (live && updated(shown, now)) refreshInstalledApps(m);
    });
  };
  const timer = setInterval(look, every);
  return () => {
    live = false;
    clearInterval(timer);
  };
};

/** Forgets the reads (tests). */
export const resetAppUpdates = (): void => looks.clear();
