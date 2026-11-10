// «Агент в браузере» (Settings → General): the sites the person blocked for the agent in the pane's browser. Anywhere
// else the agent acts without asking (HRN-681): the person sees its cursor on the page. Kept in this browser, as the
// list of the pane's sites is.
import { useSyncExternalStore } from 'react';

const KEY = 'hl:browser-agent';

export interface IBrowserAgentChoice {
  /** The sites (hosts) the agent may not act on, sorted. */
  blocked: readonly string[];
}

const DEFAULT: IBrowserAgentChoice = { blocked: [] };

const listeners = new Set<() => void>();
let choice: IBrowserAgentChoice | null = null;

const read = (): IBrowserAgentChoice => {
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return DEFAULT;
    const parsed = JSON.parse(raw) as { blocked?: unknown; sites?: unknown };
    const blocked = new Set<string>();
    if (Array.isArray(parsed.blocked))
      for (const host of parsed.blocked) if (typeof host === 'string' && host) blocked.add(host);
    // The answers of the time the agent asked: «Запретить» is a block, «Разрешить» is the default now.
    if (parsed.sites && typeof parsed.sites === 'object')
      for (const [host, decision] of Object.entries(parsed.sites)) if (decision === 'deny') blocked.add(host);
    return { blocked: [...blocked].sort() };
  } catch {
    return DEFAULT;
  }
};

export const browserAgentSnapshot = (): IBrowserAgentChoice => (choice ??= read());

const save = (next: IBrowserAgentChoice): void => {
  choice = next;
  try {
    if (next.blocked.length === 0) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // Private mode or a full storage: the list holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

/** Blocks a site for the agent, or lets it act there again. */
export const setSiteBlocked = (host: string, blocked: boolean): void => {
  const set = new Set(browserAgentSnapshot().blocked);
  if (blocked) set.add(host);
  else set.delete(host);
  save({ blocked: [...set].sort() });
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key !== KEY) return;
    choice = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
};

export const useBrowserAgent = (): IBrowserAgentChoice =>
  useSyncExternalStore(subscribe, browserAgentSnapshot, browserAgentSnapshot);
