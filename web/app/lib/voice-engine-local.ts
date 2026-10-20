import { prefixAppPath } from '@/lib/app-path';
import { t } from '@/lib/i18n';
import { openMicrophone, type IVoiceEngine, type IVoiceEngineHandlers } from '@/lib/voice-engine';
import type { TVoiceLang } from '@/lib/voice-settings';
import {
  WHISPER_SAMPLE_RATE,
  findSegmentCut,
  isSilent,
  joinTranscriptParts,
  resampleLinear,
  trimSilence,
} from '@/lib/voice-whisper-text';
import type { TWhisperWorkerRequest, TWhisperWorkerResponse } from '@/workers/voice-whisper.worker';

/**
 * In-tab Whisper engine: the microphone is read with Web Audio and recognized by a Web Worker in this
 * tab, so audio goes neither to our server nor to a browser vendor. One worker (and one loaded model) is shared
 * by every composer on the page.
 */

const PARTIAL_INTERVAL_MS = 1000;
/** Whisper sees at most 30 s at once; a longer utterance is committed in segments this long. */
const SEGMENT_MAX_S = 25;
const MIN_SPEECH_S = 0.3;

export const isLocalVoiceEngineSupported = (): boolean =>
  typeof window !== 'undefined' &&
  typeof Worker !== 'undefined' &&
  typeof WebAssembly === 'object' &&
  typeof AudioContext !== 'undefined' &&
  !!navigator.mediaDevices?.getUserMedia;

type TListener = (message: TWhisperWorkerResponse) => void;

let worker: Worker | null = null;
let modelReady = false;
let nextRequestId = 1;
const listeners = new Set<TListener>();

const ensureWorker = (): Worker => {
  if (worker) return worker;
  worker = new Worker(new URL('../workers/voice-whisper.worker.ts', import.meta.url), { type: 'module' });
  worker.onmessage = (event: MessageEvent<TWhisperWorkerResponse>) => {
    if (event.data.type === 'ready') modelReady = true;
    listeners.forEach((listener) => listener(event.data));
  };
  const assetsBase = new URL(prefixAppPath('/api/voice/assets/'), window.location.origin).href;
  worker.postMessage({ type: 'load', assetsBase } satisfies TWhisperWorkerRequest);
  return worker;
};

/** Starts downloading the model ahead of the first press, e.g. when a composer with a mic mounts. */
export const warmUpLocalVoiceEngine = (): void => {
  if (isLocalVoiceEngineSupported()) ensureWorker();
};

export interface ILocalVoiceEngineOptions {
  lang: TVoiceLang;
  deviceId: string | null;
  onLevel?: (level: number) => void;
  /** 0..1 while the model downloads on first use; `null` once it is ready. */
  onModelProgress?: (fraction: number | null) => void;
  /** Median time of one recognition pass in this dictation, reported when it ends. */
  onSpeed?: (medianPassMs: number) => void;
}

