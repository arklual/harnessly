export type TLaunchKind = 'model' | 'effort';

export interface ILaunchChange {
  kind: TLaunchKind;
  value: string;
  from: string | null;
  state: 'applying' | 'queued' | 'confirmed';
  /** Confirmed picks stop overriding the reported value at this time (ms). */
  expiresAt?: number;
  /** A pick the agent refused as busy waits for this time (ms) or for a observed busy turn, whichever comes first. */
  retryAt?: number;
  attempts?: number;
}

export const CONFIRMED_TTL_MS = 15_000;
export const RETRY_BASE_MS = 2_000;
export const RETRY_MAX_MS = 30_000;

/** Picks not yet confirmed by the agent, by session name. */
export type TLaunchQueue = Record<string, Partial<Record<TLaunchKind, ILaunchChange>>>;

const KIND_ORDER: readonly TLaunchKind[] = ['model', 'effort'];

export const requestChange = (
  queue: TLaunchQueue,
  session: string,
  change: { kind: TLaunchKind; value: string; from: string | null },
  busy: boolean,
): TLaunchQueue => ({
  ...queue,
  [session]: { ...queue[session], [change.kind]: { ...change, state: busy ? 'queued' : 'applying' } },
});

const without = (queue: TLaunchQueue, session: string, kind: TLaunchKind): TLaunchQueue => {
  const { [kind]: _dropped, ...rest } = queue[session] ?? {};
  const { [session]: _session, ...others } = queue;
  return Object.keys(rest).length ? { ...others, [session]: rest } : others;
};

const replaced = (queue: TLaunchQueue, session: string, change: ILaunchChange): TLaunchQueue => ({
  ...queue,
  [session]: { ...queue[session], [change.kind]: change },
});

/**
 * Only the answer to the current pick settles it. Success keeps the pick shown as confirmed until the agent reports
 * the new value; failure drops it, so the chip falls back to what the agent reports.
 */
export const settleChange = (
  queue: TLaunchQueue,
  session: string,
  kind: TLaunchKind,
  ok: boolean,
  value: string,
  now: number,
): TLaunchQueue => {
  const current = queue[session]?.[kind];
  if (!current || current.value !== value) return queue;
  return ok
    ? replaced(queue, session, { ...current, state: 'confirmed', expiresAt: now + CONFIRMED_TTL_MS })
    : without(queue, session, kind);
};

/** Confirmed picks the agent never reported back are dropped, so a switch it silently ignored cannot stay shown forever. */
export const expireConfirmed = (queue: TLaunchQueue, now: number): TLaunchQueue => {
  let next = queue;
  for (const [session, picks] of Object.entries(queue)) {
    for (const kind of KIND_ORDER) {
      const change = picks[kind];
      if (change?.state === 'confirmed' && (change.expiresAt ?? 0) <= now)
        next = without(next, session, kind);
    }
  }
  return next;
};

export const retryDelay = (attempts: number): number =>
  Math.min(RETRY_BASE_MS * 2 ** (attempts - 1), RETRY_MAX_MS);

/** The agent was busy after all: the pick waits for the end of the turn again. */
export const requeueChange = (
  queue: TLaunchQueue,
  session: string,
  kind: TLaunchKind,
  value: string,
  now: number,
): TLaunchQueue => {
  const current = queue[session]?.[kind];
  if (!current || current.value !== value) return queue;
  const attempts = (current.attempts ?? 0) + 1;
  return replaced(queue, session, {
    ...current,
    state: 'queued',
    attempts,
    retryAt: now + retryDelay(attempts),
  });
};

const isDue = (change: ILaunchChange | undefined, now: number): change is ILaunchChange =>
  change?.state === 'queued' && (change.retryAt === undefined || now >= change.retryAt);

/** The turn ended: every due queued pick of the session is released in one step. */
export const releaseQueued = (queue: TLaunchQueue, session: string, now: number): TLaunchQueue => {
  const picks = queue[session];
  if (!picks || !KIND_ORDER.some((kind) => isDue(picks[kind], now))) return queue;
  const next = { ...picks };
  for (const kind of KIND_ORDER) {
    const change = picks[kind];
    if (isDue(change, now)) next[kind] = { ...change, state: 'applying' };
  }
  return { ...queue, [session]: next };
};

/** A turn was seen running: picks that were refused as busy no longer wait out their backoff. */
export const observeBusy = (queue: TLaunchQueue, session: string): TLaunchQueue => {
  const picks = queue[session];
  if (!picks || !KIND_ORDER.some((kind) => picks[kind]?.retryAt !== undefined)) return queue;
  const next = { ...picks };
  for (const kind of KIND_ORDER) {
    const change = picks[kind];
    if (change?.retryAt !== undefined) next[kind] = { ...change, retryAt: undefined, attempts: undefined };
  }
  return { ...queue, [session]: next };
};

/** Drops a confirmed pick once the agent reports a value other than the old one. */
export const pruneConfirmed = (
  queue: TLaunchQueue,
  session: string,
  kind: TLaunchKind,
  reported: string | null,
): TLaunchQueue => {
  const current = queue[session]?.[kind];
  if (current?.state !== 'confirmed' || reported === current.from) return queue;
  return without(queue, session, kind);
};

/** The next pick to send for a session whose previous request has settled: the model before the effort. */
export const nextApplying = (queue: TLaunchQueue, session: string): ILaunchChange | null => {
  for (const kind of KIND_ORDER) {
    const change = queue[session]?.[kind];
    if (change?.state === 'applying') return change;
  }
  return null;
};

export const dueChanges = (
  queue: TLaunchQueue,
  session: string,
  busy: boolean,
  now: number,
): ILaunchChange[] => {
  if (busy) return [];
  return KIND_ORDER.flatMap((kind) => {
    const change = queue[session]?.[kind];
    return isDue(change, now) ? [change] : [];
  });
};

/** When the earliest backed-off pick of the session may be sent again, if any. */
export const nextRetryAt = (queue: TLaunchQueue, session: string): number | null => {
  const times = KIND_ORDER.flatMap((kind) => {
    const change = queue[session]?.[kind];
    return change?.state === 'queued' && change.retryAt !== undefined ? [change.retryAt] : [];
  });
  return times.length ? Math.min(...times) : null;
};

export const shownValue = (
  queue: TLaunchQueue,
  session: string,
  kind: TLaunchKind,
  reported: string | null,
): { value: string | null; pending: 'applying' | 'queued' | null } => {
  const change = queue[session]?.[kind];
  if (!change) return { value: reported, pending: null };
  if (change.state === 'confirmed')
    return { value: reported !== change.from ? reported : change.value, pending: null };
  return { value: change.value, pending: change.state === 'queued' ? 'queued' : 'applying' };
};
