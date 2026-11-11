// When the page asks the cluster for its copy of the chats: on mount, on a steady beat while the page is visible, and
// out of turn when a link changes — but never more often than a set gap, so machines that reconnect together make one
// request, and never from a hidden tab (what it missed is asked when the tab is shown again).
import { pageClock } from '@/next/core/clock';

export interface CentralBeatOptions {
  /** Asks for the list; `remerge` when a link changed since the last request. */
  refresh: (remerge: boolean) => unknown;
  /** The steady beat. */
  everyMs: number;
  /** The least time between a request and an out-of-turn one. */
  minGapMs: number;
  visible: () => boolean;
}

export interface CentralBeat {
  /** A link went down or came back. */
  linksChanged(): void;
  /** The tab was shown or hidden. */
  visibilityChanged(): void;
  stop(): void;
}

export function startCentralBeat({ refresh, everyMs, minGapMs, visible }: CentralBeatOptions): CentralBeat {
  const clock = pageClock();
  let last = clock.now();
  let dirty = false;
  let missed = false;
  let timer: unknown;

  function arm(ms: number): void {
    clock.clearTimeout(timer);
    timer = clock.setTimeout(tick, Math.max(0, ms));
  }
  function ask(): void {
    const remerge = dirty;
    dirty = false;
    missed = false;
    last = clock.now();
    arm(everyMs);
    void refresh(remerge);
  }
  function tick(): void {
    if (visible()) {
      ask();
      return;
    }
    // A hidden tab asks nothing; the beat goes on, and the request is owed when the tab is shown.
    missed = true;
    arm(everyMs);
  }
  /** Asks out of turn, at once or as soon as the gap since the last request allows. */
  function askSoon(): void {
    const wait = last + minGapMs - clock.now();
    if (wait <= 0) ask();
    else arm(Math.min(wait, last + everyMs - clock.now()));
  }

  void refresh(false);
  arm(everyMs);

  return {
    linksChanged() {
      dirty = true;
      if (visible()) askSoon();
    },
    visibilityChanged() {
      if (!visible()) return;
      if (missed) ask();
      else if (dirty) askSoon();
    },
    stop() {
      clock.clearTimeout(timer);
    },
  };
}