export const createLocalVoiceEngine = (
  options: ILocalVoiceEngineOptions,
  handlers: IVoiceEngineHandlers,
): IVoiceEngine => {
  const { lang, deviceId, onLevel, onModelProgress, onSpeed } = options;
  let stream: MediaStream | null = null;
  let audioCtx: AudioContext | null = null;
  let processor: ScriptProcessorNode | null = null;
  let timer: ReturnType<typeof setInterval> | null = null;
  let sampleRate = 48000;
  let chunks: Float32Array[] = [];
  let segmentLength = 0;
  let transcribedLength = 0;
  const committed: string[] = [];
  const passMs: number[] = [];
  let busy = false;
  let active = true;
  const pending = new Map<number, (text: string | null) => void>();

  const stopCapture = (): void => {
    if (timer) clearInterval(timer);
    timer = null;
    processor?.disconnect();
    processor = null;
    audioCtx?.close().catch(() => {});
    audioCtx = null;
    stream?.getTracks().forEach((track) => track.stop());
    stream = null;
    onLevel?.(0);
  };

  const finish = (): void => {
    if (active && passMs.length > 0)
      onSpeed?.([...passMs].sort((a, b) => a - b)[Math.floor(passMs.length / 2)]);
    active = false;
    stopCapture();
    listeners.delete(listener);
    onModelProgress?.(null);
  };

  const listener: TListener = (message) => {
    if (message.type === 'progress') {
      if (message.total > 0) onModelProgress?.(message.loaded / message.total);
    } else if (message.type === 'ready') {
      onModelProgress?.(null);
    } else if (message.type === 'result') {
      pending.get(message.id)?.(message.text);
      pending.delete(message.id);
    } else if (message.type === 'error') {
      const loading = message.id === undefined;
      if (!loading) {
        pending.get(message.id!)?.(null);
        pending.delete(message.id!);
      }
      // A model that cannot load here is the engine being unavailable; «Auto» moves on to the next one.
      if (active) {
        const reason = message.message;
        handlers.onError(
          t(loading ? 'lib.voiceEngineLocal.loadFailed' : 'lib.voiceEngineLocal.recognizeFailed', { reason }),
          loading,
        );
      }
      finish();
    }
  };

  const transcribe = (segment: Float32Array): Promise<string | null> => {
    const audio = trimSilence(segment).slice();
    if (isSilent(audio) || audio.length < MIN_SPEECH_S * WHISPER_SAMPLE_RATE) return Promise.resolve('');
    const id = nextRequestId++;
    const startedAt = performance.now();
    return new Promise((resolve) => {
      pending.set(id, (text) => {
        if (text !== null) passMs.push(performance.now() - startedAt);
        resolve(text);
      });
      ensureWorker().postMessage({ type: 'transcribe', id, audio, lang } satisfies TWhisperWorkerRequest, [
        audio.buffer,
      ]);
    });
  };

  const mergeChunks = (): Float32Array => {
    const merged = new Float32Array(segmentLength);
    let offset = 0;
    for (const chunk of chunks) {
      merged.set(chunk, offset);
      offset += chunk.length;
    }
    return merged;
  };

  const segmentLimit = (): number => SEGMENT_MAX_S * sampleRate;

  /** Cuts off at most `SEGMENT_MAX_S` seconds at a pause, keeping what follows as the next segment. */
  const takeSegmentHead = (): Float32Array => {
    const merged = mergeChunks();
    const cut = findSegmentCut(merged, segmentLimit(), sampleRate);
    const rest = merged.slice(cut);
    chunks = rest.length ? [rest] : [];
    segmentLength = rest.length;
    transcribedLength = 0;
    return resampleLinear(merged.subarray(0, cut), sampleRate);
  };

  const emitPreview = (tail: string): void => {
    const preview = joinTranscriptParts([...committed, tail]);
    if (preview) handlers.onPartial(preview);
  };

  /**
   * Whisper reads at most 30 s, and a slow device can record more than that while one pass runs; every full
   * segment is committed before the tail is looked at, so no pass ever gets more than `SEGMENT_MAX_S`.
   */
  const commitFullSegments = async (): Promise<boolean> => {
    while (segmentLength >= segmentLimit()) {
      const text = await transcribe(takeSegmentHead());
      if (text === null || !active) return false;
      committed.push(text);
      emitPreview('');
    }
    return true;
  };

  const tick = async (): Promise<void> => {
    if (busy || !modelReady || !active) return;
    busy = true;
    try {
      if (!(await commitFullSegments())) return;
      if (segmentLength === transcribedLength) return;
      transcribedLength = segmentLength;
      const text = await transcribe(resampleLinear(mergeChunks(), sampleRate));
      if (active && text !== null) emitPreview(text);
    } finally {
      busy = false;
    }
  };

  const start = async (): Promise<void> => {
    listeners.add(listener);
    ensureWorker();
    stream = await openMicrophone(deviceId);
    if (!active) {
      stopCapture();
      return;
    }
    audioCtx = new AudioContext();
    void audioCtx.resume().catch(() => {});
    sampleRate = audioCtx.sampleRate;
    const source = audioCtx.createMediaStreamSource(stream);
    // ScriptProcessorNode is deprecated but runs everywhere without a separate worklet module to serve.
    processor = audioCtx.createScriptProcessor(4096, 1, 1);
    processor.onaudioprocess = (event) => {
      const input = event.inputBuffer.getChannelData(0);
      chunks.push(new Float32Array(input));
      segmentLength += input.length;
      if (onLevel) {
        let sum = 0;
        for (const value of input) sum += value * value;
        onLevel(Math.min(1, Math.sqrt(sum / input.length) * 4));
      }
    };
    source.connect(processor);
    processor.connect(audioCtx.destination);
    if (!modelReady) onModelProgress?.(0);
    timer = setInterval(() => void tick(), PARTIAL_INTERVAL_MS);
  };

  const stop = (): void => {
    if (!active) return;
    stopCapture();
    void (async () => {
      while (busy) await new Promise((resolve) => setTimeout(resolve, 50));
      busy = true;
      if (!(await commitFullSegments())) return;
      const tail = segmentLength > 0 ? await transcribe(resampleLinear(mergeChunks(), sampleRate)) : '';
      if (!active || tail === null) return;
      const text = joinTranscriptParts([...committed, tail]);
      finish();
      if (text) handlers.onFinal(text);
      else handlers.onEnd();
    })();
  };

  return {
    start,
    stop,
    abort: () => {
      pending.forEach((resolve) => resolve(null));
      pending.clear();
      finish();
    },
  };
};
