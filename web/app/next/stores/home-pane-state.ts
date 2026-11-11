// The right pane of the start page, where no chat is open: whether it is out, how wide, the machine its tools work on,
// and its tabs (the review picker and the changes opened from it, GitLab, the vault, and the machine's tools: a shell,
// the browser, the files, the machine's load, Sinopia Design — each kept with the machine it is of). Kept the way a chat's pane is (stores/pane-state.ts): this browser's
// localStorage, read once and written a moment after a change, so a reload or coming back home finds it as it was left.
import { useSyncExternalStore } from 'react';
import type { TTab } from '@/next/features/pane/pane-tabs';
import { isPaneTab } from '@/next/stores/pane-state';

export const HOME_PANE_KEY = 'hx-pane-home';
const VERSION = 1;
const PERSIST_MS = 300;
/** More open changes than this are not a working set: the oldest go. */
export const HOME_TABS_MAX = 12;

export const HOME_TAB = {
  review: 'review',
  change: 'change',
  gitlab: 'gitlab',
  vault: 'vault',
  shell: 'shell',
  browser: 'browser',
  files: 'files',
  machine: 'machine',
  designly: 'designly',
} as const;

/** The tabs of a machine's tools: the chat pane's own (features/pane/pane-tabs.ts), with the machine they are of. */
export const MACHINE_TAB_KINDS = [
  HOME_TAB.shell,
  HOME_TAB.browser,
  HOME_TAB.files,
  HOME_TAB.machine,
  HOME_TAB.designly,
] as const;
type TMachineKind = (typeof MACHINE_TAB_KINDS)[number];
export type TMachineTab = Extract<TTab, { kind: TMachineKind }> & { machine: string };

export type THomeTab =
  | { id: string; kind: typeof HOME_TAB.review }
  | { id: string; kind: typeof HOME_TAB.gitlab }
  | { id: string; kind: typeof HOME_TAB.vault }
  | TMachineTab
  | {
      id: string;
      kind: typeof HOME_TAB.change;
      /** The merge request's or the commit's web address. */
      url: string;
      title: string;
      /** What the picker knew of it, for the agent: the branches and the merge request's title. */
      name: string | null;
      source: string | null;
      target: string | null;
    };

export interface IHomePane {
  open: boolean;
  maxed: boolean;
  width: number | null;
  tabs: readonly THomeTab[];
  /** The tab in front; '' is the tools. */
  active: string;
  /** The machine the tools open on, picked by the person; null: the start page's own. */
  machine: string | null;
}

export const EMPTY_HOME_PANE: IHomePane = Object.freeze({
  open: false,
  maxed: false,
  width: null,
  tabs: [],
  active: '',
  machine: null,
});

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const strOrNull = (v: unknown): string | null => (typeof v === 'string' ? v : null);

const isMachineKind = (kind: unknown): kind is TMachineKind =>
  (MACHINE_TAB_KINDS as readonly unknown[]).includes(kind);

const readTab = (v: unknown): THomeTab | null => {
  if (!isRecord(v) || typeof v.id !== 'string' || v.id === '') return null;
  if (v.kind === HOME_TAB.review || v.kind === HOME_TAB.gitlab || v.kind === HOME_TAB.vault)
    return { id: v.id, kind: v.kind };
  if (isMachineKind(v.kind)) {
    const machine = v.machine;
    if (typeof machine !== 'string' || machine === '' || !isPaneTab(v)) return null;
    return { ...(v as Extract<TTab, { kind: TMachineKind }>), machine };
  }
  if (v.kind !== HOME_TAB.change || typeof v.url !== 'string' || !v.url.startsWith('https://')) return null;
  return {
    id: v.id,
    kind: HOME_TAB.change,
    url: v.url,
    title: typeof v.title === 'string' ? v.title : v.url,
    name: strOrNull(v.name),
    source: strOrNull(v.source),
    target: strOrNull(v.target),
  };
};

/** What was kept, or the empty pane for anything this version does not read. */
export const parseHomePane = (raw: string | null): IHomePane => {
  if (!raw) return EMPTY_HOME_PANE;
  let v: unknown;
  try {
    v = JSON.parse(raw);
  } catch {
    return EMPTY_HOME_PANE;
  }
  if (!isRecord(v) || v.v !== VERSION || !isRecord(v.pane)) return EMPTY_HOME_PANE;
  const p = v.pane;
  const tabs = Array.isArray(p.tabs) ? p.tabs.flatMap((x) => readTab(x) ?? []).slice(-HOME_TABS_MAX) : [];
  const active = typeof p.active === 'string' && tabs.some((x) => x.id === p.active) ? p.active : '';
  return {
    open: p.open === true,
    maxed: p.maxed === true,
    width: typeof p.width === 'number' && Number.isFinite(p.width) && p.width > 0 ? p.width : null,
    tabs,
    active,
    machine: typeof p.machine === 'string' && p.machine !== '' ? p.machine : null,
  };
};

export const serializeHomePane = (pane: IHomePane): string =>
  JSON.stringify({ v: VERSION, pane: { ...pane, tabs: pane.tabs.slice(-HOME_TABS_MAX) } });

const storage = {
  read(): string | null {
    try {
      return localStorage.getItem(HOME_PANE_KEY);
    } catch {
      return null;
    }
  },
  write(value: string) {
    try {
      localStorage.setItem(HOME_PANE_KEY, value);
    } catch {
      // Private mode or a full storage: the pane is kept for this page only.
    }
  },
};

const listeners = new Set<() => void>();
let kept: IHomePane | null = null;
let timer: ReturnType<typeof setTimeout> | null = null;
let hooked = false;

const persistNow = () => {
  if (timer !== null) clearTimeout(timer);
  timer = null;
  if (kept) storage.write(serializeHomePane(kept));
};

export const homePaneOf = (): IHomePane => (kept ??= parseHomePane(storage.read()));

export const updateHomePane = (change: (pane: IHomePane) => IHomePane) => {
  const before = homePaneOf();
  const next = change(before);
  if (next === before) return;
  kept = next;
  if (!hooked && typeof window !== 'undefined') {
    hooked = true;
    window.addEventListener('pagehide', persistNow);
  }
  timer ??= setTimeout(persistNow, PERSIST_MS);
  for (const listener of [...listeners]) listener();
};

/** Forgets what the page read (tests). */
export const resetHomePane = () => {
  if (timer !== null) clearTimeout(timer);
  timer = null;
  kept = null;
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const useHomePaneState = (): IHomePane => useSyncExternalStore(subscribe, homePaneOf, homePaneOf);
