import { useEffect } from 'react';
import { create } from 'zustand';
import { toast } from 'sonner';
import { appFetch } from '@/lib/app-path';
import {
  HANDED_SHOWN_MS,
  WAITS_SHOWN_MS,
  interpretSendNow,
  livePhase,
  sendNowKey,
  type ISendNowPhaseEntry,
  type TSendNowPhase,
} from '@/lib/hl-send-now-client';
import { t } from '@/lib/i18n';

interface ISendNowStore {
  phases: Record<string, ISendNowPhaseEntry>;
}

const useSendNowStore = create<ISendNowStore>(() => ({ phases: {} }));

const setPhase = (key: string, entry: ISendNowPhaseEntry | null) =>
  useSendNowStore.setState((state) => {
    const phases = { ...state.phases };
    if (entry) phases[key] = entry;
    else delete phases[key];
    return { phases };
  });

export const clearSendNowPhase = (tabId: string, text: string) => {
  const key = sendNowKey(tabId, text);
  if (useSendNowStore.getState().phases[key]) setPhase(key, null);
};

/**
 * POST /api/platform/send-now for a queued message. The bubble shows "Sending…" from the click on, and goes back
 * to "Queued" with a toast when the server could not hand the message over.
 */
export const requestSendNow = async (tabId: string, text: string): Promise<void> => {
  const key = sendNowKey(tabId, text);
  if (useSendNowStore.getState().phases[key]?.phase === 'sending') return;
  setPhase(key, { phase: 'sending', until: null });
  try {
    const response = await appFetch('/api/platform/send-now', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tabId, text }),
    });
    const body = (await response.json().catch(() => null)) as Parameters<typeof interpretSendNow>[0]['body'];
    const verdict = interpretSendNow({ status: response.status, body });
    if (verdict.kind === 'handed') {
      setPhase(key, { phase: 'handed', until: Date.now() + HANDED_SHOWN_MS });
      if (verdict.outcome === 'sent') toast.success(t('hooks.useSendNow.delivered'));
    } else if (verdict.kind === 'waits') {
      setPhase(key, { phase: 'waits', until: Date.now() + WAITS_SHOWN_MS });
    } else {
      setPhase(key, null);
      toast.error(verdict.message);
    }
  } catch {
    setPhase(key, null);
    toast.error(t('hooks.useSendNow.noConnection'));
  }
};

/** The phase of this message's "Send now", null when none runs. It ends by itself when its time is up. */
export const useSendNowPhase = (tabId: string | undefined, text: string): TSendNowPhase | null => {
  const key = tabId ? sendNowKey(tabId, text) : null;
  const phase = useSendNowStore((state) => (key ? livePhase(state.phases[key], Date.now()) : null));
  useEffect(() => {
    const entry = key ? useSendNowStore.getState().phases[key] : undefined;
    if (!key || !entry || entry.until === null) return;
    const timer = setTimeout(
      () => {
        setPhase(key, null);
        if (entry.phase === 'handed') toast.error(t('hooks.useSendNow.notRead'));
      },
      Math.max(0, entry.until - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [key, phase]);
  return phase;
};
