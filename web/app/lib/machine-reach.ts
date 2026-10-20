/**
 * When the page treats a machine as down and shows its conversations from the cluster instead: the gateway
 * found it not answering (its probe already waits out a slow second), or the stored list says it missed its heartbeats
 * and it has not answered since. A gateway that could not be asked changes nothing. One answer brings it back.
 *
 * A busy machine is not a dead one: with one miss enough, runner-2 under a load of 30 flipped Sergey's page to the
 * stored view and back every few seconds, and each flip swapped the whole screen, sidebar and dictated text with it.
 * The machine is shown down only after several misses in a row over at least DOWN_AFTER_MS.
 */

export const REACH_POLL_MS = 4_000;
/** After a miss the next probe comes soon: a machine that refuses is shown from the cluster within a second or two. */
export const REACH_RECHECK_MS = 3_000;
export const MISSES_FOR_DOWN = 3;
export const DOWN_AFTER_MS = 15_000;

export interface IReachState {
  down: boolean;
  /** When the machine was first seen down; `null` while it answers. */
  since: number | null;
  misses: number;
  /** The machine answered the last probe: a stale heartbeat in the list does not outweigh that. */
  answered: boolean;
}

export const REACH_UNKNOWN: IReachState = { down: false, since: null, misses: 0, answered: false };
const REACH_UP: IReachState = { down: false, since: null, misses: 0, answered: true };

export interface IReachAnswer {
  reachable: boolean;
  since: number | null;
}

/** `answer` is the gateway's verdict, or `null` when the gateway itself could not be asked. */
export const nextReach = (state: IReachState, answer: IReachAnswer | null, now: number): IReachState => {
  if (answer === null) return state;
  if (answer.reachable) return state.answered && !state.down && !state.misses ? state : REACH_UP;
  const misses = state.misses + 1;
  const firstMissAt = state.since ?? now;
  const down = state.down || (misses >= MISSES_FOR_DOWN && now - firstMissAt >= DOWN_AFTER_MS);
  return { down, misses, since: state.since ?? answer.since ?? now, answered: false };
};

/** A stale heartbeat in the list alone does not take the screen away: the gateway must have missed the machine too. */
export const isMachineDown = (state: IReachState, listedAvailable: boolean | undefined): boolean =>
  state.down || (listedAvailable === false && !state.answered && state.misses >= MISSES_FOR_DOWN);

/** The session a stored transcript belongs to: Claude names the file after it, Codex ends `rollout-…-<id>.jsonl` with it. */
export const sessionOfTranscript = (path: string | undefined): string | null => {
  if (!path) return null;
  const match = /([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$/i.exec(path);
  return match ? match[1].toLowerCase() : null;
};
