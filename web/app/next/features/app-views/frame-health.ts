// Is an app's view up? The frame's `load` says only that some document arrived (a 404 page loads too, a script that
// failed leaves a loaded, empty page), so «готово» is the SDK's handshake, and nothing else. Three states: the frame
// loads, the view is ready, or it did not start, with the reason in words. Pure, so the machine is tested apart from
// the frame.

/** How long a view has to make its handshake before it is called «не запустилось». */
export const HANDSHAKE_WAIT_MS = 8000;
/** How long the host waits for the runtime to give a view session or a preview slot. */
export const SESSION_WAIT_MS = 15000;

export const FAIL_REASON = {
  /** The view's own file did not load (a script, a style, a page); `detail` names it. */
  file: 'file',
  /** The view server refused the request (404, no session…); `detail` is its sentence. */
  refused: 'refused',
  /** The view threw before it connected. */
  script: 'script',
  /** The page's security rules blocked something the view needed. */
  csp: 'csp',
  /** The runtime answered 401/403: the person is not signed in, or may not. */
  auth: 'auth',
  /** The machine or the app's server did not answer in time. */
  server: 'server',
  /** The app waits for the person's word or a secret before it starts. */
  waiting: 'waiting',
  /** Nothing said why: the page loaded and never connected. */
  silent: 'silent',
} as const;
export type TFailReason = (typeof FAIL_REASON)[keyof typeof FAIL_REASON];

export interface IFailure {
  reason: TFailReason;
  /** The file, the sentence or the runtime's text the reason is about. */
  detail: string;
}

export type TFrameState = { k: 'loading' } | { k: 'ready' } | { k: 'failed'; failure: IFailure };

export type TFrameEvent =
  { k: 'handshake' } | { k: 'timeout' } | { k: 'session-failed'; failure: IFailure } | { k: 'retry' };

export const LOADING: TFrameState = { k: 'loading' };

/**
 * The next state. `load` is not an event on purpose: it changes nothing. A failure the view reported (a file that did not
 * load, a refusal) is kept while the frame waits, and is what the timeout then says; a more specific word is not
 * replaced by a less specific one. A handshake after a failure still wins: the view did come up.
 */
export const nextFrameState = (
  state: TFrameState,
  event: TFrameEvent,
  seen: IFailure | null = null,
): TFrameState => {
  switch (event.k) {
    case 'handshake':
      return state.k === 'ready' ? state : { k: 'ready' };
    case 'timeout':
      return state.k === 'loading'
        ? { k: 'failed', failure: seen ?? { reason: FAIL_REASON.silent, detail: '' } }
        : state;
    case 'session-failed':
      return state.k === 'ready' ? state : { k: 'failed', failure: event.failure };
    case 'retry':
      return LOADING;
  }
};

const SPECIFICITY: Record<TFailReason, number> = {
  silent: 0,
  script: 1,
  csp: 2,
  file: 3,
  refused: 4,
  auth: 5,
  server: 5,
  waiting: 6,
};

/** Of what the view reported, the more telling reason is kept: «файл не загрузился» over a bare exception. */
export const keepTelling = (seen: IFailure | null, next: IFailure): IFailure =>
  seen === null || SPECIFICITY[next.reason] > SPECIFICITY[seen.reason] ? next : seen;

/** A watch on the handshake: calls `onTimeout` once, unless `cancel` or `done` came first. Timers are injectable for tests. */
export interface IWatch {
  done: () => void;
  cancel: () => void;
}
export const watchHandshake = (
  onTimeout: () => void,
  ms = HANDSHAKE_WAIT_MS,
  timers: { set: (f: () => void, ms: number) => unknown; clear: (id: unknown) => void } = {
    set: (f, n) => setTimeout(f, n),
    clear: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
  },
): IWatch => {
  let live = true;
  const id = timers.set(() => {
    if (!live) return;
    live = false;
    onTimeout();
  }, ms);
  const stop = () => {
    live = false;
    timers.clear(id);
  };
  return { done: stop, cancel: stop };
};

/** A promise that gives up: the runtime's session or slot that never comes must not leave a spinner for good. */
export const withTimeout = <T>(work: Promise<T>, ms: number, message: string): Promise<T> =>
  new Promise<T>((resolve, reject) => {
    const id = setTimeout(() => reject(new Error(message)), ms);
    work.then(
      (v) => {
        clearTimeout(id);
        resolve(v);
      },
      (e: unknown) => {
        clearTimeout(id);
        reject(e instanceof Error ? e : new Error(String(e)));
      },
    );
  });

/** What the runtime's refusal of a session says, sorted: no sign-in, or no answer (anything else the runtime says is taken for that, with its own text). */
export const classifySessionError = (message: string): IFailure => {
  if (/\b(401|403)\b|unauthori[sz]ed|forbidden|sign[- ]?in|not logged/i.test(message))
    return { reason: FAIL_REASON.auth, detail: message };
  return { reason: FAIL_REASON.server, detail: message };
};

/** The path of a file inside the app out of the address a view reported: after the app's id, else the last two parts. */
export const fileOf = (src: string, appId: string): string => {
  let path = src;
  try {
    path = new URL(src, 'https://x.invalid').pathname;
  } catch {
    // not an address: used as it is
  }
  const at = path.indexOf(`/${appId}/`);
  if (at >= 0) return path.slice(at + appId.length + 2);
  return path.split('/').filter(Boolean).slice(-2).join('/');
};

/** A message the view's shim sent about a failure (bridge-shim.js, or the refusal page). Anything else is null. */
export interface IViewErrorMessage {
  kind: 'resource' | 'error' | 'csp' | 'refused';
  text: string;
  src: string;
  status: number | null;
}
export const viewErrorOf = (data: unknown): IViewErrorMessage | null => {
  if (typeof data !== 'object' || data === null) return null;
  const raw = (data as { hlViewError?: unknown }).hlViewError;
  if (typeof raw !== 'object' || raw === null) return null;
  const r = raw as Record<string, unknown>;
  const kind = r.kind;
  if (kind !== 'resource' && kind !== 'error' && kind !== 'csp' && kind !== 'refused') return null;
  const status = typeof r.status === 'number' && Number.isFinite(r.status) ? r.status : null;
  return {
    kind,
    text: typeof r.text === 'string' ? r.text.slice(0, 500) : '',
    src: typeof r.src === 'string' ? r.src.slice(0, 500) : '',
    status,
  };
};

/** The failure a view's message stands for. */
export const failureOfViewError = (m: IViewErrorMessage, appId: string): IFailure => {
  if (m.kind === 'refused') return { reason: FAIL_REASON.refused, detail: m.text };
  if (m.kind === 'resource') return { reason: FAIL_REASON.file, detail: fileOf(m.src, appId) };
  if (m.kind === 'csp') return { reason: FAIL_REASON.csp, detail: m.text };
  return { reason: FAIL_REASON.script, detail: m.text };
};
