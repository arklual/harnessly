// The wake phrase: «Привет, harnessly» starts the voice without a button. The page hears it itself, with a
// small Russian speech model run in the browser (Vosk, WebAssembly, Apache-2.0) and a grammar of a few phrases; no
// sound leaves the page until the phrase is heard. The model (≈46 MB) comes from the machine (realtime/wake.go) only
// when the setting is on, and the browser engine keeps it by its address; the microphone is held only while listening.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

/** The model's words for «harnessly»: the word itself is not in its vocabulary, «хар несли» sounds the same. */
const WAKE = 'привет хар несли';
/** Decoys: words that sound close win over the phrase instead of being forced into it (measured: no false starts in
 *  11 minutes of unrelated speech with them, 20 of 20 phrases heard). */
export const WAKE_GRAMMAR = [WAKE, 'привет', 'хар', 'несли', 'харли', 'характер', '[unk]'] as const;

/** A final result of the grammar is the wake phrase. */
export const isWake = (text: string): boolean => text.includes(WAKE);

const norm = (text: string) =>
  text
    .toLowerCase()
    .replaceAll('ё', 'е')
    .replace(/[^\p{L}\p{N}\s]/gu, ' ')
    .split(/\s+/)
    .filter(Boolean);

/** «Пока» or «спасибо, всё» closing a short line of the person: the conversation is over. «Пока ты это делаешь…»
 *  and long lines that only end so are not. */
export const isBye = (text: string): boolean => {
  const w = norm(text);
  if (w.length === 0 || w.length > 5) return false;
  const tail = w.slice(-2).join(' ');
  return w[w.length - 1] === 'пока' || tail === 'спасибо все' || tail === 'все спасибо';
};

/** Seconds of nobody speaking that end a call the phrase started, unless Settings say otherwise. */
export const WAKE_SILENCE_DEFAULT = 20;

const modelUrl = (m: MachineId) => runtimePath(platformPath('/voice/live/wake/model'), m);

type TVosk = typeof import('vosk-browser');
type TModel = Awaited<ReturnType<TVosk['createModel']>>;

let loading: { machine: MachineId; model: Promise<TModel> } | null = null;

const loadModel = (m: MachineId): Promise<TModel> => {
  if (loading?.machine === m) return loading.model;
  dropModel();
  const model = import('vosk-browser').then((vosk) => vosk.createModel(modelUrl(m), -1));
  loading = { machine: m, model };
  model.catch(() => {
    if (loading?.model === model) loading = null;
  });
  return model;
};

/** The setting was turned off: the model's worker and memory go too. */
export const dropModel = () => {
  const was = loading;
  loading = null;
  void was?.model.then((m) => m.terminate()).catch(() => undefined);
};

interface IAudioDataLike {
  numberOfFrames: number;
  sampleRate: number;
  copyTo: (into: Float32Array, opt: { planeIndex: number; format: 'f32-planar' }) => void;
  close: () => void;
}
type TTrackProcessor = new (init: { track: MediaStreamTrack }) => {
  readable: ReadableStream<IAudioDataLike>;
};

/** Feeds the microphone's samples to `on` until the returned function is called. Chrome reads the track directly
 *  (no AudioContext: it would stay suspended until a click after a reload); elsewhere an AudioContext does. */
const feed = (track: MediaStreamTrack, on: (pcm: Float32Array, rate: number) => void): (() => void) => {
  const Processor = (globalThis as { MediaStreamTrackProcessor?: TTrackProcessor }).MediaStreamTrackProcessor;
  if (Processor) {
    const reader = new Processor({ track }).readable.getReader();
    let live = true;
    void (async () => {
      while (live) {
        const { value, done } = await reader.read().catch(() => ({ value: undefined, done: true }));
        if (done || !value) return;
        const pcm = new Float32Array(value.numberOfFrames);
        value.copyTo(pcm, { planeIndex: 0, format: 'f32-planar' });
        const rate = value.sampleRate;
        value.close();
        if (live) on(pcm, rate);
      }
    })();
    return () => {
      live = false;
      void reader.cancel().catch(() => undefined);
    };
  }
  const ctx = new AudioContext();
  const src = ctx.createMediaStreamSource(new MediaStream([track]));
  // ScriptProcessor is old but everywhere and enough for a 16 kHz grammar; its output is silenced.
  const proc = ctx.createScriptProcessor(4096, 1, 1);
  const mute = ctx.createGain();
  mute.gain.value = 0;
  proc.onaudioprocess = (e) => on(new Float32Array(e.inputBuffer.getChannelData(0)), ctx.sampleRate);
  src.connect(proc);
  proc.connect(mute);
  mute.connect(ctx.destination);
  const wake = () => void ctx.resume().catch(() => undefined);
  if (ctx.state === 'suspended') {
    window.addEventListener('pointerdown', wake, { once: true });
    window.addEventListener('keydown', wake, { once: true });
  }
  return () => {
    window.removeEventListener('pointerdown', wake);
    window.removeEventListener('keydown', wake);
    proc.disconnect();
    src.disconnect();
    void ctx.close().catch(() => undefined);
  };
};

/** Listens for the phrase on machine `m`'s model; `onWake` is called once per phrase. The returned function lets the
 *  microphone go (the model stays loaded for the next listen). Rejects when the model or the microphone is refused. */
export const listenForWake = async (m: MachineId, onWake: () => void): Promise<() => void> => {
  const model = await loadModel(m);
  const mic = await navigator.mediaDevices.getUserMedia({
    audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
  });
  const track = mic.getAudioTracks()[0];
  let rec: InstanceType<TModel['KaldiRecognizer']> | null = null;
  let rate = 0;
  let last = 0;
  const stopFeed = feed(track, (pcm, sampleRate) => {
    if (!rec || rate !== sampleRate) {
      rec?.remove();
      rate = sampleRate;
      rec = new model.KaldiRecognizer(sampleRate, JSON.stringify(WAKE_GRAMMAR));
      rec.on('result', (msg) => {
        const r = 'result' in msg ? msg.result : null;
        const text = r && 'text' in r ? r.text : '';
        const now = performance.now();
        if (isWake(text) && now - last > 3000) {
          last = now;
          onWake();
        }
      });
    }
    rec.acceptWaveformFloat(pcm, sampleRate);
  });
  return () => {
    stopFeed();
    rec?.remove();
    for (const t of mic.getTracks()) t.stop();
  };
};

/** A short quiet chime: rising when the voice starts listening, falling when the conversation is over. Made here,
 *  no sound files. */
export const chime = (kind: 'start' | 'end') => {
  try {
    const ctx = new AudioContext();
    const notes = kind === 'start' ? [659.25, 880] : [880, 659.25];
    const t0 = ctx.currentTime + 0.02;
    notes.forEach((hz, i) => {
      const at = t0 + i * 0.11;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'sine';
      osc.frequency.value = hz;
      gain.gain.setValueAtTime(0.0001, at);
      gain.gain.exponentialRampToValueAtTime(0.07, at + 0.015);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.22);
      osc.connect(gain).connect(ctx.destination);
      osc.start(at);
      osc.stop(at + 0.25);
    });
    window.setTimeout(() => void ctx.close().catch(() => undefined), 700);
  } catch {
    // No sound where the page may not play any: the head's state still shows it.
  }
};
