import { isPaneIntent, PANE_INTENTS_EVENT, type IPaneIntent } from '@/lib/hl-pane-intents';

/**
 * The pane intents this page has heard over its status connection, by conversation. A page reading statuses
 * from the cluster stream learns of an agent's `hl open` here, without a request to the runtime.
 */
const feed = new Map<string, IPaneIntent[]>();

const announce = (tabId: string, intents: IPaneIntent[]) =>
  window.dispatchEvent(new CustomEvent(PANE_INTENTS_EVENT, { detail: { tabId, intents } }));

const clean = (value: unknown): IPaneIntent[] => (Array.isArray(value) ? value.filter(isPaneIntent) : []);

export const syncPaneIntents = (all: Record<string, unknown>) => {
  const previous = new Set(feed.keys());
  feed.clear();
  for (const [tabId, intents] of Object.entries(all)) {
    feed.set(tabId, clean(intents));
    previous.delete(tabId);
    announce(tabId, feed.get(tabId) ?? []);
  }
  for (const tabId of previous) announce(tabId, []);
};

export const updatePaneIntents = (tabId: string, intents: unknown) => {
  feed.set(tabId, clean(intents));
  announce(tabId, feed.get(tabId) ?? []);
};

/** `null` until this page heard of the conversation at all: then it asks the runtime as before. */
export const heardPaneIntents = (tabId: string): IPaneIntent[] | null => feed.get(tabId) ?? null;
