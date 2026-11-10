// The page of an app lives on a machine: `/m/<machine>/apps/<id>` on the hub. A link of the machine's own
// page without that prefix (`/apps/<id>`, as the apps guide writes it) comes to the hub; it leads to the person's
// machine — the one online in their last space, else the first of it.
import type { Machine, SpaceApp } from './types';

/** The app's page under a machine; the query (`?chat=`) goes along. */
export const appPathOn = (machineId: string, appId: string, search = ''): string =>
  `/m/${encodeURIComponent(machineId)}/apps/${encodeURIComponent(appId)}${search.startsWith('?') ? search : ''}`;

/** The machine an app's page opens on: the first online one, else the first; none for a space without machines. */
export const machineForApp = (machines: readonly Pick<Machine, 'id' | 'status'>[]): string | null =>
  (machines.find((m) => m.status === 'online') ?? machines[0])?.id ?? null;

/** Where `/apps/<id>` leads. */
export type TAppTarget = { machine: string } | { hub: string } | null;

/**
 * The app's page on a machine that is online; with none online, its page at the hub when the space installed it
 * (the hub serves its view from the store); else the page on a machine that is off, which says so; none in a
 * space without machines and without the app.
 */
export const appTarget = (
  machines: readonly Pick<Machine, 'id' | 'status'>[],
  appId: string,
  spaceApps: readonly Pick<SpaceApp, 'id' | 'path'>[],
): TAppTarget => {
  const online = machines.find((m) => m.status === 'online');
  if (online) return { machine: online.id };
  const atHub = spaceApps.find((a) => a.id === appId)?.path;
  if (atHub) return { hub: atHub };
  const any = machineForApp(machines);
  return any ? { machine: any } : null;
};
