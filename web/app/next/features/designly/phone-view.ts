// How a phone or a tablet of an open design is drawn: its case, the status bar, and whether the page is touched
// rather than pointed at (no hover, as on the device). The person's own choice for every design, kept per browser; the
// browser's bars and the turn are the design's own (view-memory.ts).
import { useSyncExternalStore } from 'react';

export interface IPhoneView {
  /** The device's case round the screen; off, the screen alone. */
  frame: boolean;
  /** The status bar on top and the home indicator under the page. */
  status: boolean;
  /** The page is touched: its hover styles are off and the pointer is a fingertip. */
  touch: boolean;
}

export const PHONE_VIEW_DEFAULT: IPhoneView = { frame: true, status: true, touch: false };

const KEY = 'hx-next-phone-view';

const flag = (value: unknown, fallback: boolean): boolean => (typeof value === 'boolean' ? value : fallback);

export const parsePhoneView = (raw: unknown): IPhoneView => {
  if (typeof raw !== 'object' || raw === null) return PHONE_VIEW_DEFAULT;
  const rec = raw as Record<string, unknown>;
  return {
    frame: flag(rec.frame, PHONE_VIEW_DEFAULT.frame),
    status: flag(rec.status, PHONE_VIEW_DEFAULT.status),
    touch: flag(rec.touch, PHONE_VIEW_DEFAULT.touch),
  };
};

const read = (): IPhoneView => {
  try {
    const text = localStorage.getItem(KEY);
    return text === null ? PHONE_VIEW_DEFAULT : parsePhoneView(JSON.parse(text));
  } catch {
    return PHONE_VIEW_DEFAULT;
  }
};

const listeners = new Set<() => void>();
let view: IPhoneView | null = null;
const snapshot = (): IPhoneView => (view ??= read());
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const setPhoneView = (change: Partial<IPhoneView>): void => {
  view = { ...snapshot(), ...change };
  try {
    localStorage.setItem(KEY, JSON.stringify(view));
  } catch {
    // Private mode or full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

export const usePhoneView = (): IPhoneView =>
  useSyncExternalStore(subscribe, snapshot, () => PHONE_VIEW_DEFAULT);
