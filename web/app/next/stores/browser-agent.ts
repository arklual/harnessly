// «Агент в браузере» (Settings → General): whether the agent asks before it acts on a site in the pane's browser, and
// what the person answered for each site. Kept in this browser, as the list of the pane's sites is: the person answers
// on the device that shows the page.
import { useSyncExternalStore } from 'react';

const KEY = 'hl:browser-agent';

export const SITE_DECISION = { allow: 'allow', deny: 'deny' } as const;
export type TSiteDecision = (typeof SITE_DECISION)[keyof typeof SITE_DECISION];

export interface IBrowserAgentChoice {
  /** Ask before the first action on a site; off lets the agent act anywhere but on the denied sites. */
  ask: boolean;
  sites: Readonly<Record<string, TSiteDecision>>;
}

const DEFAULT: IBrowserAgentChoice = { ask: true, sites: {} };

const listeners = new Set<() => void>();
let choice: IBrowserAgentChoice | null = null;

const isDecision = (v: unknown): v is TSiteDecision => v === SITE_DECISION.allow || v === SITE_DECISION.deny;

const read = (): IBrowserAgentChoice => {
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return DEFAULT;
    const parsed = JSON.parse(raw) as { ask?: unknown; sites?: unknown };
    const sites: Record<string, TSiteDecision> = {};
    if (parsed.sites && typeof parsed.sites === 'object') {
      for (const [host, decision] of Object.entries(parsed.sites))
        if (isDecision(decision)) sites[host] = decision;
    }
    return { ask: parsed.ask !== false, sites };
  } catch {
    return DEFAULT;
  }
};

export const browserAgentSnapshot = (): IBrowserAgentChoice => (choice ??= read());

const save = (next: IBrowserAgentChoice): void => {
  choice = next;
  try {
    if (next.ask && Object.keys(next.sites).length === 0) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // Private mode or a full storage: the answers hold for this page.
  }
  for (const listener of [...listeners]) listener();
};

export const setBrowserAgentAsk = (ask: boolean): void => save({ ...browserAgentSnapshot(), ask });

/** Remembers the answer for a site; null forgets it, and the agent asks there again. */
export const setSiteDecision = (host: string, decision: TSiteDecision | null): void => {
  const sites = { ...browserAgentSnapshot().sites };
  if (decision === null) delete sites[host];
  else sites[host] = decision;
  save({ ...browserAgentSnapshot(), sites });
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
