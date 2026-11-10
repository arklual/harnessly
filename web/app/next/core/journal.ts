// The page's part of the machine's journal (HRN-976): navigation, key clicks, the page's requests to the runtime (a
// failure, a change, a slow read — with status and time), console errors, uncaught exceptions, files that did not load
// and refusals of the content policy, and what the apps' views and Sinopia report. Lines gather for a moment and go to
// the runtime of the machine in one request (POST <platform>/journal), where `harnessly logs` and the MCP tool
// harnessly_logs read them.
//
// Cheap by design: nothing is written for a read that went well, a line is a handful of short fields, the queue is
// bounded and a line past it is dropped, and the report goes with the original fetch, so it is never itself reported.
// No request body, no query string, no text the person typed: an address is its path, a click is the button's name.
import { activeMachineStore } from '@/next/stores/machines';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

export type TJournalLevel = 'error' | 'warn' | 'info';
export type TJournalSource = 'web' | 'app' | 'sinopia';

/** One line, as the runtime keeps it (internal/journal Entry). */
export interface IJournalEntry {
  at: number;
  level: TJournalLevel;
  src: TJournalSource;
  ev: string;
  text?: string;
  trace?: string;
  chat?: string;
  app?: string;
  view?: string;
  tool?: string;
  method?: string;
  url?: string;
  status?: number;
  dur?: number;
}

/** The headers a request of the page carries to the runtime: the trace of its action and its chat. */
export const TRACE_HEADER = 'X-Hl-Trace';
export const CHAT_HEADER = 'X-Hl-Chat';

/** How long lines gather; how many one report carries; how many may wait; the longest text. */
export const JOURNAL_FLUSH_MS = 3000;
/** An error goes sooner: the agent that reads the journal right after a failure should see it. */
export const JOURNAL_ERROR_FLUSH_MS = 400;
export const JOURNAL_BATCH = 100;
export const JOURNAL_QUEUE = 400;
export const JOURNAL_TEXT = 500;
/** A read slower than this is written down. */
export const JOURNAL_SLOW_MS = 3000;
/** How long a click's trace lasts for the requests that follow it. */
export const ACTION_TRACE_MS = 10_000;
/** The same line (event, address, text) is written once in this time. */
const SAME_LINE_MS = 5000;

const queues = new Map<MachineId, IJournalEntry[]>();
const recent = new Map<string, number>();
let timer: ReturnType<typeof setTimeout> | null = null;
let soon = false;
let lastMachine: MachineId | null = null;
let action: { trace: string; until: number } | null = null;
let originalFetch: typeof fetch | null = null;
let send: ((m: MachineId, entries: IJournalEntry[]) => Promise<unknown>) | null = null;

/** A fresh trace id; `prefix` says what started it (a: an action, v: a view). */
export const newTrace = (prefix = 't-'): string => {
  const bytes = new Uint8Array(6);
  crypto.getRandomValues(bytes);
  return prefix + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
};

/** The path of an address with no query, no fragment and no token-like part. */
/** A path segment that reads as a key rather than a word: a digit in it, or both cases. */
const randomLooking = (p: string): boolean => /\d/.test(p) || (/[a-z]/.test(p) && /[A-Z]/.test(p));

