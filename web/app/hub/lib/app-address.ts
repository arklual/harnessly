// The page of an app lives on a machine: `/m/<machine>/apps/<id>` on the hub. A link of the machine's own
// page without that prefix (`/apps/<id>`, as the apps guide writes it) comes to the hub; it leads to the person's
// machine — the one online in their last space, else the first of it.
import type { Machine } from './types';

/** The app's page under a machine; the query (`?chat=`) goes along. */
export const appPathOn = (machineId: string, appId: string, search = ''): string =>
  `/m/${encodeURIComponent(machineId)}/apps/${encodeURIComponent(appId)}${search.startsWith('?') ? search : ''}`;

/** The machine an app's page opens on: the first online one, else the first; none for a space without machines. */
export const machineForApp = (machines: readonly Pick<Machine, 'id' | 'status'>[]): string | null =>
  (machines.find((m) => m.status === 'online') ?? machines[0])?.id ?? null;
