// Dictation over the machine's socket (/api/voice, internal/voice): the page sends {type: 'start', lang}, then the
// recorder's chunks as they come, then {type: 'stop' | 'abort'}; it gets ready, partial (the whole text so far),
// final and error back. An error with code `unavailable` (or a socket that never opened) means this machine cannot
// recognize on the fly — the caller falls back to sending the whole recording. While the machine gets its speech
// model ready (the first dictation on a machine that has none yet downloads it) it sends `preparing` with the stage
// and how far it is; the recording waits for ready and goes on as soon as it comes.
import { runtimeSocket } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';
import type { TLang } from '@/next/i18n';

/** The model may be loading on the machine; the recording waits for it, so this is generous. */
const CONNECT_MS = 35_000;
/** A download that says nothing for this long is given up; every `preparing` message starts it over. */
const PREPARE_MS = 60_000;

export interface IDictationFailure {
  /** The words of the machine, or '' when it said none. */
  message: string;
  /** This machine cannot recognize on the fly at all: try the whole-recording route. */
  unavailable: boolean;
}

export interface IDictationEvents {
  onReady: () => void;
  /** The machine is getting its model ready (a first-use download): `stage` is what, `percent` how far (0..100). */
  onPreparing?: (stage: string, percent: number) => void;
  /** The whole text recognized so far; it replaces the previous partial. */
  onPartial: (text: string) => void;
  /** The dictation is over and this is its whole text. */
  onFinal: (text: string) => void;
  onFailure: (failure: IDictationFailure) => void;
  /** The socket went away after it was ready, with no final: the machine restarted or the network dropped. */
  onLost: () => void;
}

export interface IDictation {
  readonly ready: boolean;
  send: (chunk: Blob) => void;
  /** Asks for the last final; the socket closes after it. */
  stop: () => void;
  /** Drops the dictation: nothing is recognized further. */
  abort: () => void;
}

interface IServerMessage {
  type?: string;
  text?: string;
  message?: string;
  code?: string;
  stage?: string;
  percent?: number;
}

const parse = (data: unknown): IServerMessage | null => {
  if (typeof data !== 'string') return null;
  try {
    const doc: unknown = JSON.parse(data);
    return typeof doc === 'object' && doc !== null ? (doc as IServerMessage) : null;
  } catch {
    return null;
  }
};

export const openDictation = (m: MachineId, lang: TLang, events: IDictationEvents): IDictation => {
  let socket: WebSocket | null = null;
  let ready = false;
  let over = false;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const end = () => {
    over = true;
    if (timer) clearTimeout(timer);
    timer = null;
    if (!socket) return;
    socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
    if (socket.readyState <= WebSocket.OPEN) socket.close();
    socket = null;
  };
  const fail = (failure: IDictationFailure) => {
    if (over) return;
    end();
    events.onFailure(failure);
  };

  try {
    socket = new WebSocket(runtimeSocket('/api/voice', m));
  } catch {
    queueMicrotask(() => fail({ message: '', unavailable: true }));
    return { ready: false, send: () => {}, stop: () => {}, abort: () => {} };
  }
  const ws = socket;
  timer = setTimeout(() => fail({ message: '', unavailable: true }), CONNECT_MS);
  ws.onopen = () => ws.send(JSON.stringify({ type: 'start', lang }));
  ws.onmessage = (event) => {
    const msg = parse(event.data);
    if (!msg || over) return;
    if (msg.type === 'ready' && !ready) {
      ready = true;
      if (timer) clearTimeout(timer);
      timer = null;
      events.onReady();
    } else if (msg.type === 'preparing' && !ready) {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => fail({ message: '', unavailable: true }), PREPARE_MS);
      events.onPreparing?.(msg.stage ?? '', typeof msg.percent === 'number' ? msg.percent : 0);
    } else if (msg.type === 'partial' && typeof msg.text === 'string') {
      events.onPartial(msg.text);
    } else if (msg.type === 'final' && typeof msg.text === 'string') {
      end();
      events.onFinal(msg.text);
    } else if (msg.type === 'error') {
      fail({ message: msg.message ?? '', unavailable: msg.code === 'unavailable' });
    }
  };
  ws.onerror = () => {
    if (!ready) fail({ message: '', unavailable: true });
  };
  ws.onclose = () => {
    if (over) return;
    if (!ready) {
      fail({ message: '', unavailable: true });
      return;
    }
    end();
    events.onLost();
  };

  return {
    get ready() {
      return ready;
    },
    send: (chunk) => {
      if (ready && !over && ws.readyState === WebSocket.OPEN) ws.send(chunk);
    },
    stop: () => {
      if (ready && !over && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'stop' }));
    },
    abort: () => {
      if (!over && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'abort' }));
      end();
    },
  };
};
