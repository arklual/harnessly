// The conversation's pane intents on its machine (the runtime's /pane-intents, under its Legacy tab), asked while the
// conversation is on screen: what the agent asked with `harnessly open` and what the runtime asked on seeing it work.
// Each acts once in this browser (the ids it settled are kept here), and the machine hears which ones a page showed.
import { useEffect, useEffectEvent, useRef } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { parseIntents, planIntents, SEEN_LIMIT, type IPaneIntent } from '@/next/features/pane/intents';
import { calmPoll } from '@/next/core/net/calm-poll';
import type { MachineId } from '@/next/protocol/v2';
import { paneAutoStateSnapshot } from '@/next/stores/pane-auto';
import { PHONE_QUERY } from '@/next/ui/use-media';

/** The new interface keeps no status socket: while the conversation is on screen, its intents are asked this often. */
export const INTENTS_POLL_MS = 2000;
/** The longest step while the intents stay the same: the conversations moving bring it back to the base. */
export const INTENTS_QUIET_MS = 15_000;

const seenKey = (tabId: string) => `hx:pane-intents-seen:${tabId}`;

/** The ids this browser settled for the conversation; null before its first look. */
export const readSeen = (tabId: string): string[] | null => {
  try {
    const raw = window.localStorage.getItem(seenKey(tabId));
    if (raw === null) return null;
    const list: unknown = JSON.parse(raw);
    return Array.isArray(list) ? list.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return null;
  }
};

const writeSeen = (tabId: string, ids: readonly string[]) => {
  try {
    const before = readSeen(tabId) ?? [];
    window.localStorage.setItem(seenKey(tabId), JSON.stringify([...before, ...ids].slice(-SEEN_LIMIT)));
  } catch {
    // Private mode or a full storage: the page remembers nothing, the machine's applied marks still hold.
  }
};

const path = (tabId: string) => platformPath(`/pane-intents?tabId=${encodeURIComponent(tabId)}`);

/**
 * Applies the conversation's intents as they come: `apply` gets the ones to act on, oldest first. Nothing runs while
 * the conversation has no Legacy tab (no agent yet) or the page is hidden.
 */
export const usePaneIntents = (
  machine: MachineId | null,
  tabId: string | null,
  apply: (intents: IPaneIntent[]) => void,
  /** Every live intent of the conversation, on each look: what is told by intents already applied (whose a tab is). */
  onList?: (intents: IPaneIntent[]) => void,
): void => {
  const last = useRef<string | null>(null);
  /** One read; true when the machine's list changed. */
  const look = useEffectEvent(async (m: MachineId, tab: string, signal: AbortSignal): Promise<boolean> => {
    const body = await runtimeJson<unknown>(m, path(tab), { signal, cache: 'no-store' });
    if (signal.aborted) return false;
    const said = JSON.stringify(body);
    const changed = said !== last.current;
    last.current = said;
    const intents = parseIntents(body);
    if (changed) onList?.(intents);
    const auto = paneAutoStateSnapshot();
    const plan = planIntents(intents, readSeen(tab), Date.now(), { auto: auto.on, kinds: auto.kinds });
    if (!plan) return changed;
    writeSeen(tab, plan.settled);
    if (plan.apply.length === 0) return changed;
    apply(plan.apply);
    const device = window.matchMedia(PHONE_QUERY).matches ? 'phone' : 'desktop';
    void runtimeJson(
      m,
      platformPath('/pane-intents'),
      postJson({ tabId: tab, ids: plan.apply.map((x) => x.id), device }),
    ).catch(() => undefined);
    return true;
  });

  // Read while the page is visible, slower while the list stays the same.
  useEffect(() => {
    if (!machine || !tabId) return;
    const ctl = new AbortController();
    last.current = null;
    const stop = calmPoll({
      base: INTENTS_POLL_MS,
      max: INTENTS_QUIET_MS,
      look: () => look(machine, tabId, ctl.signal),
    });
    return () => {
      stop();
      ctl.abort();
    };
  }, [machine, tabId]);
};
