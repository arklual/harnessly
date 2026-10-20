import { scopedAppPath } from '@/lib/app-path';
import {
  VoiceEngineUnavailableError,
  openMicrophone,
  pickVoiceMimeType,
  type IVoiceEngine,
  type IVoiceEngineHandlers,
} from '@/lib/voice-engine';
import { t } from '@/lib/i18n';

/**
 * Server-side ASR engine: records with MediaRecorder, streams the chunks to /api/voice as
 * they arrive, and applies partial/final text as the machine's recognition service (@/lib/voice-server)
 * recognizes it. The default engine on phones and wherever in-tab Whisper has not proven fast.
 */

const RECORDER_TIMESLICE_MS = 100;
/** From the stop to the final text; the service answers in about a second, the rest is slack for a slow tunnel. */
const STOP_TIMEOUT_MS = 15_000;
const CONNECT_TIMEOUT_MS = 35_000;
/** A socket opened on hover/focus of the mic button waits this long for the recording to start, then closes. */
const PRECONNECT_TTL_MS = 15_000;

const openVoiceSocket = (): WebSocket => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const socket = new WebSocket(`${protocol}//${window.location.host}${scopedAppPath('/api/voice')}`);
  socket.binaryType = 'arraybuffer';
  return socket;
};

// Browser-only module state: one warmed-up socket per tab, never shared with the server.
let prepared: { socket: WebSocket; timer: ReturnType<typeof setTimeout> } | null = null;

const dropPrepared = (): void => {
  if (!prepared) return;
  clearTimeout(prepared.timer);
  prepared.socket.onclose = null;
  prepared.socket.close();
  prepared = null;
};

/** Opens the connection to the recognizer ahead of the press; `start` picks it up if it is still alive. */
export const preconnectVoiceSocket = (): void => {
  if (typeof window === 'undefined' || typeof WebSocket === 'undefined') return;
  if (prepared && prepared.socket.readyState <= WebSocket.OPEN) return;
  dropPrepared();
  const socket = openVoiceSocket();
  const entry = { socket, timer: setTimeout(dropPrepared, PRECONNECT_TTL_MS) };
  socket.onclose = () => {
    if (prepared === entry) {
      clearTimeout(entry.timer);
      prepared = null;
    }
  };
  prepared = entry;
};

const takeVoiceSocket = (): WebSocket => {
  const entry = prepared;
  if (entry && entry.socket.readyState <= WebSocket.OPEN) {
    clearTimeout(entry.timer);
    entry.socket.onclose = null;
    prepared = null;
    return entry.socket;
  }
  dropPrepared();
  return openVoiceSocket();
};

export const isServerVoiceEngineSupported = (): boolean =>
  typeof window !== 'undefined' &&
  !!navigator.mediaDevices?.getUserMedia &&
  typeof MediaRecorder !== 'undefined';

export interface IServerVoiceEngineOptions {
  lang: string;
  deviceId: string | null;
  /** 0..1 input level, sampled every animation frame; best-effort, never blocks recognition if it fails. */
  onLevel?: (level: number) => void;
}

interface IServerMessage {
  type: string;
  text?: string;
  message?: string;
  code?: 'unavailable' | 'failed';
}

