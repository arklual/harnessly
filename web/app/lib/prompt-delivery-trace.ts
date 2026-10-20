/**
 * The steps of one sent message on the browser's clock, so `prompt_delivery` says where its seconds went:
 * which way it left, when it left, when the runtime said it was typed, when Claude's prompt-submit hook reached the page,
 * when the transcript brought it back. Kept by the text the person typed, which the transcript entry starts with, and
 * by its id, which the composer's pending row carries (the text of a picture message or a Codex record differs).
 */
export type TDeliveryRoute = 'terminal' | 'runtime' | 'queue' | 'send-now' | 'attachments';

export interface IDeliveryTrace {
  id: string;
  route: TDeliveryRoute;
  tabId: string | null;
  pressAt: number;
  dispatchAt?: number;
  ackAt?: number;
  acceptAt?: number;
  /** When the agent first had it: the runtime typed it in, or Claude's prompt-submit hook said so. */
  takenAt?: number;
  /** A message with pictures: when they were uploaded and it could go out. */
  uploadAt?: number;
  /** What the runtime measured between the request and its Enter, by step. */
  server?: Record<string, number>;
}

const KEY_CHARS = 48;
const KEEP_MS = 10 * 60_000;
const ACCEPT_WINDOW_MS = 60_000;
const traces = new Map<string, IDeliveryTrace>();
const listeners = new Set<(trace: IDeliveryTrace) => void>();
let seq = 0;

/**
 * Called when a trace learns the agent has the message, so its pending row stops saying «sending»: once when
 * it is first taken, and again when Claude's hook confirms a message the runtime said it typed.
 */
export const onDeliveryTaken = (listener: (trace: IDeliveryTrace) => void): (() => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** A queue on the server holding the message is not the agent having it. */
const markTaken = (trace: IDeliveryTrace, now: number) => {
  if (trace.route === 'queue') return;
  const first = trace.takenAt === undefined;
  if (first) trace.takenAt = now;
  if (first || trace.acceptAt === now) for (const listener of [...listeners]) listener(trace);
};

const keyOf = (text: string) => text.replace(/\s+/g, ' ').trim().slice(0, KEY_CHARS);

const prune = (now: number) => {
  for (const [key, trace] of traces) if (now - trace.pressAt > KEEP_MS) traces.delete(key);
};

export const startDeliveryTrace = (
  text: string,
  route: TDeliveryRoute,
  tabId: string | null | undefined,
  now = Date.now(),
): IDeliveryTrace | null => {
  const key = keyOf(text);
  if (!key) return null;
  prune(now);
  seq += 1;
  const trace: IDeliveryTrace = {
    id: `t${now.toString(36)}-${seq}`,
    route,
    tabId: tabId ?? null,
    pressAt: now,
  };
  traces.set(key, trace);
  return trace;
};

/**
 * Claude's prompt-submit hook of a tab reached the page: the oldest message of that tab still waiting for one was taken.
 * `acceptMs` is the runtime's own Enter-to-hook time, when the runtime pressed or passed on that Enter.
 */
export const markAccepted = (tabId: string, acceptMs?: number, now = Date.now()) => {
  let oldest: IDeliveryTrace | null = null;
  for (const trace of traces.values()) {
    if (trace.tabId !== tabId || trace.acceptAt !== undefined || now - trace.pressAt > ACCEPT_WINDOW_MS)
      continue;
    if (!oldest || trace.pressAt < oldest.pressAt) oldest = trace;
  }
  if (!oldest) return;
  oldest.acceptAt = now;
  if (acceptMs !== undefined) oldest.server = { ...oldest.server, accept: acceptMs };
  markTaken(oldest, now);
};

export const markDispatched = (trace: IDeliveryTrace | null, now = Date.now()) => {
  if (trace && trace.dispatchAt === undefined) trace.dispatchAt = now;
};

export const markAcked = (
  trace: IDeliveryTrace | null,
  server?: Record<string, number>,
  now = Date.now(),
) => {
  if (!trace) return;
  trace.ackAt = now;
  if (server) trace.server = { ...trace.server, ...server };
  markTaken(trace, now);
};

export const markUploaded = (trace: IDeliveryTrace | null, now = Date.now()) => {
  if (trace && trace.uploadAt === undefined) trace.uploadAt = now;
};

/** The trace a message just sent from the composer started, for its pending row to carry. */
export const latestTraceFor = (text: string): IDeliveryTrace | null => traces.get(keyOf(text)) ?? null;

/** The trace of a delivered entry, taken once: by the id its pending row carried, else by its text. */
export const takeDeliveryTrace = (entryText: string, traceId?: string): IDeliveryTrace | null => {
  if (traceId) {
    for (const [stored, trace] of traces) {
      if (trace.id !== traceId) continue;
      traces.delete(stored);
      return trace;
    }
  }
  const key = keyOf(entryText);
  for (const [stored, trace] of traces) {
    if (key.startsWith(stored) || stored.startsWith(key)) {
      traces.delete(stored);
      return trace;
    }
  }
  return null;
};

/** The steps as metric properties, in milliseconds from the press. */
export const traceProperties = (
  trace: IDeliveryTrace | null,
  deliveredAt: number,
): Record<string, string | number | null> => {
  if (!trace) return { route: null };
  const since = (at: number | undefined) => (at === undefined ? null : Math.max(0, at - trace.pressAt));
  const props: Record<string, string | number | null> = {
    route: trace.route,
    step_dispatch_ms: since(trace.dispatchAt),
    step_ack_ms: since(trace.ackAt),
    step_accept_ms: since(trace.acceptAt),
    step_taken_ms: since(trace.takenAt),
    step_upload_ms: since(trace.uploadAt),
    step_return_ms: trace.acceptAt === undefined ? null : Math.max(0, deliveredAt - trace.acceptAt),
  };
  for (const [name, ms] of Object.entries(trace.server ?? {})) props[`server_${name}_ms`] = ms;
  return props;
};
