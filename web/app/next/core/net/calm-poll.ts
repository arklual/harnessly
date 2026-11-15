// A poll of the runtime that calms down: asked only while the page is visible, at its base step while its
// answer changes or something waits for the person, and slower and slower while the answer stays the same, up to `max`.
// The page's activity brings it back to the base step at once: the page shown again, the window focused, the network
// back, and a move of the conversations (an agent that works is what makes a code, a notice or a pane intent appear).
//
// The runtime has no event for these lists yet; this is what keeps a quiet page from asking a request a second.
import { pageClock } from '@/next/core/clock';
import { conversationsStore } from '@/next/stores/conversations';

export interface ICalmPoll {
  /** The step while the answer changes. */
  base: number;
  /** The longest step of a quiet answer. */
  max: number;
  /** One look: true when the answer changed, or when something in it waits for the person (keep the base step). */
  look: () => Promise<boolean>;
  /** A window event that asks for a look at once (the page itself changed the list: `browser-logins-changed`). */
  wakeOn?: string;
}

/** How much longer each quiet step is than the one before. */
export const CALM_FACTOR = 1.6;

/**
 * The step while agents work. Working agents move the conversations many times a second, all the time; at
 * the base step every poll asked every 2–3 s and a phone sent 150–190 requests a minute, each answered in 3–10 s by a
 * busy runtime. A move of the conversations brings a poll back to this step, not to its base: an ask of an agent shows
 * within it. The page itself coming back (shown, focused, online) still looks at once.
 */
export const BUSY_STEP = 8000;

const nudges = new Set<() => void>();
let unConvs: (() => void) | null = null;

/** Everything that polls goes back to its busy step (the conversations moved). */
export const nudgePolls = (): void => {
  for (const n of nudges) n();
};

/** The step after a move of the conversations: the busy step, or the base when the poll is slower than that anyway. */
export const busyStep = (step: number, o: Pick<ICalmPoll, 'base' | 'max'>): number =>
  Math.min(step, Math.max(o.base, BUSY_STEP));

/** The next step after a look. */
export const nextStep = (step: number, o: Pick<ICalmPoll, 'base' | 'max'>, changed: boolean): number =>
  changed ? o.base : Math.min(o.max, Math.round(step * CALM_FACTOR));

/** Starts the poll; returns its stop. */
export const calmPoll = (o: ICalmPoll): (() => void) => {
  const clock = pageClock();
  let timer: unknown = null;
  let step = o.base;
  let looking = false;
  let live = true;
  let lastLook = 0;

  const schedule = (ms: number) => {
    clock.clearTimeout(timer);
    timer = clock.setTimeout(run, ms);
  };
  function run() {
    if (!live) return;
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') {
      // Hidden: nothing is asked; the page coming back looks at once (onVisible).
      timer = null;
      return;
    }
    if (looking) return schedule(step);
    looking = true;
    lastLook = clock.now();
    void o
      .look()
      .then(
        (changed) => {
          step = nextStep(step, o, changed);
        },
        () => {
          step = nextStep(step, o, false);
        },
      )
      .finally(() => {
        looking = false;
        if (live) schedule(step);
      });
  }
  /** The conversations moved: not sooner than the busy step after the last look, a busy chat moves many times a second. */
  const wake = () => {
    if (!live) return;
    const was = step;
    step = busyStep(step, o);
    if (was > step && !looking && timer !== null) schedule(Math.max(0, lastLook + step - clock.now()));
  };
  /** The window was focused: back to the base step, not sooner than it after the last look. */
  const focus = () => {
    if (!live) return;
    const was = step;
    step = o.base;
    if (was > o.base && !looking && timer !== null) schedule(Math.max(0, lastLook + o.base - clock.now()));
  };
  /** The page came back (shown again, the network back): a look at once. */
  const now = () => {
    if (!live) return;
    step = o.base;
    if (!looking) run();
  };
  const onVisible = () => {
    if (document.visibilityState === 'visible') now();
  };

  nudges.add(wake);
  if (unConvs === null) unConvs = conversationsStore.subscribeAll(nudgePolls);
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisible);
  if (typeof window !== 'undefined') {
    window.addEventListener('focus', focus);
    window.addEventListener('online', now);
    if (o.wakeOn) window.addEventListener(o.wakeOn, now);
  }
  run();
  return () => {
    live = false;
    clock.clearTimeout(timer);
    nudges.delete(wake);
    if (nudges.size === 0) {
      unConvs?.();
      unConvs = null;
    }
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisible);
    if (typeof window !== 'undefined') {
      window.removeEventListener('focus', focus);
      window.removeEventListener('online', now);
      if (o.wakeOn) window.removeEventListener(o.wakeOn, now);
    }
  };
};
