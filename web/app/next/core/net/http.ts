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

/** Races a request against timeoutMs on the core's clock. */
async function timed(
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

export type PageOutcome<T> =
  | { ok: true; value: T }
  | Refusal
  | { r: 'failed'; code: 'timeout' | 'network'; status: null }
  | { r: 'failed'; code: 'http'; status: number };

/** GET of a v2 page through its strict reader. */
export const getPage = <T>(url: string, read: (v: unknown) => T, timeoutMs: number) =>
  sendPage('GET', url, read, timeoutMs);

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

/** A runtime resource's POST or PUT with a JSON body (outside v2's commands): the answer through the strict reader. */
export async function sendBody<T>(
  method: 'POST' | 'PUT',
  url: string,
  body: unknown,
  read: (v: unknown) => T,
  timeoutMs: number,
): Promise<PageOutcome<T>> {
  const res = await timed(url, { method, headers: headers(true), body: JSON.stringify(body) }, timeoutMs);
  return answerPage(res, read);
}

async function answerPage<T>(
  res: Response | 'timeout' | 'network',
  read: (v: unknown) => T,
): Promise<PageOutcome<T>> {
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
    if (refused) return parseRefusal(doc);
    return { ok: true, value: read(doc) };
  } catch {
    return { r: 'failed', code: 'http', status: res.status };
  }
}
