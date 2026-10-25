// The conversation's pane intents on its machine (the runtime's /pane-intents, under its Legacy tab), asked while the
// conversation is on screen: what the agent asked with `harnessly open` and what the runtime asked on seeing it work.
// Each acts once in this browser (the ids it settled are kept here), and the machine hears which ones a page showed.
import { useEffect, useEffectEvent } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { parseIntents, planIntents, SEEN_LIMIT, type IPaneIntent } from '@/next/features/pane/intents';
import { pageClock } from '@/next/core/clock';
import type { MachineId } from '@/next/protocol/v2';
import { paneAutoSnapshot } from '@/next/stores/pane-auto';
import { PHONE_QUERY } from '@/next/ui/use-media';

/** The new interface keeps no status socket: while the conversation is on screen, its intents are asked this often. */
export const INTENTS_POLL_MS = 2000;

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
): void => {
  const look = useEffectEvent(async (m: MachineId, tab: string, signal: AbortSignal) => {
    const body = await runtimeJson<unknown>(m, path(tab), { signal, cache: 'no-store' });
    if (signal.aborted) return;
    const plan = planIntents(parseIntents(body), readSeen(tab), Date.now(), { auto: paneAutoSnapshot() });
    if (!plan) return;
    writeSeen(tab, plan.settled);
    if (plan.apply.length === 0) return;
    apply(plan.apply);
    const device = window.matchMedia(PHONE_QUERY).matches ? 'phone' : 'desktop';
    void runtimeJson(
      m,
      platformPath('/pane-intents'),
      postJson({ tabId: tab, ids: plan.apply.map((x) => x.id), device }),
    ).catch(() => undefined);
  });

  useEffect(() => {
    if (!machine || !tabId) return;
    const clock = pageClock();
    const ctl = new AbortController();
    let timer: unknown = null;
    // One look at a time: a slow machine is not asked again before it answered, so nothing applies twice.
    let looking = false;
    const again = () => {
      clock.clearTimeout(timer);
      timer = clock.setTimeout(again, INTENTS_POLL_MS);
      if (looking || document.visibilityState !== 'visible') return;
      looking = true;
      void look(machine, tabId, ctl.signal)
        .catch(() => undefined)
        .finally(() => {
          looking = false;
        });
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') again();
    };
    again();
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('online', again);
    return () => {
      clock.clearTimeout(timer);
      ctl.abort();
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('online', again);
    };
  }, [machine, tabId]);
};
