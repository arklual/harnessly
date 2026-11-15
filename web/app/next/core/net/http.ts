// HTTP of v2: commands end in a CmdOutcome — the runtime's result, or failed{timeout|network|http} — never a throw,
// never a localized string. Every request carries the device header.

import { deviceId } from '@/next/core/device';
import { pageClock } from '@/next/core/clock';
import {
  DEVICE_HEADER,
  parseCmdResult,
  parseRefusal,
  type CmdOutcome,
  type Refusal,
} from '@/next/protocol/v2';

export type Fetch = (url: string, init: RequestInit) => Promise<Response>;

let fetcher: Fetch = (url, init) => fetch(url, init);

/** The fetch every v2 request goes through (tests put a fake runtime here). */
export function setFetch(next: Fetch): void {
  fetcher = next;
}

const headers = (json: boolean): Record<string, string> => ({
  [DEVICE_HEADER]: deviceId(),
  ...(json ? { 'Content-Type': 'application/json' } : {}),
});

/**
 * What may wait out a failure instead of answering it: given how long the request has gone on and may go on, it says
 * whether to send it again (after waiting as long as it likes). The page's rollout watch sets it (features/rollout):
 * while the hub or a runtime is being replaced, a request that met the gap is sent again until the link is back.
 */
export type Hold = (elapsedMs: number, budgetMs: number) => Promise<boolean>;
let hold: Hold | null = null;

export function setHold(next: Hold | null): void {
  hold = next;
}

/** How long a request may be held in all, whatever its own term. */
export const HOLD_MAX_MS = 30_000;

/**
 * A failure that is the gap of a rollout rather than an answer: no answer at all, or the gateway's — a 502/503/504
 * that is not the runtime's own refusal. A request that changes something is sent again only when it surely never
 * reached the machine: the hub had no link to it, or nothing stood behind the balancer.
 */
export async function isGap(method: string, res: Response | 'timeout' | 'network'): Promise<boolean> {
  if (res === 'timeout') return false;
  const safe = method === 'GET' || method === 'HEAD';
  if (res === 'network') return safe;
  if (res.status !== 502 && res.status !== 503 && res.status !== 504) return false;
  const body: unknown = await res
    .clone()
    .json()
    .catch(() => undefined);
  if (body === undefined) return safe || res.status === 503;
  const code = typeof body === 'object' && body !== null && 'error' in body ? body.error : null;
  if (code === 'machine_offline') return true;
  return safe && code === 'machine_error';
}

/** A request, sent again across the gap of a rollout while the hold says so. */
export async function held(
  send: () => Promise<Response | 'timeout' | 'network'>,
  method: string,
  timeoutMs: number,
): Promise<Response | 'timeout' | 'network'> {
  const clock = pageClock();
  const start = clock.now();
  const budget = Math.min(HOLD_MAX_MS, Math.max(timeoutMs, 10_000));
  for (;;) {
    const res = await send();
    if (!hold || !(await isGap(method, res))) return res;
    if (!(await hold(clock.now() - start, budget))) return res;
  }
}

/** Races a request against timeoutMs on the core's clock, held across a rollout's gap. */
const timed = (
  url: string,
  init: RequestInit,
  timeoutMs: number,
): Promise<Response | 'timeout' | 'network'> =>
  held(() => timedOnce(url, init, timeoutMs), init.method ?? 'GET', timeoutMs);

/** Races a request against timeoutMs on the core's clock. */
async function timedOnce(
  url: string,
  init: RequestInit,
  timeoutMs: number,
): Promise<Response | 'timeout' | 'network'> {
  const ctl = new AbortController();
  const clock = pageClock();
  let timer: unknown;
  const timeout = new Promise<'timeout'>((resolve) => {
    timer = clock.setTimeout(() => {
      ctl.abort();
      resolve('timeout');
    }, timeoutMs);
  });
  try {
    return await Promise.race([
      fetcher(url, { ...init, signal: ctl.signal }).catch(() => 'network' as const),
      timeout,
    ]);
  } finally {
    clock.clearTimeout(timer);
  }
}

/** POST /api/v2/cmd/<op>: the body as is, the answer as CmdOutcome. */
export async function postCommand(url: string, body: unknown, timeoutMs: number): Promise<CmdOutcome> {
  const res = await timed(
    url,
    { method: 'POST', headers: headers(true), body: JSON.stringify(body) },
    timeoutMs,
  );
  if (res === 'timeout' || res === 'network') return { r: 'failed', code: res, status: null };
  let doc: unknown;
  try {
    doc = await res.json();
  } catch {
    return { r: 'failed', code: 'http', status: res.status };
  }
  try {
    return parseCmdResult(doc);
  } catch {
    return { r: 'failed', code: 'http', status: res.status };
  }
}

