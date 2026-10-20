import { useEffect, useEffectEvent } from 'react';
import { appFetch, currentMachineId } from '@/lib/app-path';
import { readMachineLoad } from '@/lib/hl-machine-load-store';
import { autoOpenAllowed, effectiveLevel } from '@/lib/hl-machine-load-view';
import { readPane, updatePane } from '@/lib/hl-pane-store';
import {
  applyIntentPlan,
  isPaneIntent,
  planIntents,
  PANE_INTENTS_EVENT,
  type IPaneIntent,
  type TPaneIntentDevice,
} from '@/lib/hl-pane-intents';
import { heardPaneIntents } from '@/lib/pane-intent-feed';
import { markArrival } from '@/lib/hl-pane-arrival';
import { sendPaneIntentsApplied } from '@/hooks/use-agent-status';

interface IPaneIntentsOptions {
  tabId: string | null;
  /** The conversation is on screen: a hidden frame applies nothing, it waits for the person. */
  active: boolean;
  paneVisible: boolean;
  /** A phone, where the panel is a sheet over the whole chat: it is not thrown over what the person reads. */
  overlay: boolean;
  onShow: () => void;
}

const fetchIntents = async (tabId: string, signal: AbortSignal): Promise<IPaneIntent[]> => {
  const response = await appFetch(`/api/platform/pane-intents?tabId=${encodeURIComponent(tabId)}`, {
    signal,
    cache: 'no-store',
  });
  if (!response.ok) return [];
  const body: unknown = await response.json();
  const intents = (body as { intents?: unknown })?.intents;
  return Array.isArray(intents) ? intents.filter(isPaneIntent) : [];
};

const reportApplied = (tabId: string, ids: string[], device: TPaneIntentDevice): void => {
  if (ids.length === 0) return;
  if (sendPaneIntentsApplied(tabId, ids, device)) return;
  void appFetch('/api/platform/pane-intents', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ tabId, ids, device }),
  }).catch(() => undefined);
};

const machineBusy = (): boolean =>
  !autoOpenAllowed(effectiveLevel(readMachineLoad(currentMachineId() ?? '')));

/**
 * Applies what the agent asked its pane to show (`hl open`), kept with the conversation on the runtime:
 * when the conversation comes on screen, when the agent asks while it is there, and after the page or the socket comes
 * back. Each intent acts once on this device; a person busy in another tab gets the new one behind, marked unread.
 */
const usePaneIntents = ({ tabId, active, paneVisible, overlay, onShow }: IPaneIntentsOptions): void => {
  const apply = useEffectEvent((key: string, intents: readonly IPaneIntent[]) => {
    if (key !== tabId || !active) return;
    // A dev server is not opened on an overloaded machine: it stays unseen and waits for a calmer look.
    const overloaded = machineBusy();
    const plan = planIntents(
      intents,
      readPane(key).seen,
      Date.now(),
      (intent) => overloaded && intent.command.kind === 'open' && intent.command.tab === 'browser',
    );
    if (!plan) return;
    for (const intent of plan.apply)
      if (intent.command.kind === 'open') markArrival(key, intent.command.tab, intent.command.target ?? null);
    const next = applyIntentPlan(readPane(key), plan, paneVisible);
    updatePane(key, () => next.state);
    if (next.show && !overlay) onShow();
    reportApplied(
      key,
      plan.apply.map((intent) => intent.id),
      overlay ? 'phone' : 'desktop',
    );
  });

  const refresh = useEffectEvent((signal: AbortSignal) => {
    const key = tabId;
    if (!key || !active) return;
    const heard = heardPaneIntents(key);
    if (heard) return apply(key, heard);
    void fetchIntents(key, signal)
      .then((intents) => {
        if (!signal.aborted) apply(key, intents);
      })
      .catch(() => undefined);
  });

  useEffect(() => {
    if (!tabId || !active) return;
    let controller = new AbortController();
    const again = () => {
      controller.abort();
      controller = new AbortController();
      refresh(controller.signal);
    };
    const onChanged = (event: Event) => {
      const detail = (event as CustomEvent<{ tabId?: unknown; intents?: unknown }>).detail;
      if (detail?.tabId !== tabId) return;
      if (Array.isArray(detail.intents)) apply(tabId, detail.intents.filter(isPaneIntent));
      else again();
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') again();
    };
    refresh(controller.signal);
    window.addEventListener(PANE_INTENTS_EVENT, onChanged);
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('online', again);
    return () => {
      controller.abort();
      window.removeEventListener(PANE_INTENTS_EVENT, onChanged);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('online', again);
    };
  }, [tabId, active]);
};

export default usePaneIntents;
