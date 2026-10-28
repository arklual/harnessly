// The clocks of the show and the recording: «m:ss», the plan of the talk (how long each slide may take so that the whole
// fits the minutes asked for), «в графике» / behind, and a stopwatch that does not run while paused. Pure; time comes in
// as an argument.

/** How far past the plan a slide may run before the presenter is told they are behind. */
export const BEHIND_GRACE_SEC = 20;

const two = (n: number) => String(n).padStart(2, '0');

/** 75 → «1:15»; an hour or more → «1:02:03». Negative and non-finite → «0:00». */
export const formatClock = (seconds: number): string => {
  const s = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0;
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}:${two(m)}:${two(s % 60)}` : `${m}:${two(s % 60)}`;
};

/** The same for milliseconds (the video's length, «−1:12» of a cut). */
export const formatMs = (ms: number): string => formatClock(Math.round(ms / 1000));

/**
 * Seconds planned for each slide: the minutes asked for, shared in proportion to the slides' own length (the notes'
 * seconds); equal when no slide has notes. Without minutes (the length was left to the agent) each slide keeps the
 * time its own notes take.
 */
export const slideBudgets = (seconds: readonly number[], minutes: number): number[] => {
  if (seconds.length === 0) return [];
  if (minutes <= 0) return seconds.map((s) => Math.max(0, s));
  const total = Math.max(1, minutes) * 60;
  const sum = seconds.reduce((a, b) => a + Math.max(0, b), 0);
  return seconds.map((s) => (sum > 0 ? (Math.max(0, s) / sum) * total : total / seconds.length));
};

export const PACE = { onTime: 'onTime', behind: 'behind' } as const;
export type TPace = (typeof PACE)[keyof typeof PACE];

/** `elapsed` seconds into the talk while on slide `index` (0-based): behind when the slide's planned end has passed. */
export const pace = (elapsed: number, index: number, budgets: readonly number[]): TPace => {
  let end = 0;
  for (let i = 0; i <= index && i < budgets.length; i += 1) end += budgets[i] ?? 0;
  return elapsed > end + BEHIND_GRACE_SEC ? PACE.behind : PACE.onTime;
};

/** A stopwatch that stands still while paused; `now` is any monotonic ms clock. */
export class Stopwatch {
  private began: number | null = null;
  private held = 0;
  private pausedAt: number | null = null;

  start(now: number): void {
    this.began = now;
    this.held = 0;
    this.pausedAt = null;
  }

  pause(now: number): void {
    if (this.began !== null && this.pausedAt === null) this.pausedAt = now;
  }

  resume(now: number): void {
    if (this.pausedAt === null) return;
    this.held += now - this.pausedAt;
    this.pausedAt = null;
  }

  get paused(): boolean {
    return this.pausedAt !== null;
  }

  get started(): boolean {
    return this.began !== null;
  }

  /** Milliseconds run, not counting the time paused. */
  elapsed(now: number): number {
    if (this.began === null) return 0;
    const to = this.pausedAt ?? now;
    return Math.max(0, to - this.began - this.held);
  }
}