export const cleanUrl = (raw: string): string => {
  let path = raw;
  try {
    const u = new URL(raw, typeof location === 'undefined' ? 'https://x.invalid' : location.href);
    const same = typeof location !== 'undefined' && u.origin === location.origin;
    path = same ? u.pathname : `${u.origin}${u.pathname}`;
  } catch {
    path = raw.split(/[?#]/)[0] ?? '';
  }
  return path
    .split('/')
    .map((p) =>
      (/^[A-Za-z0-9_-]{20,}$/.test(p) || /^[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]{39,}$/.test(p)) && randomLooking(p)
        ? '…'
        : p,
    )
    .join('/')
    .slice(0, 300);
};

/** The chat the page shows now, from its address (`/c/<id>`), or null. */
export const currentChat = (): string | null => {
  if (typeof location === 'undefined') return null;
  const m = /\/c\/([^/?#]+)/.exec(location.pathname);
  if (!m?.[1]) return null;
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return m[1];
  }
};

/** The trace of the action under way (a click in the last few seconds), or null. */
export const currentTrace = (): string | null =>
  action !== null && Date.now() < action.until ? action.trace : null;

/** Starts a new action: the requests of the next seconds carry its trace. */
export const startAction = (): string => {
  const trace = newTrace('a-');
  action = { trace, until: Date.now() + ACTION_TRACE_MS };
  return trace;
};

/** Makes `trace` the action under way: the requests of the next seconds carry it (a view's mount, not a click). */
export const followTrace = (trace: string): void => {
  action = { trace, until: Date.now() + ACTION_TRACE_MS };
};

const machineOf = (url: string): MachineId | null => {
  const m = /[?&]machine=([^&#]+)/.exec(url);
  if (!m?.[1]) return null;
  try {
    return decodeURIComponent(m[1]) as MachineId;
  } catch {
    return null;
  }
};

/** Writes a line down for the machine given (else the one the page talked to last, else the active one). */
export const journal = (
  entry: Omit<IJournalEntry, 'at'> & { at?: number },
  machine?: MachineId | null,
): void => {
  const m = machine ?? lastMachine ?? activeMachineStore.get();
  if (m === null) return;
  const line: IJournalEntry = { ...entry, at: entry.at ?? Date.now() };
  if (line.text !== undefined) line.text = line.text.slice(0, JOURNAL_TEXT);
  line.chat ??= currentChat() ?? undefined;
  line.trace ??= currentTrace() ?? undefined;
  const key = `${line.ev}|${line.url ?? ''}|${line.status ?? ''}|${line.text ?? ''}|${line.app ?? ''}`;
  const seen = recent.get(key);
  if (seen !== undefined && line.at - seen < SAME_LINE_MS) return;
  if (recent.size > 500) recent.clear();
  recent.set(key, line.at);
  const q = queues.get(m) ?? [];
  if (q.length >= JOURNAL_QUEUE) return;
  q.push(line);
  queues.set(m, q);
  if (line.level === 'error' && !soon) {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(flushJournal, JOURNAL_ERROR_FLUSH_MS);
    soon = true;
  }
  timer ??= setTimeout(flushJournal, JOURNAL_FLUSH_MS);
};

const post = (m: MachineId, entries: IJournalEntry[], keepalive = false): Promise<unknown> => {
  if (send !== null) return send(m, entries);
  const f = originalFetch ?? fetch;
  return f(runtimePath(platformPath('/journal'), m), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ entries }),
    keepalive,
  });
};

/** Sends what waits now. */
export const flushJournal = (keepalive = false): void => {
  if (timer !== null) clearTimeout(timer);
  timer = null;
  soon = false;
  for (const [m, q] of queues) {
    queues.delete(m);
    for (let i = 0; i < q.length; i += JOURNAL_BATCH) {
      void post(m, q.slice(i, i + JOURNAL_BATCH), keepalive).catch(() => undefined);
    }
  }
};

/** Tests put their own sender (null: the runtime). */
export const setJournalSender = (next: typeof send): void => {
  send = next;
};

const textOf = (v: unknown): string => {
  if (v instanceof Error) return `${v.name}: ${v.message}`;
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v)?.slice(0, JOURNAL_TEXT) ?? String(v);
  } catch {
    return String(v);
  }
};

const isJournalUrl = (url: string): boolean =>
  /\/journal(\?|$)/.test(url) || /\/apps\/view-log(\?|$)/.test(url);

/** Same origin as the page: a request a header may be added to without a preflight of its own. */
const sameOrigin = (url: string): boolean => {
  try {
    return new URL(url, location.href).origin === location.origin;
  } catch {
    return false;
  }
};

const wrapFetch = (orig: typeof fetch): typeof fetch =>
  async function journaledFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (isJournalUrl(url) || !sameOrigin(url)) return orig(input, init);
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    const machine = machineOf(url);
    if (machine !== null) lastMachine = machine;
    const trace = currentTrace();
    const chat = currentChat();
    let call = init;
    // A Request object keeps its own headers; a plain call gets the trace and the chat.
    if (!(input instanceof Request) && (trace !== null || chat !== null)) {
      const headers = new Headers(init?.headers);
      if (trace !== null && !headers.has(TRACE_HEADER)) headers.set(TRACE_HEADER, trace);
      if (chat !== null && !headers.has(CHAT_HEADER)) headers.set(CHAT_HEADER, chat);
      call = { ...init, headers };
    }
    const start = performance.now();
    const at = Date.now();
    try {
      const res = await orig(input, call);
      const dur = Math.round(performance.now() - start);
      const changes = method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS';
      // A stream's answer comes at once and its body never ends: its time says nothing.
      const stream = res.headers.get('content-type')?.startsWith('text/event-stream') ?? false;
      if (!res.ok || changes || (!stream && dur >= JOURNAL_SLOW_MS)) {
        journal(
          {
            at,
            level: res.status >= 500 ? 'error' : res.ok ? 'info' : 'warn',
            src: 'web',
            ev: 'fetch',
            method,
            url: cleanUrl(url),
            status: res.status,
            dur,
            ...(trace === null ? {} : { trace }),
          },
          machine,
        );
      }
      return res;
    } catch (e) {
      // The caller's own abort is not a failure of the way.
      if (init?.signal?.aborted || (e instanceof DOMException && e.name === 'AbortError')) throw e;
      journal(
        {
          at,
          level: 'error',
          src: 'web',
          ev: 'fetch',
          method,
          url: cleanUrl(url),
          text: `no answer: ${textOf(e)}`,
          dur: Math.round(performance.now() - start),
          ...(trace === null ? {} : { trace }),
        },
        machine,
      );
      throw e;
    }
  };

