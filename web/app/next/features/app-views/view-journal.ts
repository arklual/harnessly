// An app view's part of the machine's journal (HRN-976): which source its lines are, and what the machine knows of why
// a view did not connect, for the card and the debug section to say.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { TRACE_HEADER, type TJournalSource } from '@/next/core/journal';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

/** Sinopia, the builtin app, has lines of its own source. */
export const SINOPIA_APP = 'harnessly.sinopia';

export const journalSource = (appId: string): TJournalSource => (appId === SINOPIA_APP ? 'sinopia' : 'app');

/** How long the page waits for the machine's answer before it says the plain words. */
export const REASON_WAIT_MS = 2500;

/**
 * The last error the machine's journal holds for the app (and chat) since the view was mounted, in a few words
 * («скрипт __hl/bridge-shim.js: 401»); "" when it knows none or does not answer in time.
 */
export const journalReason = async (
  m: MachineId,
  app: string,
  chat: string | null,
  since: number,
  trace = '',
  fetcher: typeof fetch = fetch,
): Promise<string> => {
  const q = new URLSearchParams({ app, since: String(since) });
  // The mount's own lines: an earlier view of the same app is another story.
  if (trace !== '') q.set('trace', trace);
  if (chat !== null) q.set('chat', chat);
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), REASON_WAIT_MS);
  try {
    const res = await fetcher(runtimePath(platformPath(`/journal/reason?${q.toString()}`), m), {
      credentials: 'same-origin',
      signal: ctl.signal,
    });
    if (!res.ok) return '';
    const body: unknown = await res.json().catch(() => null);
    const reason = typeof body === 'object' && body !== null && 'reason' in body ? body.reason : '';
    return typeof reason === 'string' ? reason.slice(0, 300) : '';
  } catch {
    return '';
  } finally {
    clearTimeout(timer);
  }
};

/** The trace a view's address carries for the gateways in front of the machine, which log their refusals with it. */
export const TRACE_PARAM = '__hl_trace';

/** The frame's address with the mount's trace in its query; the address as it was when it cannot be read. */
export const withTraceParam = (src: string, trace: string): string => {
  try {
    const u = new URL(src, window.location.href);
    u.searchParams.set(TRACE_PARAM, trace);
    return u.origin === window.location.origin && src.startsWith('/')
      ? `${u.pathname}${u.search}${u.hash}`
      : u.href;
  } catch {
    return src;
  }
};

/** What the page saw of its frame: the address, when it was given, when it loaded, a CSP refusal of the page. */
export interface IFrameSeen {
  src: string;
  at: number;
  loadedAt: number | null;
  csp: string | null;
}

/** How long the page's own look at a view's address may take. */
export const PROBE_WAIT_MS = 4000;

/** The page's own word on a frame the machine never saw a request of: why, and the status when it read one. */
export interface IFrameReason {
  text: string;
  status?: number;
}

const hostOf = (src: string): string => {
  try {
    return new URL(src, window.location.href).host;
  } catch {
    return '';
  }
};

/**
 * Why a frame did not connect, told by the page when the machine's journal holds nothing of it: the request stopped in
 * front of the machine (a gateway's sign-in, a refusal, the network) or the page's own policy. A frame of the page's
 * origin (the hub's slots by path) is asked again with a fetch, which reads the status and a redirect; another
 * origin's answer the page cannot read, and its gateway tells the frame's refusal itself (hlViewError). "" when the
 * page knows nothing more.
 */
export const frameReason = async (
  seen: IFrameSeen | null,
  trace: string,
  fetcher: typeof fetch = fetch,
): Promise<IFrameReason> => {
  if (seen === null) return { text: '' };
  const host = hostOf(seen.src);
  if (seen.csp !== null) return { text: t('next.appfail.frame.csp', { host, directive: seen.csp }) };
  let same = false;
  try {
    same = new URL(seen.src, window.location.href).origin === window.location.origin;
  } catch {
    return { text: '' };
  }
  if (same) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), PROBE_WAIT_MS);
    try {
      const res = await fetcher(seen.src, {
        credentials: 'same-origin',
        redirect: 'manual',
        cache: 'no-store',
        headers: { [TRACE_HEADER]: trace },
        signal: ctl.signal,
      });
      if (res.type === 'opaqueredirect') return { text: t('next.appfail.frame.redirect', { host }) };
      if (!res.ok) {
        const body: unknown = await res.json().catch(() => null);
        const code = codeOf(body);
        return {
          text: t('next.appfail.frame.status', {
            host,
            status: res.status,
            code: code === '' ? '' : ` (${code})`,
          }),
          status: res.status,
        };
      }
    } catch {
      return { text: t('next.appfail.frame.timeout', { host, s: PROBE_WAIT_MS / 1000 }) };
    } finally {
      clearTimeout(timer);
    }
    return { text: '' };
  }
  if (seen.loadedAt === null) return { text: t('next.appfail.frame.noLoad', { host }) };
  return { text: t('next.appfail.frame.unreached', { host }) };
};

const codeOf = (body: unknown): string => {
  if (typeof body !== 'object' || body === null) return '';
  const b = body as { code?: unknown; error?: unknown };
  if (typeof b.code === 'string') return b.code.slice(0, 60);
  if (typeof b.error === 'object' && b.error !== null) {
    const c = (b.error as { code?: unknown }).code;
    if (typeof c === 'string') return c.slice(0, 60);
  }
  return '';
};
