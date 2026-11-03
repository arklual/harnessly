// Signing in to an MCP server in a popup of the service's own sign-in. The popup opens blank in the click itself (a
// window opened after an await is blocked), then goes to the sign-in once the machine says where it is. The runtime's
// callback page tells this page by a `harnessly-mcp` message; a GET of the list every 2 s while the popup is open is
// the fallback (an IdP's COOP can cut the opener). A blocked popup leaves a link to open the sign-in by hand.
import { useCallback, useEffect, useRef, useState } from 'react';
import { MCP_STATE, type IMcpServer } from '@/next/features/connections/mcp-api';

/** The popup's name: a second sign-in reuses the window of the first. */
export const POPUP_NAME = 'harnessly-mcp';
export const POPUP_FEATURES = 'popup,width=520,height=720';
/** The message the runtime's callback page posts to its opener. */
export const MESSAGE_TYPE = 'harnessly-mcp';
const POLL_MS = 2000;
/** The runtime keeps a sign-in's state this long: waiting longer is pointless. */
const GIVE_UP_MS = 10 * 60_000;

export const FLOW = { opening: 'opening', waiting: 'waiting', blocked: 'blocked' } as const;
export type TFlowPhase = (typeof FLOW)[keyof typeof FLOW];

export interface IFlow {
  id: string;
  phase: TFlowPhase;
  /** Known once the machine answered; the blocked phase links to it. */
  authUrl: string | null;
}

/** A popup opened in a click, before it is known where it goes; null when the browser blocked it. */
export interface ISlot {
  win: Window | null;
}

export interface ISignInFailure {
  id: string;
  reason: string;
}

interface IMessage {
  type: typeof MESSAGE_TYPE;
  id: string;
  ok: boolean;
  reason: string;
}

const readMessage = (data: unknown): IMessage | null => {
  if (typeof data !== 'object' || data === null) return null;
  const o = data as Record<string, unknown>;
  if (o.type !== MESSAGE_TYPE || typeof o.id !== 'string') return null;
  return {
    type: MESSAGE_TYPE,
    id: o.id,
    ok: o.ok === true,
    reason: typeof o.reason === 'string' ? o.reason : '',
  };
};

const openPopup = (url: string): Window | null => {
  try {
    return window.open(url, POPUP_NAME, POPUP_FEATURES);
  } catch {
    return null;
  }
};

const closeQuietly = (win: Window | null) => {
  try {
    win?.close();
  } catch {
    // A window of another origin may refuse: it is the person's to close then.
  }
};

export interface ISignInOptions {
  servers: readonly IMcpServer[] | null;
  /** Where the service's sign-in is, or the words of why the machine would not say. */
  onStart: (id: string) => Promise<{ authUrl: string } | { error: string }>;
  /** Reads the list again (its answer replaces the page's). */
  onRefresh: () => Promise<void>;
  /** Words for a reason the callback sent. */
  reasonText: (reason: string) => string;
}

export interface ISignIn {
  flow: IFlow | null;
  failure: ISignInFailure | null;
  /** Opens the blank popup: call it in the click, before any await. */
  prepare: () => ISlot;
  /** Gives up a prepared popup that turned out not to be needed. */
  release: (slot: ISlot) => void;
  /** Asks where the sign-in is and sends the prepared popup there. */
  start: (id: string, slot: ISlot) => Promise<void>;
  /** The «Open sign-in» link of a blocked popup: opens it in this click. */
  reopen: () => boolean;
  cancel: () => void;
}

export const useMcpSignIn = ({ servers, onStart, onRefresh, reasonText }: ISignInOptions): ISignIn => {
  const [flow, setFlow] = useState<IFlow | null>(null);
  const [failure, setFailure] = useState<ISignInFailure | null>(null);
  const popup = useRef<Window | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const prepare = useCallback((): ISlot => ({ win: openPopup('about:blank') }), []);
  const release = useCallback((slot: ISlot) => closeQuietly(slot.win), []);

  const start = useCallback(
    async (id: string, slot: ISlot) => {
      setFailure(null);
      setFlow({ id, phase: FLOW.opening, authUrl: null });
      const got = await onStart(id);
      if (!alive.current) return;
      if ('error' in got) {
        closeQuietly(slot.win);
        setFlow(null);
        setFailure({ id, reason: got.error });
        return;
      }
      if (slot.win === null || slot.win.closed) {
        setFlow({ id, phase: FLOW.blocked, authUrl: got.authUrl });
        return;
      }
      slot.win.location.href = got.authUrl;
      popup.current = slot.win;
      setFlow({ id, phase: FLOW.waiting, authUrl: got.authUrl });
    },
    [onStart],
  );

  const reopen = useCallback((): boolean => {
    if (flow === null || flow.authUrl === null) return false;
    const win = openPopup(flow.authUrl);
    if (win === null) return false;
    popup.current = win;
    setFlow({ ...flow, phase: FLOW.waiting });
    return true;
  }, [flow]);

  const cancel = useCallback(() => {
    closeQuietly(popup.current);
    popup.current = null;
    setFlow(null);
  }, []);

  const waitingId = flow !== null && flow.phase === FLOW.waiting ? flow.id : null;

  // The callback page's word: this page's origin only, this flow's server only.
  useEffect(() => {
    if (waitingId === null) return;
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      const msg = readMessage(event.data);
      if (msg === null || msg.id !== waitingId) return;
      popup.current = null;
      setFlow(null);
      if (!msg.ok) setFailure({ id: msg.id, reason: reasonText(msg.reason) });
      void onRefresh();
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [waitingId, onRefresh, reasonText]);

  // The fallback: the list every 2 s while the popup is open; a closed popup ends the wait after one last read.
  useEffect(() => {
    if (waitingId === null) return;
    const began = Date.now();
    const timer = window.setInterval(() => {
      const closed = popup.current === null || popup.current.closed;
      void onRefresh();
      if (!closed && Date.now() - began < GIVE_UP_MS) return;
      popup.current = null;
      setFlow((now) => (now !== null && now.id === waitingId ? null : now));
    }, POLL_MS);
    return () => window.clearInterval(timer);
  }, [waitingId, onRefresh]);

  // Signed in: the list says so (by the message's read or the poll). The wait ends as the list is taken (state adjusted
  // during render); the callback page closes its own window.
  const done =
    waitingId !== null &&
    servers !== null &&
    servers.some((s) => s.id === waitingId && s.state === MCP_STATE.ok);
  if (done) setFlow(null);

  return { flow, failure, prepare, release, start, reopen, cancel };
};