/** The name of a clicked control: its data-action, aria-label or title — never its text or a field's value. */
const controlName = (target: EventTarget | null): string | null => {
  if (!(target instanceof Element)) return null;
  const el = target.closest('[data-action],button,[role="button"],[role="menuitem"],a[href]');
  if (el === null) return null;
  const name =
    el.getAttribute('data-action') ?? el.getAttribute('aria-label') ?? el.getAttribute('title') ?? null;
  return name === null || name.trim() === '' ? null : name.trim().slice(0, 80);
};

let stop: (() => void) | null = null;

/** Starts the page's journal: wraps fetch and listens for errors, navigation and clicks. Returns the stop. */
export const startJournal = (): (() => void) => {
  if (stop !== null || typeof window === 'undefined') return stop ?? (() => undefined);
  originalFetch = window.fetch.bind(window);
  window.fetch = wrapFetch(originalFetch);

  const onError = (e: Event) => {
    const t = e.target;
    // A file that did not load: a script, a style, a picture.
    if (t instanceof HTMLScriptElement || t instanceof HTMLLinkElement || t instanceof HTMLImageElement) {
      const src = t instanceof HTMLLinkElement ? t.href : t.src;
      journal({
        level: 'error',
        src: 'web',
        ev: 'resource',
        url: cleanUrl(src),
        text: `${t.tagName.toLowerCase()} did not load`,
      });
      return;
    }
    if (e instanceof ErrorEvent) {
      const where = e.filename ? ` (${cleanUrl(e.filename)}:${e.lineno})` : '';
      journal({ level: 'error', src: 'web', ev: 'exception', text: `${e.message || 'error'}${where}` });
    }
  };
  const onRejection = (e: PromiseRejectionEvent) =>
    journal({ level: 'error', src: 'web', ev: 'exception', text: `unhandled: ${textOf(e.reason)}` });
  const onCsp = (e: SecurityPolicyViolationEvent) =>
    journal({
      level: 'error',
      src: 'web',
      ev: 'csp',
      url: cleanUrl(e.blockedURI || 'inline'),
      text: `blocked by ${e.violatedDirective}`,
    });
  let lastPath = location.pathname;
  const onNav = () => {
    if (location.pathname === lastPath) return;
    lastPath = location.pathname;
    journal({ level: 'info', src: 'web', ev: 'nav', url: cleanUrl(location.pathname) });
  };
  const onClick = (e: MouseEvent) => {
    const name = controlName(e.target);
    if (name === null) return;
    const trace = startAction();
    journal({ level: 'info', src: 'web', ev: 'click', tool: name, trace });
  };
  const onHide = () => flushJournal(true);

  const origError = console.error;
  const origWarn = console.warn;
  console.error = (...args: unknown[]) => {
    journal({ level: 'error', src: 'web', ev: 'console', text: args.map(textOf).join(' ') });
    origError.apply(console, args);
  };
  console.warn = (...args: unknown[]) => {
    journal({ level: 'warn', src: 'web', ev: 'console', text: args.map(textOf).join(' ') });
    origWarn.apply(console, args);
  };
  const origPush = history.pushState.bind(history);
  const origReplace = history.replaceState.bind(history);
  history.pushState = (...a: Parameters<History['pushState']>) => {
    origPush(...a);
    onNav();
  };
  history.replaceState = (...a: Parameters<History['replaceState']>) => {
    origReplace(...a);
    onNav();
  };

  window.addEventListener('error', onError, true);
  window.addEventListener('unhandledrejection', onRejection);
  document.addEventListener('securitypolicyviolation', onCsp);
  window.addEventListener('popstate', onNav);
  document.addEventListener('click', onClick, true);
  window.addEventListener('pagehide', onHide);

  stop = () => {
    flushJournal(true);
    if (originalFetch !== null) window.fetch = originalFetch;
    originalFetch = null;
    console.error = origError;
    console.warn = origWarn;
    history.pushState = origPush;
    history.replaceState = origReplace;
    window.removeEventListener('error', onError, true);
    window.removeEventListener('unhandledrejection', onRejection);
    document.removeEventListener('securitypolicyviolation', onCsp);
    window.removeEventListener('popstate', onNav);
    document.removeEventListener('click', onClick, true);
    window.removeEventListener('pagehide', onHide);
    stop = null;
  };
  return stop;
};
