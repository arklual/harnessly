// The UI slots of apps (docs/apps.md, «Slots»): which views of the installed apps fill the settings page, the
// onboarding, an object's action row and the branding. All of it is empty unless the machine runs with
// HARNESSLY_UI_SLOTS=on, so without extensions the interface stays as it was.
import type { IAppView, IBranding, IInstalledApp, IInstalledApps } from '@/next/features/app-views/api';

export type TSlot = 'settings' | 'onboarding' | 'object-action';

export interface ISlotEntry {
  app: IInstalledApp;
  view: IAppView;
}

export interface ISlotObject {
  kind: string;
  id: string;
  name: string;
}

/** The views of the slot, by the view's `order` and then by app. An object action lists only the kinds it is for. */
export const slotViews = (list: IInstalledApps | null, slot: TSlot, objectKind?: string): ISlotEntry[] => {
  if (list === null || list.slots !== true) return [];
  const found: ISlotEntry[] = [];
  for (const app of list.apps)
    for (const view of app.views) {
      if (!view.targets.includes(slot)) continue;
      if (slot === 'object-action' && objectKind !== undefined && !(view.objects ?? []).includes(objectKind))
        continue;
      found.push({ app, view });
    }
  return found.sort(
    (a, b) => (a.view.order ?? 100) - (b.view.order ?? 100) || a.app.id.localeCompare(b.app.id),
  );
};

/** The branding the first app that brings one sets; null when there is none or the slots are off. */
export const brandingOf = (list: IInstalledApps | null): IBranding | null => {
  if (list === null || list.slots !== true) return null;
  return list.apps.find((a) => a.branding !== undefined)?.branding ?? null;
};

const ACCENT = /^#[0-9a-f]{6}$/i;

/** Applies the name and accent of a branding to the page; returns the undo. The accent is checked again here. */
export const applyBranding = (b: IBranding, root: HTMLElement = document.documentElement): (() => void) => {
  const title = document.title;
  document.title = b.name;
  const names = ['--hx-accent', '--hx-accent-soft'];
  const had = names.map((n) => root.style.getPropertyValue(n));
  if (b.accent !== undefined && ACCENT.test(b.accent)) {
    root.style.setProperty('--hx-accent', b.accent);
    root.style.setProperty('--hx-accent-soft', `color-mix(in srgb, ${b.accent} 14%, transparent)`);
  }
  return () => {
    document.title = title;
    names.forEach((n, i) => {
      if (had[i] === '') root.style.removeProperty(n);
      else root.style.setProperty(n, had[i]);
    });
  };
};
