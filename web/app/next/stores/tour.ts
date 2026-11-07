// The tour the person is on, in this browser (localStorage, written at every change): which lesson, which step, and
// the lessons passed. Storage that throws is the same as none: the tour holds for the page. Features tell the tour
// what happened with tourSignal; a step reads the signals given since it began.
import { useSyncExternalStore } from 'react';

export interface ITourState {
  /** The lesson on screen; null when no tour runs. */
  track: string | null;
  step: number;
  /** The lessons passed to the end. */
  passed: readonly string[];
}

const KEY = 'hx-tour';
const NONE: ITourState = { track: null, step: 0, passed: [] };

const listeners = new Set<() => void>();
let state: ITourState | null = null;
let signals = new Set<string>();

const load = (): ITourState => {
  try {
    const raw = window.localStorage.getItem(KEY);
    const v = raw ? (JSON.parse(raw) as Partial<ITourState>) : null;
    if (!v) return NONE;
    return {
      track: typeof v.track === 'string' ? v.track : null,
      step: typeof v.step === 'number' && v.step >= 0 ? v.step : 0,
      passed: Array.isArray(v.passed) ? v.passed.filter((x): x is string => typeof x === 'string') : [],
    };
  } catch {
    return NONE;
  }
};

export const tourState = (): ITourState => {
  state ??= load();
  return state;
};

const set = (next: ITourState) => {
  state = next;
  signals = new Set();
  try {
    window.localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // The tour holds for the page.
  }
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

export const useTour = () => useSyncExternalStore(subscribe, tourState, () => NONE);

export const startTour = (track: string) => set({ ...tourState(), track, step: 0 });

export const toStep = (step: number) => set({ ...tourState(), step });

export const finishTour = () => {
  const s = tourState();
  set({
    track: null,
    step: 0,
    passed: s.track && !s.passed.includes(s.track) ? [...s.passed, s.track] : s.passed,
  });
};

export const leaveTour = () => set({ ...tourState(), track: null, step: 0 });

/** A feature says something happened (a page opened, a mode changed); the running step may be waiting for it. */
export const tourSignal = (name: string) => {
  if (tourState().track === null || signals.has(name)) return;
  signals.add(name);
  for (const listener of [...listeners]) listener();
};

export const tourSignals = (): ReadonlySet<string> => signals;

let facts = new Set<string>();

/** A feature says what holds on this page whatever the step (no agent can answer here): steps may pass over it. */
export const tourFact = (name: string, on: boolean) => {
  if (facts.has(name) === on) return;
  facts = new Set(facts);
  if (on) facts.add(name);
  else facts.delete(name);
  for (const listener of [...listeners]) listener();
};

export const tourFacts = (): ReadonlySet<string> => facts;
