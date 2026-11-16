// A smart widget's short answer: the widget's request and its data go to the machine (POST
// <platform>/side-widgets/smart), which asks the model picked for «Умные виджеты» (Claude Haiku of the machine without
// a pick). The page asks no more often than the widget's interval, and only while the widget is unfolded and on screen;
// the last answer is kept in this browser per widget, so a reload or another chat does not ask again before it is due.
// The machine holds its own floor on top (once a minute per widget, sixty an hour).
import { useCallback, useEffect, useRef, useState } from 'react';
import { deviceId } from '@/next/core/device';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { smartDue } from '@/next/features/widgets/model';
import { DEVICE_HEADER, type MachineId } from '@/next/protocol/v2';

export interface ISmartAnswer {
  text: string;
  at: number;
}

export type TSmartState =
  | { k: 'idle'; answer: ISmartAnswer | null }
  | { k: 'asking'; answer: ISmartAnswer | null }
  | { k: 'failed'; answer: ISmartAnswer | null; message: string };

const KEPT = 'hx-widget-smart:';

const readKept = (key: string): ISmartAnswer | null => {
  try {
    const v: unknown = JSON.parse(window.localStorage.getItem(KEPT + key) ?? 'null');
    if (typeof v !== 'object' || v === null) return null;
    const { text, at } = v as Record<string, unknown>;
    return typeof text === 'string' && typeof at === 'number' ? { text, at } : null;
  } catch {
    return null;
  }
};

const keep = (key: string, answer: ISmartAnswer) => {
  try {
    window.localStorage.setItem(KEPT + key, JSON.stringify(answer));
  } catch {
    // The answer holds for this page.
  }
};

export const askSmart = async (
  machine: MachineId,
  body: { key: string; prompt: string; data: string },
): Promise<{ ok: true; answer: ISmartAnswer } | { ok: false; message: string }> => {
  try {
    const res = await fetch(runtimePath(platformPath('/side-widgets/smart'), machine), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { [DEVICE_HEADER]: deviceId(), 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const doc: unknown = await res.json().catch(() => null);
    const rec = typeof doc === 'object' && doc !== null ? (doc as Record<string, unknown>) : {};
    if (res.ok && typeof rec.text === 'string' && typeof rec.at === 'number')
      return { ok: true, answer: { text: rec.text, at: rec.at } };
    return { ok: false, message: typeof rec.message === 'string' ? rec.message : String(res.status) };
  } catch {
    return { ok: false, message: 'network' };
  }
};

/**
 * The smart answer of a widget while `on` (unfolded, on screen): asked when due, again when the interval passes, and at
 * once by `refresh`. `data` is read when the ask goes, so its changes alone ask nothing.
 */
export const useSmart = (
  machine: MachineId | null,
  key: string,
  prompt: string | null,
  everyMs: number,
  on: boolean,
  data: () => string,
): { state: TSmartState; refresh: () => void } => {
  const [state, setState] = useState<TSmartState>(() => ({ k: 'idle', answer: readKept(key) }));
  const dataRef = useRef(data);
  useEffect(() => {
    dataRef.current = data;
  });
  const asking = useRef(false);
  const go = useCallback(async () => {
    if (!machine || !prompt || asking.current) return;
    asking.current = true;
    setState((s) => ({ k: 'asking', answer: s.answer }));
    const res = await askSmart(machine, { key, prompt, data: dataRef.current() });
    asking.current = false;
    if (res.ok) {
      keep(key, res.answer);
      setState({ k: 'idle', answer: res.answer });
    } else setState((s) => ({ k: 'failed', answer: s.answer, message: res.message }));
  }, [machine, key, prompt]);

  const lastAt = state.answer?.at ?? null;
  useEffect(() => {
    if (!on || !prompt) return;
    const now = Date.now();
    if (smartDue(lastAt, everyMs, now) && !document.hidden) {
      void go();
      return;
    }
    const wait = lastAt === null ? everyMs : Math.max(1000, lastAt + everyMs - now);
    const timer = window.setTimeout(() => {
      if (!document.hidden) void go();
    }, wait);
    return () => window.clearTimeout(timer);
  }, [on, prompt, lastAt, everyMs, go]);

  const refresh = useCallback(() => void go(), [go]);
  return { state, refresh };
};