export type PageOutcome<T, R extends { r: 'refused' } = Refusal> =
  | { ok: true; value: T }
  | R
  | { r: 'failed'; code: 'timeout' | 'network'; status: null }
  | { r: 'failed'; code: 'http'; status: number };

/** GET of a v2 page through its strict reader. */
export const getPage = <T>(url: string, read: (v: unknown) => T, timeoutMs: number) =>
  sendPage('GET', url, read, timeoutMs);

/** What a conditional GET brings: the page with its entity tag, or word that the copy the caller holds still stands. */
export type FreshOutcome<T> =
  { same: true } | { ok: true; value: T; tag: string | null } | Exclude<PageOutcome<T>, { ok: true }>;

/**
 * GET of a page the caller may hold already: `tag` is the entity tag of the held copy (null for none), sent as
 * If-None-Match, and a 304 answers `same` without a body.
 */
export async function getPageIfChanged<T>(
  url: string,
  read: (v: unknown) => T,
  timeoutMs: number,
  tag: string | null,
): Promise<FreshOutcome<T>> {
  const res = await timed(
    url,
    { method: 'GET', headers: { ...headers(false), ...(tag ? { 'If-None-Match': tag } : {}) } },
    timeoutMs,
  );
  if (typeof res !== 'string' && res.status === 304) return { same: true };
  const out = await answerPage(res, read);
  if (!('ok' in out)) return out;
  return { ...out, tag: typeof res === 'string' ? null : res.headers.get('ETag') };
}

/**
 * A v2 resource's POST or DELETE without a body: the answer through the strict reader as a page's is; 204 (nothing
 * to say) reads as null.
 */
export async function sendPage<T>(
  method: 'GET' | 'POST' | 'DELETE',
  url: string,
  read: (v: unknown) => T,
  timeoutMs: number,
): Promise<PageOutcome<T>> {
  return answerPage(await timed(url, { method, headers: headers(false) }, timeoutMs), read);
}

/**
 * A v2 resource's change by PATCH with a JSON body: the answer through the strict reader as a page's is, a refusal
 * (`conflict`, `bad-request`) as the refusal it is.
 */
export async function sendJson<T>(
  url: string,
  body: unknown,
  read: (v: unknown) => T,
  timeoutMs: number,
): Promise<PageOutcome<T>> {
  const res = await timed(
    url,
    { method: 'PATCH', headers: headers(true), body: JSON.stringify(body) },
    timeoutMs,
  );
  return answerPage(res, read);
}

/**
 * A runtime resource's POST or PUT with a JSON body (outside v2's commands): the answer through the strict reader. A
 * resource whose refusal says more than its code (a switch says why) gives the reader of its refusal.
 */
export async function sendBody<T, R extends { r: 'refused' } = Refusal>(
  method: 'POST' | 'PUT',
  url: string,
  body: unknown,
  read: (v: unknown) => T,
  timeoutMs: number,
  readRefusal?: (v: unknown) => R,
): Promise<PageOutcome<T, R>> {
  const res = await timed(url, { method, headers: headers(true), body: JSON.stringify(body) }, timeoutMs);
  return answerPage(res, read, readRefusal);
}

/** A resource's PUT of raw bytes (a recording), typed as the blob is: the answer through the strict reader. */
export async function sendBlob<T>(
  url: string,
  blob: Blob,
  read: (v: unknown) => T,
  timeoutMs: number,
): Promise<PageOutcome<T>> {
  const res = await timed(
    url,
    {
      method: 'PUT',
      headers: { ...headers(false), 'Content-Type': blob.type || 'application/octet-stream' },
      body: blob,
    },
    timeoutMs,
  );
  return answerPage(res, read);
}

async function answerPage<T, R extends { r: 'refused' } = Refusal>(
  res: Response | 'timeout' | 'network',
  read: (v: unknown) => T,
  readRefusal?: (v: unknown) => R,
): Promise<PageOutcome<T, R>> {
  if (res === 'timeout' || res === 'network') return { r: 'failed', code: res, status: null };
  if (res.status === 204) return { ok: true, value: read(null) };
  let doc: unknown;
  try {
    doc = await res.json();
  } catch {
    return { r: 'failed', code: 'http', status: res.status };
  }
  try {
    // `r` is a refusal's mark; a change that was taken (`accepted`) carries its result beside it.
    const refused = typeof doc === 'object' && doc !== null && 'r' in doc && doc.r !== 'accepted';
    // Without a reader of its own the refusal is the plain one, which is R's default.
    if (refused) return (readRefusal ?? parseRefusal)(doc) as R;
    return { ok: true, value: read(doc) };
  } catch {
    return { r: 'failed', code: 'http', status: res.status };
  }
}