export const createServerVoiceEngine = (
  options: IServerVoiceEngineOptions,
  handlers: IVoiceEngineHandlers,
): IVoiceEngine => {
  const { lang, deviceId, onLevel } = options;
  let stream: MediaStream | null = null;
  let recorder: MediaRecorder | null = null;
  let ws: WebSocket | null = null;
  let audioCtx: AudioContext | null = null;
  let levelFrame: number | null = null;
  let stopTimer: ReturnType<typeof setTimeout> | null = null;
  /** Chunks go out strictly in order, and the stop message only after the last of them. */
  let sending: Promise<void> = Promise.resolve();
  /**
   * Recording starts with the microphone, before the service is ready, so the first word is not lost to the
   * connection setup; what is recorded meanwhile waits here.
   */
  let outbox: (() => Promise<string | ArrayBuffer | null>)[] | null = [];
  let lastPartial = '';
  let started = false;
  let stopping = false;
  let settled = false;

  const teardownAudio = (): void => {
    if (levelFrame !== null) cancelAnimationFrame(levelFrame);
    levelFrame = null;
    audioCtx?.close().catch(() => {});
    audioCtx = null;
    stream?.getTracks().forEach((track) => track.stop());
    stream = null;
  };

  const settle = (): void => {
    settled = true;
    if (stopTimer) clearTimeout(stopTimer);
    stopTimer = null;
    if (recorder && recorder.state !== 'inactive') recorder.stop();
    teardownAudio();
    ws?.close();
  };

  const monitorLevel = (source: MediaStream): void => {
    if (!onLevel) return;
    try {
      const ctx = new AudioContext();
      audioCtx = ctx;
      void ctx.resume().catch(() => {});
      const analyser = ctx.createAnalyser();
      analyser.fftSize = 512;
      ctx.createMediaStreamSource(source).connect(analyser);
      const data = new Uint8Array(analyser.frequencyBinCount);
      const tick = () => {
        analyser.getByteTimeDomainData(data);
        let sumSquares = 0;
        for (const value of data) {
          const centered = (value - 128) / 128;
          sumSquares += centered * centered;
        }
        onLevel(Math.min(1, Math.sqrt(sumSquares / data.length) * 4));
        levelFrame = requestAnimationFrame(tick);
      };
      levelFrame = requestAnimationFrame(tick);
    } catch {
      // level meter is a nice-to-have; recognition works without it
    }
  };

  const sendInOrder = (produce: () => Promise<string | ArrayBuffer | null>): void => {
    if (outbox) {
      outbox.push(produce);
      return;
    }
    sending = sending.then(async () => {
      const payload = await produce();
      if (payload !== null && ws?.readyState === WebSocket.OPEN) ws.send(payload);
    });
  };

  const flushOutbox = (): void => {
    const queued = outbox ?? [];
    outbox = null;
    queued.forEach(sendInOrder);
  };

  const sendStop = (): void => sendInOrder(async () => JSON.stringify({ type: 'stop' }));

  /** Resolves when the recognizer says `ready`; the socket is opened before the microphone so both warm up together. */
  const connect = (): Promise<void> =>
    new Promise<void>((resolve, reject) => {
      const socket = takeVoiceSocket();
      ws = socket;
      const connectTimer = setTimeout(() => {
        reject(new VoiceEngineUnavailableError(t('lib.voiceEngineServer.noAnswer')));
        settle();
      }, CONNECT_TIMEOUT_MS);

      const beforeStart = (error: Error): void => {
        clearTimeout(connectTimer);
        reject(error);
        settle();
      };

      const sendStart = (): void => socket.send(JSON.stringify({ type: 'start', lang }));
      if (socket.readyState === WebSocket.OPEN) sendStart();
      else socket.onopen = sendStart;
      socket.onmessage = (event) => {
        if (typeof event.data !== 'string') return;
        let msg: IServerMessage;
        try {
          msg = JSON.parse(event.data) as IServerMessage;
        } catch {
          return;
        }
        if (msg.type === 'ready' && !started) {
          clearTimeout(connectTimer);
          started = true;
          flushOutbox();
          resolve();
        } else if (msg.type === 'partial' && msg.text !== undefined) {
          lastPartial = msg.text;
          handlers.onPartial(msg.text);
        } else if (msg.type === 'final' && msg.text !== undefined) {
          settle();
          handlers.onFinal(msg.text);
        } else if (msg.type === 'error') {
          const message = msg.message ?? t('lib.voiceEngineServer.recognitionError');
          if (!started) {
            beforeStart(
              msg.code === 'unavailable' ? new VoiceEngineUnavailableError(message) : new Error(message),
            );
            return;
          }
          settle();
          handlers.onError(message);
        }
      };
      socket.onerror = () => {
        if (!started) beforeStart(new VoiceEngineUnavailableError(t('lib.voiceEngineServer.connectFailed')));
      };
      socket.onclose = () => {
        if (!started) {
          beforeStart(new VoiceEngineUnavailableError(t('lib.voiceEngineServer.closed')));
          return;
        }
        if (settled) return;
        settle();
        // The runtime restarted mid-dictation: what was already recognized stays in the composer.
        if (lastPartial) handlers.onFinal(lastPartial);
        else handlers.onError(t('lib.voiceEngineServer.dropped'));
      };
    });

  const start = async (): Promise<void> => {
    const mimeType = pickVoiceMimeType((mime) => MediaRecorder.isTypeSupported(mime));
    if (!mimeType) throw new VoiceEngineUnavailableError(t('lib.voiceEngineServer.cannotRecord'));
    const ready = connect();
    // A failure before the microphone is up is reported by the microphone branch below, not as an unhandled rejection.
    ready.catch(() => {});
    try {
      stream = await openMicrophone(deviceId);
    } catch (error) {
      settle();
      throw error;
    }
    if (settled) {
      teardownAudio();
      return ready;
    }
    monitorLevel(stream);
    const rec = new MediaRecorder(stream, { mimeType });
    recorder = rec;
    rec.ondataavailable = (chunk) => {
      if (chunk.data.size > 0) sendInOrder(() => chunk.data.arrayBuffer());
    };
    rec.onstop = () => {
      teardownAudio();
      if (stopping) sendStop();
    };
    rec.start(RECORDER_TIMESLICE_MS);
    handlers.onCapture?.();
    return ready;
  };

  return {
    start,
    stop: () => {
      if (settled || stopping) return;
      stopping = true;
      if (!recorder || recorder.state === 'inactive') {
        sendStop();
      } else {
        // The recorder hands over its last chunk before `onstop`, which then sends the stop.
        recorder.stop();
      }
      stopTimer = setTimeout(() => {
        if (settled) return;
        settle();
        if (lastPartial) handlers.onFinal(lastPartial);
        else handlers.onError(t('lib.voiceEngineServer.timeout'));
      }, STOP_TIMEOUT_MS);
    },
    abort: () => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'abort' }));
      settle();
    },
  };
};
