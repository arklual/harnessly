// A limit running out, told once per step: when the chat is opened, and then as its limits move, a notice at
// the top (ui/toast.tsx) for each step newly reached. The week from under 5% on each per cent (4, 3, 2, 1, 0); the five
// hours from 20% on each five (20, 15, 10, 5, 0). A step is told once in a window: what was told is kept per window
// (the agent's account, the kind, its reset) in this browser, and a window that has reset is forgotten. A window that
// jumped past several steps tells only the one it is at.
import { useEffect } from 'react';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';
import {
  WINDOW,
  leftOf,
  profileName,
  resetClock,
  windowsOf,
  type LimitsView,
  type WindowKind,
} from '@/next/features/limits/model';

/** The steps told, per window, from the highest. */
export const ALERT_STEPS: Readonly<Partial<Record<WindowKind, readonly number[]>>> = {
  [WINDOW.sevenDay]: [4, 3, 2, 1, 0],
  [WINDOW.fiveHour]: [20, 15, 10, 5, 0],
};

/** The step a window with `left` per cent is at: the lowest step not under it; null above the first. */
export const stepOf = (left: number, steps: readonly number[]): number | null => {
  let at: number | null = null;
  for (const step of steps) if (left <= step) at = step;
  return at;
};

/** What was told: the lowest step per window, and the window's reset to forget it by (epoch seconds). */
export type TAlertMemory = Readonly<Record<string, { step: number; until: number }>>;

export interface IAlert {
  id: string;
  kind: WindowKind;
  step: number;
  left: number;
  resetsAt: number;
}

/** A window by whose limit it is and when it resets; the hour, so a reading's drift of the reset is the same window. */
const windowId = (view: LimitsView, kind: WindowKind, resetsAt: number): string =>
  [view.provider, view.profile ?? '', kind, Math.round(resetsAt / 3600)].join('|');

/** The notices due now, and the memory with them told (and with the windows that have reset forgotten). */
export const alertsDue = (
  view: LimitsView | null,
  memory: TAlertMemory,
  nowSec: number,
): { due: IAlert[]; memory: TAlertMemory } => {
  const kept: Record<string, { step: number; until: number }> = {};
  for (const [id, entry] of Object.entries(memory)) if (entry.until > nowSec) kept[id] = entry;
  const due: IAlert[] = [];
  if (view?.state === 'ready') {
    for (const { kind, window } of windowsOf(view.limits, nowSec)) {
      const steps = ALERT_STEPS[kind];
      if (!steps) continue;
      const left = leftOf(window);
      const step = stepOf(left, steps);
      if (step === null) continue;
      const id = windowId(view, kind, window.resetsAt);
      const told = kept[id];
      if (told && told.step <= step) continue;
      kept[id] = { step, until: window.resetsAt };
      due.push({ id, kind, step, left, resetsAt: window.resetsAt });
    }
  }
  return { due, memory: kept };
};

const KEY = 'hx-limit-alerts';

const readMemory = (): TAlertMemory => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? '{}');
    if (typeof raw !== 'object' || raw === null) return {};
    const out: Record<string, { step: number; until: number }> = {};
    for (const [id, entry] of Object.entries(raw as Record<string, unknown>)) {
      const e = entry as { step?: unknown; until?: unknown } | null;
      if (typeof e?.step === 'number' && typeof e.until === 'number')
        out[id] = { step: e.step, until: e.until };
    }
    return out;
  } catch {
    return {};
  }
};

const writeMemory = (memory: TAlertMemory): void => {
  try {
    localStorage.setItem(KEY, JSON.stringify(memory));
  } catch {
    // A browser that keeps nothing tells the step again next time: no harm.
  }
};

const ALERT_MS = 8000;

/** The notice's words: «Недельный лимит Claude» / «Осталось 3%, сброс в пт 14:00». */
export const alertWords = (view: LimitsView, alert: IAlert): { title: string; text: string } => {
  const provider = view.profile ? profileName(view.profile) : t(`next.agent.${view.provider}`);
  const at = resetClock(alert.kind, alert.resetsAt);
  return {
    title: t(alert.kind === WINDOW.sevenDay ? 'next.limits.alert.sevenDay' : 'next.limits.alert.fiveHour', {
      provider,
    }),
    text:
      alert.left === 0
        ? t('next.limits.alert.textOut', { at })
        : t('next.limits.alert.text', { left: alert.left, at }),
  };
};

/** Tells the chat's limit steps as they are reached; the chat's view, null while it has none. */
let lastSeen = '';

/** What the notices depend on: whose limits, and each window's left and reset. A new object of the same is no news. */
const seenKey = (view: LimitsView): string =>
  view.state === 'ready'
    ? [
        view.provider,
        view.profile ?? '',
        ...windowsOf(view.limits).map((w) => `${w.kind}:${leftOf(w.window)}:${w.window.resetsAt}`),
      ].join('|')
    : '';

export const useLimitAlerts = (view: LimitsView | null): void => {
  useEffect(() => {
    if (view?.state !== 'ready') return;
    const key = seenKey(view);
    if (key === lastSeen) return;
    lastSeen = key;
    const { due, memory } = alertsDue(view, readMemory(), Date.now() / 1000);
    writeMemory(memory);
    for (const alert of due) {
      const { title, text } = alertWords(view, alert);
      toast(title, { tone: alert.step <= 5 ? 'error' : 'warning', description: text, ms: ALERT_MS });
    }
  }, [view]);
};
