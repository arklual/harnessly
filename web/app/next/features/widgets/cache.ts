// The apps that have a widget, kept in this browser: the stack under the chat's right column used to wait
// for the machine's list of apps (a read that starts the view server) every time a chat opened, and drew nothing till
// then. The last list is shown at once and the live one replaces it when it comes; with nothing kept yet the cards are
// placeholders, not a blank.
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { MachineId } from '@/next/protocol/v2';

const KEY = 'hx-wg-apps:';
const VERSION = 1;

export interface ICachedSide {
  apps: IInstalledApp[];
  viewPort: number;
}

/** The apps with a view of the side target, as the list holds them. */
export const sideApps = (apps: readonly IInstalledApp[]): IInstalledApp[] =>
  apps.filter((a) => a.views.some((v) => v.targets.includes('side')));

export const readCachedSide = (machine: MachineId): ICachedSide | null => {
  try {
    const v: unknown = JSON.parse(window.localStorage.getItem(KEY + machine) ?? 'null');
    if (typeof v !== 'object' || v === null) return null;
    const { version, apps, viewPort } = v as Record<string, unknown>;
    if (version !== VERSION || !Array.isArray(apps) || typeof viewPort !== 'number') return null;
    return { apps: apps as IInstalledApp[], viewPort };
  } catch {
    return null;
  }
};

export const writeCachedSide = (
  machine: MachineId,
  apps: readonly IInstalledApp[],
  viewPort: number,
): void => {
  try {
    window.localStorage.setItem(
      KEY + machine,
      JSON.stringify({ version: VERSION, apps: sideApps(apps), viewPort }),
    );
  } catch {
    // A closed or full storage: the next open waits for the machine, as before.
  }
};
