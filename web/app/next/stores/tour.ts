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
  /** The step was reached by «Назад»: what it asks may be done already, and that is not a reason to leave it again. */
  back: boolean;
  /** Which arrival at a step this is (every change of the tour counts): a step come back to is a new one, not the one done before. */
  at: number;
}

const KEY = 'hx-tour';
const NONE: ITourState = { track: null, step: 0, passed: [], back: false, at: 0 };
let arrivals = 0;

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
      back: false,
      at: 0,
    };
  } catch {
    return NONE;
  }
};

export const tourState = (): ITourState => {
  state ??= load();
  return state;
};

const set = (next: Omit<ITourState, 'at'>) => {
  state = { ...next, at: ++arrivals };
  signals = new Set();
  try {
    window.localStorage.setItem(KEY, JSON.stringify(state));
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

export const startTour = (track: string) => set({ ...tourState(), track, step: 0, back: false });

export const toStep = (step: number) => set({ ...tourState(), step, back: false });

/** Back to an earlier step: it stays on screen even when it is done already (`ITourState.back`). */
export const stepBack = (step: number) => set({ ...tourState(), step, back: true });

export const finishTour = () => {
  const s = tourState();
  set({
    track: null,
    step: 0,
    passed: s.track && !s.passed.includes(s.track) ? [...s.passed, s.track] : s.passed,
    back: false,
  });
};

export const leaveTour = () => set({ ...tourState(), track: null, step: 0, back: false });

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

/** A feature says a family of facts at once (`lib:route:`): every fact of the prefix is replaced by these names. */
export const tourFactGroup = (prefix: string, names: readonly string[]) => {
  const next = new Set([...facts].filter((f) => !f.startsWith(prefix)));
  for (const name of names) next.add(name);
  if (next.size === facts.size && [...next].every((f) => facts.has(f))) return;
  facts = next;
  for (const listener of [...listeners]) listener();
};
