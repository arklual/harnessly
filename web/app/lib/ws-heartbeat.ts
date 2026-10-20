import { recordSocketDead } from '@/lib/net-telemetry';
import { PING_FRAME, PONG_FRAME } from '@/lib/ws-ping';

export const HEARTBEAT = {
  /** How often a socket on a shown page proves it still reaches the runtime. */
  intervalMs: 20_000,
  /** No answer (any message) within this after a ping: the socket is dead. */
  timeoutMs: 8_000,
  /** After the page comes back to the front or the network returns, a quicker verdict. */
  wakeTimeoutMs: 4_000,
} as const;

/** Closes a socket the road dropped and runs its own close handler now, instead of after the TCP timeout. */
const dropDead = (ws: WebSocket) => {
  const onclose = ws.onclose;
  ws.onopen = null;
  ws.onmessage = null;
  ws.onerror = null;
  ws.onclose = null;
  try {
    ws.close(4000, 'no answer to ping');
  } catch {}
  onclose?.call(ws, new CloseEvent('close', { code: 4000, reason: 'no answer to ping', wasClean: false }));
};

/**
 * Keeps a socket honest: a ping every 20 s while the page is shown, and one at once when the page comes back
 * or the network returns (a phone waking up holds a socket that looks open and leads nowhere). Any message counts as
 * an answer. A socket that stays silent is dropped through its own close handler, which reconnects as usual.
 * The first ping goes out as soon as the socket opens, to learn whether its runtime answers. Returns the stop function;
 * it also stops by itself when the socket closes. A socket with its own protocol (the terminal's binary frames, which
 * every runtime echoes) passes its own `ping`.
 *
 * Only a socket that has itself answered a ping is held to answer the next ones. A pong on one socket used to
 * hold every socket of the same key, so a route that never carried the pong (the timeline through the database gateway
 * forwarded it as a binary frame) had its quiet socket dropped and reopened two or three times a minute.
 */
export const watchSocket = (ws: WebSocket, key: string, ownPing?: () => void): (() => void) => {
  const ping = ownPing ?? (() => ws.send(PING_FRAME));
  let answersPings = ownPing !== undefined;
  let deadline: ReturnType<typeof setTimeout> | null = null;
  const answered = (event: MessageEvent) => {
    if (event.data === PONG_FRAME) answersPings = true;
    if (deadline === null) return;
    clearTimeout(deadline);
    deadline = null;
  };
  const probe = (timeoutMs: number) => {
    if (ws.readyState !== WebSocket.OPEN || deadline !== null) return;
    const held = answersPings;
    try {
      ping();
    } catch {
      return;
    }
    if (!held) return;
    deadline = setTimeout(() => {
      deadline = null;
      stop();
      recordSocketDead(key);
      dropDead(ws);
    }, timeoutMs);
  };
  const onWake = () => {
    if (document.visibilityState === 'visible') probe(HEARTBEAT.wakeTimeoutMs);
  };
  const interval = setInterval(() => {
    if (document.visibilityState === 'visible') probe(HEARTBEAT.timeoutMs);
  }, HEARTBEAT.intervalMs);
  const onOpen = () => probe(HEARTBEAT.timeoutMs);
  const stop = () => {
    if (deadline !== null) clearTimeout(deadline);
    deadline = null;
    clearInterval(interval);
    ws.removeEventListener('open', onOpen);
    ws.removeEventListener('message', answered);
    ws.removeEventListener('close', stop);
    document.removeEventListener('visibilitychange', onWake);
    window.removeEventListener('online', onWake);
  };
  if (!ownPing) ws.addEventListener('open', onOpen);
  ws.addEventListener('message', answered);
  ws.addEventListener('close', stop);
  document.addEventListener('visibilitychange', onWake);
  window.addEventListener('online', onWake);
  return stop;
};

/** The network came back or the page came to the front: a socket waiting out its backoff should try at once. */
export const onNetworkWake = (callback: () => void): (() => void) => {
  const wake = () => {
    if (document.visibilityState === 'visible') callback();
  };
  document.addEventListener('visibilitychange', wake);
  window.addEventListener('online', wake);
  return () => {
    document.removeEventListener('visibilitychange', wake);
    window.removeEventListener('online', wake);
  };
};
